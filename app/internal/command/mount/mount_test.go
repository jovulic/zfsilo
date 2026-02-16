package mount_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jovulic/zfsilo/app/internal/command/mount"
	"github.com/jovulic/zfsilo/lib/command"
	"github.com/stretchr/testify/require"
)

// The test host for mount tests runs on port 2222.
var testHostConfig = command.RemoteExecutorConfig{
	Address:  "localhost",
	Port:     2222,
	Username: "root",
	Password: "",
}

func newTestExecutor(t *testing.T, config command.RemoteExecutorConfig) command.Executor {
	if testing.Short() {
		t.Skip("skipping test that requires remote executor in short mode")
	}

	executor := command.NewRemoteExecutor(config)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := executor.Startup(ctx); err != nil {
		t.Fatalf("failed to start remote executor for %s:%d: %v", config.Address, config.Port, err)
	}

	t.Cleanup(func() {
		executor.Shutdown(context.Background())
	})

	return executor
}

func TestMountAndUmount(t *testing.T) {
	ctx := context.Background()
	executor := newTestExecutor(t, testHostConfig)
	mountClient := mount.With(executor)

	sourcePath := fmt.Sprintf("/tmp/mount-test-src-%d", time.Now().UnixNano())
	targetPath := fmt.Sprintf("/tmp/mount-test-target-%d", time.Now().UnixNano())

	// Create source and target directories.
	_, err := executor.Exec(ctx, fmt.Sprintf("mkdir -p %s %s", sourcePath, targetPath))
	require.NoError(t, err)

	// Cleanup.
	defer func() {
		// Unmount if it's still mounted.
		notMounted, err := mountClient.IsMounted(ctx, targetPath)
		if err == nil && !notMounted {
			_ = mountClient.Umount(ctx, mount.UmountArguments{Path: targetPath})
		}
		// Remove directories.
		_, _ = executor.Exec(ctx, fmt.Sprintf("rm -rf %s %s", sourcePath, targetPath))
	}()

	// Verify target is not a mountpoint initially.
	mounted, err := mountClient.IsMounted(ctx, targetPath)
	require.NoError(t, err)
	require.False(t, mounted, "target path should not be a mountpoint initially")

	// Perform a bind mount.
	err = mountClient.Mount(ctx, mount.MountArguments{
		SourcePath: sourcePath,
		TargetPath: targetPath,
		Options:    []string{"bind", "defaults"},
	})
	require.NoError(t, err)

	// 3. Verify target is now a mountpoint.
	mounted, err = mountClient.IsMounted(ctx, targetPath)
	require.NoError(t, err)
	require.True(t, mounted, "target path should be a mountpoint after mount")

	// Unmount the target.
	err = mountClient.Umount(ctx, mount.UmountArguments{Path: targetPath})
	require.NoError(t, err)

	// Verify target is no longer a mountpoint.
	mounted, err = mountClient.IsMounted(ctx, targetPath)
	require.NoError(t, err)
	require.False(t, mounted, "target path should not be a mountpoint after umount")
}
