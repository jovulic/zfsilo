package command_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jovulic/zfsilo/lib/command"
	"golang.org/x/crypto/ssh"
)

// TestLocalExecutor covers all test cases for the LocalExecutor.
func TestLocalExecutor(t *testing.T) {
	ctx := context.Background()

	t.Run("it executes a successful command", func(t *testing.T) {
		executor := command.NewLocalExecutor(command.LocalExecutorConfig{})
		result, err := executor.Exec(ctx, `echo "hello world"`)
		if err != nil {
			t.Fatalf("expected no error, but got: %v", err)
		}
		if result == nil {
			t.Fatal("expected a result, but got nil")
		}

		expectedOut := "hello world\n"
		if result.Stdout != expectedOut {
			t.Errorf("expected stdout %q, but got %q", expectedOut, result.Stdout)
		}
		if result.Stderr != "" {
			t.Errorf("expected empty stderr, but got %q", result.Stderr)
		}
		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, but got %d", result.ExitCode)
		}
	})

	t.Run("it captures stderr correctly", func(t *testing.T) {
		executor := command.NewLocalExecutor(command.LocalExecutorConfig{})
		result, err := executor.Exec(ctx, `echo "error message" >&2`)
		if err != nil {
			t.Fatalf("expected no error for successful command with stderr, but got: %v", err)
		}

		expectedErr := "error message\n"
		if result.Stderr != expectedErr {
			t.Errorf("expected stderr %q, but got %q", expectedErr, result.Stderr)
		}
		if result.Stdout != "" {
			t.Errorf("expected empty stdout, but got %q", result.Stdout)
		}
		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, but got %d", result.ExitCode)
		}
	})

	t.Run("it handles non-zero exit codes", func(t *testing.T) {
		executor := command.NewLocalExecutor(command.LocalExecutorConfig{})
		result, err := executor.Exec(ctx, `sh -c 'exit 42'`)

		if err == nil {
			t.Fatal("expected an error for non-zero exit code, but got nil")
		}

		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("expected error to be of type *exec.ExitError, but it wasn't")
		}

		if result == nil {
			t.Fatal("result should not be nil on ExitError")
		}

		if result.ExitCode != 42 {
			t.Errorf("expected exit code 42, but got %d", result.ExitCode)
		}
	})

	t.Run("it handles command not found", func(t *testing.T) {
		executor := command.NewLocalExecutor(command.LocalExecutorConfig{})
		result, err := executor.Exec(ctx, "this-command-should-not-exist-12345")

		if err == nil {
			t.Fatal("expected an error for a command not found, but got nil")
		}
		fmt.Printf("%d\n", result.ExitCode)
		if !strings.Contains(result.Stderr, "command not found") {
			t.Errorf("expected stderr to contain 'command not found', but got: %v", result.Stderr)
		}
		if result.ExitCode != 127 {
			t.Errorf("expected exit code 127, but got: %d", result.ExitCode)
		}
	})

	t.Run("it executes as root when configured", func(t *testing.T) {
		// This test can only run on non-Windows OS and requires passwordless sudo for the current user.
		if runtime.GOOS == "windows" {
			t.Skip("Skipping sudo test on Windows")
		}

		currentUser, err := user.Current()
		if err != nil {
			t.Fatalf("could not get current user: %v", err)
		}
		if currentUser.Uid == "0" {
			t.Skip("skipping sudo test because user is already root")
		}

		// Check for passwordless sudo by running a non-interactive command.
		// If this fails, we can't reliably test the feature.
		cmd := exec.Command("sudo", "-n", "true")
		if err := cmd.Run(); err != nil {
			t.Skipf("skipping sudo test: passwordless sudo does not appear to be configured for user %s. error: %v", currentUser.Username, err)
		}

		executor := command.NewLocalExecutor(command.LocalExecutorConfig{
			RunAsRoot: true,
		})

		// `whoami` when run with sudo should return "root".
		result, err := executor.Exec(ctx, `whoami`)
		if err != nil {
			t.Fatalf("expected no error for sudo command, but got: %v", err)
		}

		expectedOut := "root\n"
		if result.Stdout != expectedOut {
			t.Errorf("expected stdout %q from sudo command, but got %q", expectedOut, result.Stdout)
		}
		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, but got %d", result.ExitCode)
		}
	})
}

// sshServer represents a test SSH server and its properties.
type sshServer struct {
	listener   net.Listener
	serverConf *ssh.ServerConfig
	wg         sync.WaitGroup
	mu         sync.Mutex
	conns      []net.Conn
}

// newTestSSHServer sets up and starts a mock SSH server for testing.
func newTestSSHServer(t *testing.T) *sshServer {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("Failed to create signer from private key: %v", err)
	}

	serverConf := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "testuser" && string(pass) == "testpass" {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}
	serverConf.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen on a port: %v", err)
	}

	s := &sshServer{
		listener:   listener,
		serverConf: serverConf,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				// listener was closed.
				return
			}

			s.mu.Lock()
			s.conns = append(s.conns, conn)
			s.mu.Unlock()

			go s.handleConnection(conn)
		}
	}()

	return s
}

// Addr returns the network address of the running server.
func (s *sshServer) Addr() string {
	return s.listener.Addr().String()
}

