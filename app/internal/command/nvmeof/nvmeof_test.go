package nvmeof_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jovulic/zfsilo/app/internal/command/lib/host"
	"github.com/jovulic/zfsilo/app/internal/command/nvmeof"
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

var takeHostConfig = command.RemoteExecutorConfig{
	Address:  "localhost",
	Port:     9100,
	Username: "root",
	Password: "",
}

type testClients struct {
	giveZfs    zfs.ZFS
	giveNVMeOF nvmeof.NVMeOF
	takeNVMeOF nvmeof.NVMeOF
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

func newTestClients(t *testing.T) *testClients {
	if testing.Short() {
		t.Skip("skipping test that requires remote infrastructure in short mode")
	}

	giveExecutor := newTestExecutor(t, giveHostConfig)
	takeExecutor := newTestExecutor(t, takeHostConfig)
	return &testClients{
		giveZfs:    zfs.With(giveExecutor),
		giveNVMeOF: nvmeof.With(giveExecutor),
		takeNVMeOF: nvmeof.With(takeExecutor),
	}
}

func TestHost_NQN(t *testing.T) {
	type fields struct {
		domain    string
		ownerTime time.Time
		hostname  string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{
			name: "nominal",
			fields: fields{
				domain:    "nvmexpress.org",
				ownerTime: time.Date(2014, 8, 1, 0, 0, 0, 0, time.UTC),
				hostname:  "give",
			},
			want: "nqn.2014-08.org.nvmexpress:give",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := host.New(tt.fields.domain, tt.fields.ownerTime, tt.fields.hostname)
			if got := h.NQN(); got != tt.want {
				t.Errorf("Host.NQN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHost_VolumeNQN(t *testing.T) {
	type fields struct {
		domain    string
		ownerTime time.Time
		hostname  string
	}
	type args struct {
		volumeName string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   string
	}{
		{
			name: "nominal",
			fields: fields{
				domain:    "nvmexpress.org",
				ownerTime: time.Date(2014, 8, 1, 0, 0, 0, 0, time.UTC),
				hostname:  "give",
			},
			args: args{
				volumeName: "tank-ivol",
			},
			want: "nqn.2014-08.org.nvmexpress:give:tank-ivol",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := host.New(tt.fields.domain, tt.fields.ownerTime, tt.fields.hostname)
			if got := tr.VolumeNQN(tt.args.volumeName); got != tt.want {
				t.Errorf("Target.VolumeNQN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPublishAndUnpublishVolume(t *testing.T) {
	ctx := context.Background()

	clients := newTestClients(t)

	volName := "tank/test-nvme-pub-unpub"
	devPath := fmt.Sprintf("/dev/zvol/%s", volName)
	targetNQN := nvmeof.NQN("nqn.2014-08.org.nvmexpress:give:test-nvme-pub-unpub")

	// Create ZFS volume.
	err := clients.giveZfs.CreateVolume(ctx, zfs.CreateVolumeArguments{Name: volName, Size: mb})
	require.NoError(t, err)

	defer func() {
		// Cleanup ZFS volume.
		err := clients.giveZfs.DestroyVolume(ctx, zfs.DestroyVolumeArguments{Name: volName})
		require.NoError(t, err, "zfs volume cleanup failed")
	}()

	// Publish volume.
	err = clients.giveNVMeOF.PublishVolume(ctx, nvmeof.PublishVolumeArguments{
		VolumeID:   "test-nvme-pub-unpub",
		DevicePath: devPath,
		TargetNQN:  targetNQN,
	})
	require.NoError(t, err)

	// Unpublish volume.
	err = clients.giveNVMeOF.UnpublishVolume(ctx, nvmeof.UnpublishVolumeArguments{
		TargetNQN: targetNQN,
	})
	require.NoError(t, err)
}

func TestConnectAndDisconnectTarget(t *testing.T) {
	ctx := context.Background()

	clients := newTestClients(t)

	volName := "tank/test-nvme-conn-disconn"
	volIdentifier := "test-nvme-conn-disconn"
	devPath := fmt.Sprintf("/dev/zvol/%s", volName)
	targetNQN := nvmeof.NQN(fmt.Sprintf("nqn.2014-08.org.nvmexpress:give:%s", volIdentifier))
	initiatorNQN := nvmeof.NQN("nqn.2014-08.org.nvmexpress:take")
	initiatorPassword := "DHHC-1:00:aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkMTIzNDU=:"
	targetPassword := "DHHC-1:00:bXV0dWFsaGVsbG93b3JsZG11dHVhbGhlbGxvd29ybGQxMjM=:"
	targetEndpoint := "$(dig +short give)"

	// Create ZFS volume.
	err := clients.giveZfs.CreateVolume(ctx, zfs.CreateVolumeArguments{Name: volName, Size: mb})
	require.NoError(t, err)

	defer func() {
		// Cleanup ZFS volume.
		err := clients.giveZfs.DestroyVolume(ctx, zfs.DestroyVolumeArguments{Name: volName})
		require.NoError(t, err, "zfs volume cleanup failed")
	}()

	// Publish volume.
	err = clients.giveNVMeOF.PublishVolume(ctx, nvmeof.PublishVolumeArguments{
		VolumeID:   volIdentifier,
		DevicePath: devPath,
		TargetNQN:  targetNQN,
	})
	require.NoError(t, err)

	defer func() {
		// Cleanup NVMe publish.
		err := clients.giveNVMeOF.UnpublishVolume(ctx, nvmeof.UnpublishVolumeArguments{
			TargetNQN: targetNQN,
		})
		require.NoError(t, err, "nvmeof unpublish cleanup failed")
	}()

	// Authorize initiator on target side.
	err = clients.giveNVMeOF.Authorize(ctx, nvmeof.AuthorizeArguments{
		TargetNQN:         targetNQN,
		InitiatorNQN:      initiatorNQN,
		InitiatorPassword: initiatorPassword,
		TargetPassword:    targetPassword,
	})
	require.NoError(t, err)

	defer func() {
		// Cleanup NVMe authorization.
		err := clients.giveNVMeOF.Unauthorize(ctx, nvmeof.UnauthorizeArguments{
			TargetNQN:    targetNQN,
			InitiatorNQN: initiatorNQN,
		})
		require.NoError(t, err, "nvmeof unauthorize cleanup failed")
	}()

	// Connect to target.
	err = clients.takeNVMeOF.ConnectTarget(ctx, nvmeof.ConnectTargetArguments{
		TargetNQN:         targetNQN,
		TargetAddress:     targetEndpoint,
		InitiatorNQN:      initiatorNQN,
		InitiatorPassword: initiatorPassword,
		TargetPassword:    targetPassword,
	})
	require.NoError(t, err)

	// Disconnect from target.
	err = clients.takeNVMeOF.DisconnectTarget(ctx, nvmeof.DisconnectTargetArguments{
		TargetNQN: targetNQN,
	})
	require.NoError(t, err)
}
