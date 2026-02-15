package service

import (
	"context"
	"fmt"

	"github.com/jovulic/zfsilo/app/internal/command"
	"github.com/jovulic/zfsilo/app/internal/command/fs"
	"github.com/jovulic/zfsilo/app/internal/command/iscsi"
	"github.com/jovulic/zfsilo/app/internal/command/literal"
	"github.com/jovulic/zfsilo/app/internal/command/mount"
	"github.com/jovulic/zfsilo/app/internal/command/zfs"
	"github.com/jovulic/zfsilo/app/internal/database"
	libcommand "github.com/jovulic/zfsilo/lib/command"
	slogctx "github.com/veqryn/slog-context"
	"gorm.io/gorm"
)

type VolumeSyncer struct {
	database    *gorm.DB
	producer    command.ProduceExecutor
	consumers   command.ConsumeExecutorMap
	host        *iscsi.Host
	credentials iscsi.Credentials
}

func NewVolumeSyncer(
	database *gorm.DB,
	producer command.ProduceExecutor,
	consumers command.ConsumeExecutorMap,
	host *iscsi.Host,
	credentials iscsi.Credentials,
) *VolumeSyncer {
	return &VolumeSyncer{
		database:    database,
		producer:    producer,
		consumers:   consumers,
		host:        host,
		credentials: credentials,
	}
}

func (s *VolumeSyncer) Sync(ctx context.Context, volumedb *database.Volume) error {
	if err := s.syncZFS(ctx, volumedb); err != nil {
		return fmt.Errorf("failed to sync zfs: %w", err)
	}

	if err := s.syncPublish(ctx, volumedb); err != nil {
		return fmt.Errorf("failed to sync publish: %w", err)
	}

	if err := s.syncConnect(ctx, volumedb); err != nil {
		return fmt.Errorf("failed to sync connect: %w", err)
	}

	if err := s.syncMount(ctx, volumedb); err != nil {
		return fmt.Errorf("failed to sync mount: %w", err)
	}

	return nil
}

func (s *VolumeSyncer) syncZFS(ctx context.Context, volumedb *database.Volume) error {
	exists, err := zfs.With(s.producer).VolumeExists(ctx, zfs.VolumeExistsArguments{
		Name: volumedb.DatasetID,
	})
	if err != nil {
		return fmt.Errorf("failed to check volume existence: %w", err)
	}

	if exists {
		return nil
	}

	// Create ZFS volume.
	// NOTE: We only check for volume existance currently. In the future we might
	// want to also verify size etc.
	opts := make(map[string]string)
	for _, option := range volumedb.Options.Data() {
		opts[option.Key] = option.Value
	}
	err = zfs.With(s.producer).CreateVolume(ctx, zfs.CreateVolumeArguments{
		Name:    volumedb.DatasetID,
		Size:    uint64(volumedb.CapacityBytes),
		Options: opts,
		Sparse:  volumedb.Sparse,
	})
	if err != nil {
		return fmt.Errorf("failed to create zfs volume: %w", err)
	}

	// Format if filesystem.
	if volumedb.Mode == database.VolumeModeFILESYSTEM {
		err := fs.With(s.producer).Format(ctx, fs.FormatArguments{
			Device:        volumedb.DevicePathZFS(),
			WaitForDevice: true,
		})
		if err != nil {
			return fmt.Errorf("failed to format zfs volume: %w", err)
		}
	}

	return nil
}

func (s *VolumeSyncer) syncPublish(ctx context.Context, volumedb *database.Volume) error {
	getTargetIQN := func(volumedb *database.Volume) string {
		// We try to pull the target iqn via the volume field, but if we fail that,
		// we compute what it would be with the configured host.
		if volumedb.TargetIQN == "" {
			return volumedb.TargetIQN
		}
		return string(s.host.VolumeIQN(volumedb.ID))
	}
	checkPublished := func(targetIQN string) bool {
		// If err != nil, likely does not exist (ls returns non-zero).
		_, err := literal.With(s.producer).Run(ctx, fmt.Sprintf("ls -d %s", database.BuildDevicePathISCSIServer(targetIQN)))
		return err == nil
	}

	if volumedb.IsPublished() {
		targetIQN := getTargetIQN(volumedb)
		isPublished := checkPublished(targetIQN)
		if !isPublished {
			slogctx.Info(ctx, "publishing volume during sync", "volumeId", volumedb.ID)
			err := iscsi.With(s.producer).PublishVolume(ctx, iscsi.PublishVolumeArguments{
				VolumeID:    volumedb.ID,
				DevicePath:  volumedb.DevicePathZFS(),
				TargetIQN:   iscsi.IQN(targetIQN),
				Credentials: s.credentials,
			})
			if err != nil {
				return fmt.Errorf("failed to publish volume: %w", err)
			}
		}
	} else {
		targetIQN := getTargetIQN(volumedb)
		isPublished := checkPublished(targetIQN)
		if isPublished {
			slogctx.Info(ctx, "unpublishing volume during sync", "volumeId", volumedb.ID)
			err := iscsi.With(s.producer).UnpublishVolume(ctx, iscsi.UnpublishVolumeArguments{
				VolumeID:  volumedb.ID,
				TargetIQN: iscsi.IQN(targetIQN),
			})
			if err != nil {
				return fmt.Errorf("failed to unpublish volume: %w", err)
			}
		}
	}

	return nil
}