// Close gracefully shuts down the server and its connections.
func (s *sshServer) Close() {
	s.listener.Close()
	s.mu.Lock()
	for _, conn := range s.conns {
		conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// CloseClientConnections simulates a network drop by closing all active client conns.
func (s *sshServer) CloseClientConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.conns {
		conn.Close()
	}
	s.conns = nil
}

// handleConnection manages an incoming SSH connection.
func (s *sshServer) handleConnection(conn net.Conn) {
	_, chans, reqs, err := ssh.NewServerConn(conn, s.serverConf)
	if err != nil {
		return // connection failed
	}
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go func(in <-chan *ssh.Request) {
			for req := range in {
				if req.Type == "exec" {
					var payload struct{ Command string }
					if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
						req.Reply(false, nil)
						channel.Close()
						return
					}
					command := payload.Command
					req.Reply(true, nil)

					switch command {
					case `echo "hello ssh"`:
						io.WriteString(channel, "hello ssh\n")
						channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
					case `echo "ssh error" >&2`:
						io.WriteString(channel.Stderr(), "ssh error\n")
						channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
					case `exit 99`:
						channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{99}))
					default:
						io.WriteString(channel.Stderr(), "unknown command\n")
						channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{127}))
					}
					channel.Close()
					return
				}
				req.Reply(false, nil)
			}
		}(requests)
	}
}

// TestRemoteExecutor covers all test cases for the RemoteExecutor.
func TestRemoteExecutor(t *testing.T) {
	server := newTestSSHServer(t)
	defer server.Close()

	host, portStr, _ := net.SplitHostPort(server.Addr())
	var port uint16
	fmt.Sscanf(portStr, "%d", &port)

	baseConfig := command.RemoteExecutorConfig{
		Address:  host,
		Port:     port,
		Username: "testuser",
		Password: "testpass",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("it performs startup and shutdown correctly", func(t *testing.T) {
		executor := command.NewRemoteExecutor(baseConfig)
		if err := executor.Startup(ctx); err != nil {
			t.Fatalf("Startup() failed: %v", err)
		}

		// Idempotent startup
		if err := executor.Startup(ctx); err != nil {
			t.Fatalf("second Startup() call failed: %v", err)
		}

		if err := executor.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown() failed: %v", err)
		}

		// Idempotent shutdown
		if err := executor.Shutdown(ctx); err != nil {
			t.Fatalf("second Shutdown() call failed: %v", err)
		}
	})

	t.Run("it executes a command successfully after startup", func(t *testing.T) {
		executor := command.NewRemoteExecutor(baseConfig)
		if err := executor.Startup(ctx); err != nil {
			t.Fatalf("Startup() failed: %v", err)
		}
		defer executor.Shutdown(ctx)

		result, err := executor.Exec(ctx, `echo "hello ssh"`)
		if err != nil {
			t.Fatalf("Exec() failed: %v", err)
		}

		if result.Stdout != "hello ssh\n" {
			t.Errorf("expected stdout %q, got %q", "hello ssh\n", result.Stdout)
		}
		if result.Stderr != "" {
			t.Errorf("expected empty stderr, got %q", result.Stderr)
		}
		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.ExitCode)
		}
	})

	t.Run("it executes lazily without explicit startup", func(t *testing.T) {
		executor := command.NewRemoteExecutor(baseConfig)
		defer executor.Shutdown(ctx) // Ensure cleanup even if test fails

		result, err := executor.Exec(ctx, `echo "hello ssh"`)
		if err != nil {
			t.Fatalf("Exec() failed on lazy startup: %v", err)
		}
		if result.Stdout != "hello ssh\n" {
			t.Errorf("expected stdout %q, got %q", "hello ssh\n", result.Stdout)
		}
	})

	t.Run("it handles non-zero exit codes from remote", func(t *testing.T) {
		executor := command.NewRemoteExecutor(baseConfig)
		defer executor.Shutdown(ctx)

		result, err := executor.Exec(ctx, "exit 99")
		if err == nil {
			t.Fatal("expected an error for non-zero exit code, but got nil")
		}

		var exitErr *ssh.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected error to be of type *ssh.ExitError, but it wasn't")
		}
		if result.ExitCode != 99 {
			t.Errorf("expected exit code 99, got %d", result.ExitCode)
		}
	})

	t.Run("it handles connection drop and reconnects", func(t *testing.T) {
		executor := command.NewRemoteExecutor(baseConfig)

		// First command should succeed and establish connection.
		_, err := executor.Exec(ctx, `echo "hello ssh"`)
		if err != nil {
			t.Fatalf("initial command failed: %v", err)
		}

		// Simulate a network drop by closing the connection from the server side.
		server.CloseClientConnections()

		// Allow a moment for the connection to be recognized as closed.
		time.Sleep(100 * time.Millisecond)

		// This command should fail on the first attempt inside Exec, trigger a redial, and then succeed.
		result, err := executor.Exec(ctx, `echo "hello ssh"`)
		if err != nil {
			t.Fatalf("second command after reconnect failed: %v", err)
		}

		if result.Stdout != "hello ssh\n" {
			t.Errorf("expected stdout %q, got %q", "hello ssh\n", result.Stdout)
		}
	})
}
