package mkfs_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jovulic/zfsilo/app/internal/command/mkfs"
	"github.com/jovulic/zfsilo/app/internal/command/zfs"
	"github.com/jovulic/zfsilo/lib/command"
	"github.com/stretchr/testify/require"
)

const mb = 1 << 20

var giveHostConfig = command.RemoteExecutorConfig{
	Address:  "localhost",
	Port:     9000,
	Username: "root",
	Password: "",
}

func newTestExecutor(t *testing.T, config command.RemoteExecutorConfig) command.Executor {
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

func TestFormat(t *testing.T) {
	ctx := context.Background()
	executor := newTestExecutor(t, giveHostConfig)

	zfsClient := zfs.With(executor)
	mkfsClient := mkfs.With(executor)

	volName := fmt.Sprintf("tank/test-mkfs-%d", time.Now().UnixNano())
	volSize := uint64(10 * mb)

	// Create a ZFS volume to get a block device.
	err := zfsClient.CreateVolume(ctx, zfs.CreateVolumeArguments{Name: volName, Size: volSize})
	require.NoError(t, err, "failed to create zfs volume for mkfs test")

	devicePath := fmt.Sprintf("/dev/zvol/%s", volName)
	defer func() {
		// Use the Mkfs client's Clear method to wipe filesystem signatures
		// before destroying the ZFS volume. Errors from Clear are ignored
		// in defer as the goal is to cleanup the volume regardless.
		_ = mkfsClient.Clear(ctx, mkfs.ClearArguments{Device: devicePath})

		err := zfsClient.DestroyVolume(ctx, zfs.DestroyVolumeArguments{Name: volName})
		require.NoError(t, err, "failed to destroy zfs volume")
	}()

	// Format the device with ext4, waiting for it to be ready.
	err = mkfsClient.Format(ctx, mkfs.FormatArguments{
		Device:        devicePath,
		WaitForDevice: true,
	})
	require.NoError(t, err)

	// Verify that the device is formatted as ext4.
	blkidCmd := fmt.Sprintf("blkid -s TYPE -o value %s", devicePath)
	result, err := executor.Exec(ctx, blkidCmd)
	require.NoError(t, err, "failed to execute blkid to verify filesystem type")
	fsType := strings.TrimSpace(result.Stdout)
	require.Equal(t, "ext4", fsType, "filesystem type should be ext4 after formatting")
}
