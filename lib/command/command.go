// Package command provides a uniform interface for executing commands on local
// and remote hosts.
package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/jovulic/zfsilo/lib/structs"
	slogctx "github.com/veqryn/slog-context"
)

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor interface {
	Exec(ctx context.Context, command string) (*CommandResult, error)
}

type LocalExecutorConfig struct {
	RunAsRoot bool
}

type LocalExecutor struct {
	runAsRoot bool
}

func NewLocalExector(config LocalExecutorConfig) *LocalExecutor {
	if err := structs.Apply(&config); err != nil {
		message := fmt.Sprintf("command: failed to process config: %s", err)
		panic(message)
	}
	return &LocalExecutor{
		runAsRoot: config.RunAsRoot,
	}
}

func (e *LocalExecutor) Exec(ctx context.Context, command string) (*CommandResult, error) {
	cmd := func() *exec.Cmd {
		if e.runAsRoot {
			return exec.CommandContext(ctx, "sudo", "sh", "-c", command)
		}
		return exec.CommandContext(ctx, "sh", "-c", command)
	}()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: cmd.ProcessState.ExitCode(),
	}

	if err != nil {
		// exec.ExitError is expected for non-zero exit codes, so we return the
		// result along with the error.
		if _, ok := err.(*exec.ExitError); ok {
			return result, err
		}
		// For other errors, just return the error.
		return nil, err
	}
	return result, nil
}

type RemoteExecutorConfig struct {
	RunAsRoot bool
	Address   string `validate:"required"`
	Port      uint16 `validate:"required"`
	Username  string `validate:"required"`
	Password  string `validate:"required"`
}

type RemoteExecutor struct {
	runAsRoot  bool
	address    string
	port       uint16
	username   string
	password   string
	clientLock sync.Mutex
	client     *ssh.Client
}

func NewRemoteExecutor(config RemoteExecutorConfig) *RemoteExecutor {
	if err := structs.Apply(&config); err != nil {
		message := fmt.Sprintf("command: failed to process config: %s", err)
		panic(message)
	}
	return &RemoteExecutor{
		runAsRoot: config.RunAsRoot,
		address:   config.Address,
		port:      config.Port,
		username:  config.Username,
		password:  config.Password,
	}
}

func (e *RemoteExecutor) Startup(ctx context.Context) error {
	e.clientLock.Lock()
	defer e.clientLock.Unlock()

	connected := e.client != nil
	if connected {
		return nil
	}

	client, err := e.dial(ctx)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}

	e.client = client
	return nil
}

func (e *RemoteExecutor) Shutdown(ctx context.Context) error {
	e.clientLock.Lock()
	defer e.clientLock.Unlock()

	connected := e.client != nil
	if !connected {
		return nil
	}

	if err := e.client.Close(); err != nil {
		return fmt.Errorf("failed to close client: %w", err)
	}

	e.client = nil
	return nil
}

func (e *RemoteExecutor) Exec(ctx context.Context, command string) (*CommandResult, error) {
	connected := e.client != nil
	if !connected {
		// We perform the startup if the executor has not been initialized rather
		// than erroring out.
		slogctx.Debug(ctx, "performing remote executor startup from exec")
		if err := e.Startup(ctx); err != nil {
			return nil, fmt.Errorf("failed to perform startup: %w", err)
		}
	}

	e.clientLock.Lock()
	defer e.clientLock.Unlock()

	var session *ssh.Session
	for cnt := 0; ; cnt++ {
		if cnt > 1 {
			return nil, fmt.Errorf("failed to create ssh session: retry failed")
		}

		sess, err := e.client.NewSession()
		if errors.Is(err, io.EOF) {
			// The underlying connection dropped (maybe?). Try re-connecting and then
			// retry creating a session.
			var client *ssh.Client
			client, err = e.dial(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to create new session: failed to dial: %w", err)
			}

			// We close the old client before replacement (to be nice).
			e.client.Close()
			e.client = client
			continue
		} else if err != nil {
			return nil, fmt.Errorf("failed to create new session: %w", err)
		}

		session = sess
		break
	}

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err := session.Run(command)

	result := &CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0, // default to 0
	}

	if err != nil {
		// If there was an error, we try to extract the exit code.
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitStatus()
		}
		// We always return the result on error as it can contain useful
		// information.
		return result, err
	}
	return result, nil
}

func (e *RemoteExecutor) dial(ctx context.Context) (*ssh.Client, error) {
	_ = ctx

	client, err := ssh.Dial(
		"tcp",
		fmt.Sprintf("%s:%d", e.address, e.port),
		&ssh.ClientConfig{
			User:            e.username,
			Auth:            []ssh.AuthMethod{ssh.Password(e.password)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to dial host: %w", err)
	}
	return client, nil
}
