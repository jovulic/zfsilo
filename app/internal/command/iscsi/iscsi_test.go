package iscsi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jovulic/zfsilo/app/internal/command/iscsi"
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
	giveZfs   *zfs.ZFS
	giveIscsi *iscsi.ISCSI
	takeIscsi *iscsi.ISCSI
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

func newTestClients(t *testing.T) *testClients {
	giveExecutor := newTestExecutor(t, giveHostConfig)
	takeExecutor := newTestExecutor(t, takeHostConfig)
	return &testClients{
		giveZfs:   zfs.NewZFS(giveExecutor),
		giveIscsi: iscsi.NewISCSI(giveExecutor),
		takeIscsi: iscsi.NewISCSI(takeExecutor),
	}
}

func TestHost_IQN(t *testing.T) {
	type fields struct {
		domain    string
		ownerTime time.Time
		hostname  string
	}
	tests := []struct {
		name   string
		fields fields
		want   iscsi.IQN
	}{
		{
			name: "nominal",
			fields: fields{
				domain:    "linux-iscsi.org",
				ownerTime: time.Date(2003, 1, 1, 0, 0, 0, 0, time.UTC),
				hostname:  "give",
			},
			want: "iqn.2003-01.org.linux-iscsi.give",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := iscsi.NewHost(tt.fields.domain, tt.fields.ownerTime, tt.fields.hostname)
			if got := h.IQN(); got != tt.want {
				t.Errorf("Host.IQN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHost_VolumeIQN(t *testing.T) {
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
		want   iscsi.IQN
	}{
		{
			name: "nominal",
			fields: fields{
				domain:    "linux-iscsi.org",
				ownerTime: time.Date(2003, 1, 1, 0, 0, 0, 0, time.UTC),
				hostname:  "give",
			},
			args: args{
				volumeName: "tank-ivol",
			},
			want: "iqn.2003-01.org.linux-iscsi.give:tank-ivol",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := iscsi.NewHost(tt.fields.domain, tt.fields.ownerTime, tt.fields.hostname)
			if got := tr.VolumeIQN(tt.args.volumeName); got != tt.want {
				t.Errorf("Target.NewIQN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPublishAndUnpublishVolume(t *testing.T) {
	ctx := context.Background()

	clients := newTestClients(t)

	volName := "tank/test-pub-unpub"
	devPath := fmt.Sprintf("/dev/zvol/%s", volName)
	targetIQN := iscsi.IQN("iqn.2003-01.org.linux-iscsi.give:test-pub-unpub")
	creds := iscsi.Credentials{
		UserID:         "userid",
		Password:       "password",
		MutualUserID:   "mutualuserid",
		MutualPassword: "mutualpassword",
	}

	// Create ZFS volume.
	err := clients.giveZfs.CreateVolume(ctx, zfs.CreateVolumeArguments{Name: volName, Size: mb})
	require.NoError(t, err)

	defer func() {
		// Cleanup ZFS volume.
		err := clients.giveZfs.DestroyVolume(ctx, zfs.DestroyVolumeArguments{Name: volName})
		require.NoError(t, err, "zfs volume cleanup failed")
	}()

	// Publish volume.
	err = clients.giveIscsi.PublishVolume(ctx, iscsi.PublishVolumeArguments{
		VolumeName:  "test-pub-unpub",
		DevicePath:  devPath,
		TargetIQN:   targetIQN,
		Credentials: creds,
	})
	require.NoError(t, err)

	// Unpublish volume.
	err = clients.giveIscsi.UnpublishVolume(ctx, iscsi.UnpublishVolumeArguments{
		TargetIQN:  targetIQN,
		VolumeName: "test-pub-unpub",
	})
	require.NoError(t, err)
}

func TestConnectAndDisconnectTarget(t *testing.T) {
	ctx := context.Background()

	clients := newTestClients(t)

	volName := "tank/test-conn-disconn"
	volIdentifier := "test-conn-disconn"
	devPath := fmt.Sprintf("/dev/zvol/%s", volName)
	targetIQN := iscsi.IQN(fmt.Sprintf("iqn.2003-01.org.linux-iscsi.give:%s", volIdentifier))
	creds := iscsi.Credentials{
		UserID:         "userid",
		Password:       "password",
		MutualUserID:   "mutualuserid",
		MutualPassword: "mutualpassword",
	}
	targetEndpoint := "$(dig +short give):3260"

	// Create ZFS volume.
	err := clients.giveZfs.CreateVolume(ctx, zfs.CreateVolumeArguments{Name: volName, Size: mb})
	require.NoError(t, err)

	defer func() {
		// Cleanup ZFS volume.
		err := clients.giveZfs.DestroyVolume(ctx, zfs.DestroyVolumeArguments{Name: volName})
		require.NoError(t, err, "zfs volume cleanup failed")
	}()

	// Publish volume.
	err = clients.giveIscsi.PublishVolume(ctx, iscsi.PublishVolumeArguments{
		VolumeName:  volIdentifier,
		DevicePath:  devPath,
		TargetIQN:   targetIQN,
		Credentials: creds,
	})
	require.NoError(t, err)

	defer func() {
		// Cleanup iSCSI publish.
		err := clients.giveIscsi.UnpublishVolume(ctx, iscsi.UnpublishVolumeArguments{
			TargetIQN:  targetIQN,
			VolumeName: volIdentifier,
		})
		require.NoError(t, err, "iscsi unpublish cleanup failed")
	}()

	// Connect to target.
	err = clients.takeIscsi.ConnectTarget(ctx, iscsi.ConnectTargetArguments{
		TargetIQN:      targetIQN,
		TargetEndpoint: targetEndpoint,
		Credentials:    creds,
	})
	require.NoError(t, err)

	// Disconnect from target.
	err = clients.takeIscsi.DisconnectTarget(ctx, iscsi.DisconnectTargetArguments{
		TargetIQN:      targetIQN,
		TargetEndpoint: targetEndpoint,
	})
	require.NoError(t, err)
}

