package zfs_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jovulic/zfsilo/app/internal/command/zfs"
	"github.com/jovulic/zfsilo/lib/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestZFSClient(t *testing.T) zfs.ZFS {
	// This connects to the 'give' vm, which is tunneled to localhost:9000
	// by the dev just recipe.
	// NOTE: This assumes that the test VM is running and accessible.
	// Run `just dev` in a separate terminal.
	// NOTE: This also assumes that the root user can log in without a password,
	// or that the SSH client is configured to use a key.
	executor := command.NewRemoteExecutor(command.RemoteExecutorConfig{
		Address:  "localhost",
		Port:     9000,
		Username: "root",
		Password: "",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := executor.Startup(ctx); err != nil {
		t.Fatalf("failed to start remote executor: %v", err)
	}

	t.Cleanup(func() {
		executor.Shutdown(context.Background())
	})

	return zfs.With(executor)
}

func TestCreateAndDestroyVolume(t *testing.T) {
	client := getTestZFSClient(t)

	volName := "tank/testvol-" + fmt.Sprintf("%d", time.Now().UnixNano())
	volSize := uint64(1024 * 1024 * 10) // 10MB

	// Create the volume.
	createArgs := zfs.CreateVolumeArguments{
		Name: volName,
		Size: volSize,
	}
	err := client.CreateVolume(context.Background(), createArgs)
	require.NoError(t, err, "failed to create volume")

	// Verify the volume exists.
	exists, err := client.VolumeExists(context.Background(), zfs.VolumeExistsArguments{Name: volName})
	require.NoError(t, err, "failed to check if volume exists after creation")
	assert.True(t, exists, "volume should exist after creation")

	// Destroy the volume.
	destroyArgs := zfs.DestroyVolumeArguments{
		Name: volName,
	}
	err = client.DestroyVolume(context.Background(), destroyArgs)
	require.NoError(t, err, "failed to destroy volume")

	// Verify the volume is gone.
	exists, err = client.VolumeExists(context.Background(), zfs.VolumeExistsArguments{Name: volName})
	require.NoError(t, err, "failed to check if volume exists after destruction")
	assert.False(t, exists, "volume should not exist after destruction")
}