func (s *VolumeSyncer) syncConnect(ctx context.Context, volumedb *database.Volume) error {
	getTargetIQN := func(volumedb *database.Volume) string {
		// We try to pull the target iqn via the volume field, but if we fail that,
		// we compute what it would be with the configured host.
		if volumedb.TargetIQN == "" {
			return volumedb.TargetIQN
		}
		return string(s.host.VolumeIQN(volumedb.ID))
	}
	checkConnected := func(consumer libcommand.Executor, targetIQN string) bool {
		// iscsiadm -m session returns list. We grep for target IQN.
		_, err := literal.With(consumer).Run(ctx, fmt.Sprintf("iscsiadm -m session | grep -q %s", targetIQN))
		return err == nil
	}

	// If we don't have an initiator, we can't connect.
	if volumedb.InitiatorIQN == "" {
		return nil
	}

	if volumedb.IsConnected() {
		consumer, ok := s.consumers[volumedb.InitiatorIQN]
		if !ok {
			return fmt.Errorf("unknown consumer: %s", volumedb.InitiatorIQN)
		}

		targetIQN := getTargetIQN(volumedb)
		isConnected := checkConnected(consumer, targetIQN)
		if !isConnected {
			slogctx.Info(ctx, "connecting volume during sync", "volumeId", volumedb.ID)
			err := iscsi.With(consumer).ConnectTarget(ctx, iscsi.ConnectTargetArguments{
				TargetIQN:     iscsi.IQN(targetIQN),
				TargetAddress: volumedb.TargetAddress,
				Credentials:   s.credentials,
			})
			if err != nil {
				return fmt.Errorf("failed to connect volume: %w", err)
			}

		}

	} else {
		consumer, ok := s.consumers[volumedb.InitiatorIQN]
		if !ok {
			return fmt.Errorf("unknown consumer: %s", volumedb.InitiatorIQN)
		}

		targetIQN := getTargetIQN(volumedb)
		isConnected := checkConnected(consumer, targetIQN)
		if isConnected {
			slogctx.Info(ctx, "disconnecting volume during sync", "volumeId", volumedb.ID)
			err := iscsi.With(consumer).DisconnectTarget(ctx, iscsi.DisconnectTargetArguments{
				TargetIQN:     iscsi.IQN(targetIQN),
				TargetAddress: volumedb.TargetAddress,
			})
			if err != nil {
				return fmt.Errorf("failed to disconnect volume: %w", err)
			}
		}
	}

	return nil
}

func (s *VolumeSyncer) syncMount(ctx context.Context, volumedb *database.Volume) error {
	// NOTE: We likely should check if the mount check failed for other reasons,
	// but this syncs it up with the other check commands in semantics.
	checkMounted := func(consumer libcommand.Executor, mountPath string) bool {
		isMounted, _ := mount.With(consumer).IsMounted(ctx, mountPath)
		return isMounted
	}

	// If no initiator we can't connect. If no mount path we can't check.
	if volumedb.InitiatorIQN == "" || volumedb.MountPath == "" {
		return nil
	}

	if volumedb.IsMounted() {
		consumer, ok := s.consumers[volumedb.InitiatorIQN]
		if !ok {
			return fmt.Errorf("unknown consumer: %s", volumedb.InitiatorIQN)
		}

		isMounted := checkMounted(consumer, volumedb.MountPath)
		if !isMounted {
			slogctx.Info(ctx, "mounting volume during sync", "volumeId", volumedb.ID)

			// Prepare mount path.
			switch volumedb.Mode {
			case database.VolumeModeBLOCK:
				_, err := literal.With(consumer).Run(ctx, fmt.Sprintf("install -m 0644 /dev/null %s", volumedb.MountPath))
				if err != nil {
					return fmt.Errorf("failed to touch mount path: %w", err)
				}
				err = mount.With(consumer).Mount(ctx, mount.MountArguments{
					SourcePath: volumedb.DevicePathISCSIClient(),
					TargetPath: volumedb.MountPath,
					Options:    []string{"bind"},
				})
				if err != nil {
					return fmt.Errorf("failed to mount volume: %w", err)
				}
			case database.VolumeModeFILESYSTEM:
				_, err := literal.With(consumer).Run(ctx, fmt.Sprintf("mkdir -m 0750 -p %s", volumedb.MountPath))
				if err != nil {
					return fmt.Errorf("failed to touch mount path: %w", err)
				}
				err = mount.With(consumer).Mount(ctx, mount.MountArguments{
					SourcePath: volumedb.DevicePathISCSIClient(),
					TargetPath: volumedb.MountPath,
					FSType:     "ext4",
					Options:    []string{"defaults"},
				})
				if err != nil {
					return fmt.Errorf("failed to mount volume: %w", err)
				}

				_, err = literal.With(consumer).Run(ctx, fmt.Sprintf("chmod 0777 %s", volumedb.MountPath))
				if err != nil {
					return fmt.Errorf("failed to chmod mount path: %w", err)
				}
			}
		}
	} else {
		consumer, ok := s.consumers[volumedb.InitiatorIQN]
		if !ok {
			return fmt.Errorf("unknown consumer: %s", volumedb.InitiatorIQN)
		}

		isMounted := checkMounted(consumer, volumedb.MountPath)
		if isMounted {
			slogctx.Info(ctx, "unmounting volume during sync", "volumeId", volumedb.ID)
			err := mount.With(consumer).Umount(ctx, mount.UmountArguments{
				Path: volumedb.MountPath,
			})
			if err != nil {
				return fmt.Errorf("failed to unmount volume: %w", err)
			}
		}
	}

	return nil
}
