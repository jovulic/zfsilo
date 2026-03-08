package service

import (
	"context"
	"fmt"

	"github.com/jovulic/zfsilo/app/internal/command"
	"github.com/jovulic/zfsilo/app/internal/command/fs"
	"github.com/jovulic/zfsilo/app/internal/command/iscsi"
	"github.com/jovulic/zfsilo/app/internal/command/literal"
	"github.com/jovulic/zfsilo/app/internal/command/mount"
	"github.com/jovulic/zfsilo/app/internal/command/nvmeof"
	"github.com/jovulic/zfsilo/app/internal/command/zfs"
	"github.com/jovulic/zfsilo/app/internal/database"
	libcommand "github.com/jovulic/zfsilo/lib/command"
	slogctx "github.com/veqryn/slog-context"
	"gorm.io/gorm"
)

type VolumeSyncer struct {
	database       *gorm.DB
	produceTarget  command.ProduceTarget
	consumeTargets command.ConsumeTargetMap
}

func NewVolumeSyncer(
	database *gorm.DB,
	produceTarget command.ProduceTarget,
	consumeTargets command.ConsumeTargetMap,
) *VolumeSyncer {
	return &VolumeSyncer{
		database:       database,
		produceTarget:  produceTarget,
		consumeTargets: consumeTargets,
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
	exists, err := zfs.With(s.produceTarget.Executor).VolumeExists(ctx, zfs.VolumeExistsArguments{
		Name: volumedb.DatasetID,
	})
	if err != nil {
		return fmt.Errorf("failed to check volume existence: %w", err)
	}

	if exists {
		return nil
	}

	// Create ZFS volume.
	// NOTE: We only check for volume existence currently. In the future we might
	// want to also verify size etc.
	opts := make(map[string]string)
	for _, option := range volumedb.Options.Data() {
		opts[option.Key] = option.Value
	}
	err = zfs.With(s.produceTarget.Executor).CreateVolume(ctx, zfs.CreateVolumeArguments{
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
		err := fs.With(s.produceTarget.Executor).Format(ctx, fs.FormatArguments{
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
	getTargetID := func(volumedb *database.Volume) string {
		if volumedb.TargetID != "" {
			return volumedb.TargetID
		}
		switch volumedb.Transport {
		case database.VolumeTransportISCSI:
			return string(s.produceTarget.Host.VolumeIQN(volumedb.ID))
		case database.VolumeTransportNVMEOF_TCP:
			return string(s.produceTarget.Host.VolumeNQN(volumedb.ID))
		case database.VolumeTransportUNSPECIFIED:
			fallthrough
		default:
			// If transport is not set yet, we can't compute a default ID.
			// But syncPublish usually happens when it SHOULD be published.
			return ""
		}
	}
	checkPublished := func(transport database.VolumeTransport, targetID string) bool {
		if targetID == "" {
			return false
		}
		var path string
		switch transport {
		case database.VolumeTransportISCSI:
			path = fmt.Sprintf("/sys/kernel/config/target/iscsi/%s", targetID)
		case database.VolumeTransportNVMEOF_TCP:
			path = fmt.Sprintf("/sys/kernel/config/nvmet/subsystems/%s", targetID)
		case database.VolumeTransportUNSPECIFIED:
			fallthrough
		default:
			return false
		}
		_, err := literal.With(s.produceTarget.Executor).Run(ctx, fmt.Sprintf("ls -d %s", path))
		return err == nil
	}

	if volumedb.IsPublished() {
		targetID := getTargetID(volumedb)
		isPublished := checkPublished(volumedb.Transport, targetID)
		if !isPublished {
			slogctx.Info(ctx, "publishing volume during sync", "volumeId", volumedb.ID, "transport", volumedb.Transport)
			switch volumedb.Transport {
			case database.VolumeTransportISCSI:
				err := iscsi.With(s.produceTarget.Executor).PublishVolume(ctx, iscsi.PublishVolumeArguments{
					VolumeID:   volumedb.ID,
					DevicePath: volumedb.DevicePathZFS(),
					TargetIQN:  iscsi.IQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to publish iscsi volume: %w", err)
				}
			case database.VolumeTransportNVMEOF_TCP:
				err := nvmeof.With(s.produceTarget.Executor).PublishVolume(ctx, nvmeof.PublishVolumeArguments{
					VolumeID:   volumedb.ID,
					DevicePath: volumedb.DevicePathZFS(),
					TargetNQN:  nvmeof.NQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to publish nvmeof volume: %w", err)
				}
			case database.VolumeTransportUNSPECIFIED:
				fallthrough
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}
	} else {
		// Even if not intended to be published, check if it IS published and clean up.
		// Note: We might need to check both transports if we don't know which one was used.
		// For simplicity, we check the current transport if set, or just skip.
		targetID := getTargetID(volumedb)
		isPublished := checkPublished(volumedb.Transport, targetID)
		if isPublished {
			slogctx.Info(ctx, "unpublishing volume during sync", "volumeId", volumedb.ID)
			switch volumedb.Transport {
			case database.VolumeTransportISCSI:
				err := iscsi.With(s.produceTarget.Executor).UnpublishVolume(ctx, iscsi.UnpublishVolumeArguments{
					VolumeID:  volumedb.ID,
					TargetIQN: iscsi.IQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to unpublish iscsi volume: %w", err)
				}
			case database.VolumeTransportNVMEOF_TCP:
				err := nvmeof.With(s.produceTarget.Executor).UnpublishVolume(ctx, nvmeof.UnpublishVolumeArguments{
					TargetNQN: nvmeof.NQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to unpublish nvmeof volume: %w", err)
				}
			case database.VolumeTransportUNSPECIFIED:
				fallthrough
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}
	}

	return nil
}

func (s *VolumeSyncer) syncConnect(ctx context.Context, volumedb *database.Volume) error {
	getTargetID := func(volumedb *database.Volume) string {
		if volumedb.TargetID != "" {
			return volumedb.TargetID
		}
		switch volumedb.Transport {
		case database.VolumeTransportISCSI:
			return string(s.produceTarget.Host.VolumeIQN(volumedb.ID))
		case database.VolumeTransportNVMEOF_TCP:
			return string(s.produceTarget.Host.VolumeNQN(volumedb.ID))
		case database.VolumeTransportUNSPECIFIED:
			fallthrough
		default:
			return ""
		}
	}
	checkAuthorized := func(transport database.VolumeTransport, targetID, clientID string) bool {
		var path string
		switch transport {
		case database.VolumeTransportISCSI:
			path = fmt.Sprintf("/sys/kernel/config/target/iscsi/%s/tpgt_1/acls/%s", targetID, clientID)
		case database.VolumeTransportNVMEOF_TCP:
			path = fmt.Sprintf("/sys/kernel/config/nvmet/subsystems/%s/allowed_hosts/%s", targetID, clientID)
		case database.VolumeTransportUNSPECIFIED:
			fallthrough
		default:
			return false
		}
		_, err := literal.With(s.produceTarget.Executor).Run(ctx, fmt.Sprintf("ls -d %s", path))
		return err == nil
	}
	checkConnected := func(transport database.VolumeTransport, consumer libcommand.Executor, targetID string) bool {
		var cmd string
		switch transport {
		case database.VolumeTransportISCSI:
			cmd = fmt.Sprintf("iscsiadm -m session | grep -q %s", targetID)
		case database.VolumeTransportNVMEOF_TCP:
			cmd = fmt.Sprintf("nvme list-subsys -n %s", targetID)
		case database.VolumeTransportUNSPECIFIED:
			fallthrough
		default:
			return false
		}
		_, err := literal.With(consumer).Run(ctx, cmd)
		return err == nil
	}

	// If we don't have a client, we can't connect.
	if volumedb.ClientID == "" {
		return nil
	}

	if volumedb.IsConnected() {
		consumeTarget, ok := s.consumeTargets[volumedb.ClientID]
		if !ok {
			return fmt.Errorf("unknown consume target: %s", volumedb.ClientID)
		}

		targetID := getTargetID(volumedb)

		// Reconcile authorization.
		isAuthorized := checkAuthorized(volumedb.Transport, targetID, volumedb.ClientID)
		if !isAuthorized {
			slogctx.Info(ctx, "authorizing client during sync", "volumeId", volumedb.ID, "clientId", volumedb.ClientID)
			switch volumedb.Transport {
			case database.VolumeTransportISCSI:
				err := iscsi.With(s.produceTarget.Executor).Authorize(ctx, iscsi.AuthorizeArguments{
					TargetIQN:         iscsi.IQN(targetID),
					InitiatorIQN:      iscsi.IQN(consumeTarget.ID),
					InitiatorPassword: consumeTarget.Password,
					TargetPassword:    s.produceTarget.Password,
				})
				if err != nil {
					return fmt.Errorf("failed to authorize iscsi client: %w", err)
				}
			case database.VolumeTransportNVMEOF_TCP:
				err := nvmeof.With(s.produceTarget.Executor).Authorize(ctx, nvmeof.AuthorizeArguments{
					TargetNQN:         nvmeof.NQN(targetID),
					InitiatorNQN:      nvmeof.NQN(consumeTarget.ID),
					InitiatorPassword: consumeTarget.Password,
					TargetPassword:    s.produceTarget.Password,
				})
				if err != nil {
					return fmt.Errorf("failed to authorize nvmeof client: %w", err)
				}
			case database.VolumeTransportUNSPECIFIED:
				fallthrough
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}

		// Reconcile connection.
		isConnected := checkConnected(volumedb.Transport, consumeTarget.Executor, targetID)
		if !isConnected {
			slogctx.Info(ctx, "connecting volume during sync", "volumeId", volumedb.ID)
			switch volumedb.Transport {
			case database.VolumeTransportISCSI:
				err := iscsi.With(consumeTarget.Executor).ConnectTarget(ctx, iscsi.ConnectTargetArguments{
					TargetIQN:         iscsi.IQN(targetID),
					TargetAddress:     volumedb.TargetAddress,
					InitiatorIQN:      iscsi.IQN(consumeTarget.ID),
					InitiatorPassword: consumeTarget.Password,
					TargetPassword:    s.produceTarget.Password,
				})
				if err != nil {
					return fmt.Errorf("failed to connect iscsi volume: %w", err)
				}
			case database.VolumeTransportNVMEOF_TCP:
				err := nvmeof.With(consumeTarget.Executor).ConnectTarget(ctx, nvmeof.ConnectTargetArguments{
					TargetNQN:         nvmeof.NQN(targetID),
					TargetAddress:     volumedb.TargetAddress,
					InitiatorNQN:      nvmeof.NQN(consumeTarget.ID),
					InitiatorPassword: consumeTarget.Password,
					TargetPassword:    s.produceTarget.Password,
				})
				if err != nil {
					return fmt.Errorf("failed to connect nvmeof volume: %w", err)
				}
			case database.VolumeTransportUNSPECIFIED:
				fallthrough
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}
	} else {
		consumeTarget, ok := s.consumeTargets[volumedb.ClientID]
		if !ok {
			return fmt.Errorf("unknown consume target: %s", volumedb.ClientID)
		}

		targetID := getTargetID(volumedb)

		// Reconcile connection.
		isConnected := checkConnected(volumedb.Transport, consumeTarget.Executor, targetID)
		if isConnected {
			slogctx.Info(ctx, "disconnecting volume during sync", "volumeId", volumedb.ID)
			switch volumedb.Transport {
			case database.VolumeTransportISCSI:
				err := iscsi.With(consumeTarget.Executor).DisconnectTarget(ctx, iscsi.DisconnectTargetArguments{
					TargetIQN:     iscsi.IQN(targetID),
					TargetAddress: volumedb.TargetAddress,
				})
				if err != nil {
					return fmt.Errorf("failed to disconnect iscsi volume: %w", err)
				}
			case database.VolumeTransportNVMEOF_TCP:
				err := nvmeof.With(consumeTarget.Executor).DisconnectTarget(ctx, nvmeof.DisconnectTargetArguments{
					TargetNQN: nvmeof.NQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to disconnect nvmeof volume: %w", err)
				}
			case database.VolumeTransportUNSPECIFIED:
				fallthrough
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}

		// Reconcile authorization.
		isAuthorized := checkAuthorized(volumedb.Transport, targetID, volumedb.ClientID)
		if isAuthorized {
			slogctx.Info(ctx, "unauthorizing client during sync", "volumeId", volumedb.ID, "clientId", volumedb.ClientID)
			switch volumedb.Transport {
			case database.VolumeTransportISCSI:
				err := iscsi.With(s.produceTarget.Executor).Unauthorize(ctx, iscsi.UnauthorizeArguments{
					TargetIQN:    iscsi.IQN(targetID),
					InitiatorIQN: iscsi.IQN(volumedb.ClientID),
				})
				if err != nil {
					return fmt.Errorf("failed to unauthorize iscsi client: %w", err)
				}
			case database.VolumeTransportNVMEOF_TCP:
				err := nvmeof.With(s.produceTarget.Executor).Unauthorize(ctx, nvmeof.UnauthorizeArguments{
					TargetNQN:    nvmeof.NQN(targetID),
					InitiatorNQN: nvmeof.NQN(volumedb.ClientID),
				})
				if err != nil {
					return fmt.Errorf("failed to unauthorize nvmeof client: %w", err)
				}
			case database.VolumeTransportUNSPECIFIED:
				fallthrough
			default:
				return fmt.Errorf("no transport specified on volume")
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

	// If no client we can't connect. If no mount path we can't check.
	if volumedb.ClientID == "" || volumedb.MountPath == "" {
		return nil
	}

	if volumedb.IsMounted() {
		consumer, ok := s.consumeTargets[volumedb.ClientID]
		if !ok {
			return fmt.Errorf("unknown consumer: %s", volumedb.ClientID)
		}

		isMounted := checkMounted(consumer.Executor, volumedb.MountPath)
		if !isMounted {
			slogctx.Info(ctx, "mounting volume during sync", "volumeId", volumedb.ID)

			devicePath, err := volumedb.DevicePathClient()
			if err != nil {
				return fmt.Errorf("failed to get device path: %w", err)
			}

			// Prepare mount path.
			switch volumedb.Mode {
			case database.VolumeModeBLOCK:
				_, err := literal.With(consumer.Executor).Run(ctx, fmt.Sprintf("install -m 0644 /dev/null %s", volumedb.MountPath))
				if err != nil {
					return fmt.Errorf("failed to touch mount path: %w", err)
				}
				err = mount.With(consumer.Executor).Mount(ctx, mount.MountArguments{
					SourcePath: devicePath,
					TargetPath: volumedb.MountPath,
					Options:    []string{"bind"},
				})
				if err != nil {
					return fmt.Errorf("failed to mount volume: %w", err)
				}
			case database.VolumeModeFILESYSTEM:
				_, err := literal.With(consumer.Executor).Run(ctx, fmt.Sprintf("mkdir -m 0750 -p %s", volumedb.MountPath))
				if err != nil {
					return fmt.Errorf("failed to touch mount path: %w", err)
				}
				err = mount.With(consumer.Executor).Mount(ctx, mount.MountArguments{
					SourcePath: devicePath,
					TargetPath: volumedb.MountPath,
					FSType:     "ext4",
					Options:    []string{"defaults"},
				})
				if err != nil {
					return fmt.Errorf("failed to mount volume: %w", err)
				}

				_, err = literal.With(consumer.Executor).Run(ctx, fmt.Sprintf("chmod 0777 %s", volumedb.MountPath))
				if err != nil {
					return fmt.Errorf("failed to chmod mount path: %w", err)
				}
			case database.VolumeModeUNSPECIFIED:
				fallthrough
			default:
				return fmt.Errorf("unsupported volume mode: %s", volumedb.Mode)
			}
		}
	} else {
		consumer, ok := s.consumeTargets[volumedb.ClientID]
		if !ok {
			return fmt.Errorf("unknown consumer: %s", volumedb.ClientID)
		}

		isMounted := checkMounted(consumer.Executor, volumedb.MountPath)
		if isMounted {
			slogctx.Info(ctx, "unmounting volume during sync", "volumeId", volumedb.ID)
			err := mount.With(consumer.Executor).Umount(ctx, mount.UmountArguments{
				Path: volumedb.MountPath,
			})
			if err != nil {
				return fmt.Errorf("failed to unmount volume: %w", err)
			}
		}
	}

	return nil
}
