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
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type VolumeSyncer struct {
	database        *gorm.DB
	executorFactory *command.ExecutorFactory
}

func NewVolumeSyncer(
	database *gorm.DB,
	executorFactory *command.ExecutorFactory,
) *VolumeSyncer {
	return &VolumeSyncer{
		database:        database,
		executorFactory: executorFactory,
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

	if err := s.syncStage(ctx, volumedb); err != nil {
		return fmt.Errorf("failed to sync stage: %w", err)
	}

	if err := s.syncMount(ctx, volumedb); err != nil {
		return fmt.Errorf("failed to sync mount: %w", err)
	}

	return nil
}

func (s *VolumeSyncer) getExecutorForHost(ctx context.Context, hostID string) (libcommand.Executor, *database.Host, error) {
	if hostID == "" {
		return nil, nil, fmt.Errorf("host ID is empty")
	}
	// We search by ID, name, or any of the IDs in the JSON list.
	// NOTE: We use SQLite specific JSON function here.
	host, err := gorm.G[*database.Host](s.database).Where("id = ? OR name = ? OR EXISTS (SELECT 1 FROM json_each(identifiers) WHERE value = ?)", hostID, hostID, hostID).First(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get host %s: %w", hostID, err)
	}
	executor, err := s.executorFactory.BuildExecutor(host)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build executor for host %s: %w", hostID, err)
	}
	return executor, host, nil
}

func (s *VolumeSyncer) syncZFS(ctx context.Context, volumedb *database.Volume) error {
	if volumedb.ServerHost == "" {
		return nil
	}
	executor, _, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
	if err != nil {
		return err
	}

	exists, err := zfs.With(executor).VolumeExists(ctx, zfs.VolumeExistsArguments{
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
	err = zfs.With(executor).CreateVolume(ctx, zfs.CreateVolumeArguments{
		Name:    volumedb.DatasetID,
		Size:    uint64(volumedb.CapacityBytes),
		Options: opts,
		Sparse:  volumedb.Sparse,
	})
	if err != nil {
		return fmt.Errorf("failed to create zfs volume: %w", err)
	}

	return nil
}

func (s *VolumeSyncer) syncPublish(ctx context.Context, volumedb *database.Volume) error {
	if volumedb.ServerHost == "" {
		return nil
	}
	executor, host, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
	if err != nil {
		return err
	}

	getTargetID := func(volumedb *database.Volume, host *database.Host) string {
		transport := volumedb.Transport.Data()
		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			id, _ := host.VolumeIQN(volumedb.ID)
			return id
		case database.VolumeTransportTypeNVMEOF_TCP:
			id, _ := host.VolumeNQN(volumedb.ID)
			return id
		case database.VolumeTransportTypeUNSPECIFIED:
			return ""
		default:
			return ""
		}
	}
	checkPublished := func(transport database.VolumeTransport, targetID string) bool {
		if targetID == "" {
			return false
		}
		var path string
		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			path = fmt.Sprintf("/sys/kernel/config/target/iscsi/%s", targetID)
		case database.VolumeTransportTypeNVMEOF_TCP:
			path = fmt.Sprintf("/sys/kernel/config/nvmet/subsystems/%s", targetID)
		case database.VolumeTransportTypeUNSPECIFIED:
			return false
		default:
			return false
		}
		_, err := literal.With(executor).Run(ctx, fmt.Sprintf("ls -d %s", path))
		return err == nil
	}

	transport := volumedb.Transport.Data()
	targetID := getTargetID(volumedb, host)
	if volumedb.IsPublished() {
		isPublished := checkPublished(transport, targetID)
		if !isPublished {
			slogctx.Info(ctx, "publishing volume during sync", "volumeId", volumedb.ID, "transport", transport.Type)
			switch transport.Type {
			case database.VolumeTransportTypeISCSI:
				err := iscsi.With(executor).PublishVolume(ctx, iscsi.PublishVolumeArguments{
					VolumeID:   volumedb.ID,
					DevicePath: volumedb.DevicePathZFS(),
					TargetIQN:  iscsi.IQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to publish iscsi volume: %w", err)
				}
			case database.VolumeTransportTypeNVMEOF_TCP:
				err := nvmeof.With(executor).PublishVolume(ctx, nvmeof.PublishVolumeArguments{
					VolumeID:   volumedb.ID,
					DevicePath: volumedb.DevicePathZFS(),
					TargetNQN:  nvmeof.NQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to publish nvmeof volume: %w", err)
				}
			case database.VolumeTransportTypeUNSPECIFIED:
				return fmt.Errorf("no transport specified for volume publish")
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}
	} else {
		isPublished := checkPublished(transport, targetID)
		if isPublished {
			slogctx.Info(ctx, "unpublishing volume during sync", "volumeId", volumedb.ID)
			switch transport.Type {
			case database.VolumeTransportTypeISCSI:
				err := iscsi.With(executor).UnpublishVolume(ctx, iscsi.UnpublishVolumeArguments{
					VolumeID:  volumedb.ID,
					TargetIQN: iscsi.IQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to unpublish iscsi volume: %w", err)
				}
			case database.VolumeTransportTypeNVMEOF_TCP:
				err := nvmeof.With(executor).UnpublishVolume(ctx, nvmeof.UnpublishVolumeArguments{
					TargetNQN: nvmeof.NQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to unpublish nvmeof volume: %w", err)
				}
			case database.VolumeTransportTypeUNSPECIFIED:
				return fmt.Errorf("no transport specified for volume publish")
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}
	}

	return nil
}

func (s *VolumeSyncer) syncConnect(ctx context.Context, volumedb *database.Volume) error {
	if volumedb.ServerHost == "" || volumedb.ClientHost == "" {
		return nil
	}
	publishExecutor, publishHost, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
	if err != nil {
		return err
	}
	connectExecutor, connectHost, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
	if err != nil {
		return err
	}

	getTargetID := func(volumedb *database.Volume, host *database.Host) string {
		switch volumedb.Transport.Data().Type {
		case database.VolumeTransportTypeISCSI:
			id, _ := host.VolumeIQN(volumedb.ID)
			return id
		case database.VolumeTransportTypeNVMEOF_TCP:
			id, _ := host.VolumeNQN(volumedb.ID)
			return id
		case database.VolumeTransportTypeUNSPECIFIED:
			return ""
		default:
			return ""
		}
	}
	getClientID := func(transport datatypes.JSONType[database.VolumeTransport], host *database.Host) string {
		switch transport.Data().Type {
		case database.VolumeTransportTypeISCSI:
			id, _ := host.IQN()
			return id
		case database.VolumeTransportTypeNVMEOF_TCP:
			id, _ := host.NQN()
			return id
		case database.VolumeTransportTypeUNSPECIFIED:
			return ""
		default:
			return ""
		}
	}
	checkAuthorized := func(transport datatypes.JSONType[database.VolumeTransport], targetID, clientID string) bool {
		var path string
		switch transport.Data().Type {
		case database.VolumeTransportTypeISCSI:
			path = fmt.Sprintf("/sys/kernel/config/target/iscsi/%s/tpgt_1/acls/%s", targetID, clientID)
		case database.VolumeTransportTypeNVMEOF_TCP:
			path = fmt.Sprintf("/sys/kernel/config/nvmet/subsystems/%s/allowed_hosts/%s", targetID, clientID)
		case database.VolumeTransportTypeUNSPECIFIED:
			return false
		default:
			return false
		}
		_, err := literal.With(publishExecutor).Run(ctx, fmt.Sprintf("ls -d %s", path))
		return err == nil
	}
	checkConnected := func(transport datatypes.JSONType[database.VolumeTransport], targetID string) bool {
		var cmd string
		switch transport.Data().Type {
		case database.VolumeTransportTypeISCSI:
			cmd = fmt.Sprintf("iscsiadm -m session | grep -q %s", targetID)
		case database.VolumeTransportTypeNVMEOF_TCP:
			cmd = fmt.Sprintf("nvme list-subsys -n %s", targetID)
		case database.VolumeTransportTypeUNSPECIFIED:
			return false
		default:
			return false
		}
		_, err := literal.With(connectExecutor).Run(ctx, cmd)
		return err == nil
	}

	targetID := getTargetID(volumedb, publishHost)
	clientID := getClientID(volumedb.Transport, connectHost)
	targetAddress := publishHost.Connection.Data().Remote.Address
	targetPassword := publishHost.Key
	initiatorPassword := connectHost.Key

	if volumedb.IsConnected() {
		// Reconcile authorization.
		isAuthorized := checkAuthorized(volumedb.Transport, targetID, clientID)
		if !isAuthorized {
			slogctx.Info(ctx, "authorizing client during sync", "volumeId", volumedb.ID, "clientId", clientID)
			switch volumedb.Transport.Data().Type {
			case database.VolumeTransportTypeISCSI:
				err := iscsi.With(publishExecutor).Authorize(ctx, iscsi.AuthorizeArguments{
					TargetIQN:         iscsi.IQN(targetID),
					InitiatorIQN:      iscsi.IQN(clientID),
					InitiatorPassword: initiatorPassword,
					TargetPassword:    targetPassword,
				})
				if err != nil {
					return fmt.Errorf("failed to authorize iscsi client: %w", err)
				}
			case database.VolumeTransportTypeNVMEOF_TCP:
				err := nvmeof.With(publishExecutor).Authorize(ctx, nvmeof.AuthorizeArguments{
					TargetNQN:         nvmeof.NQN(targetID),
					InitiatorNQN:      nvmeof.NQN(clientID),
					InitiatorPassword: initiatorPassword,
					TargetPassword:    targetPassword,
				})
				if err != nil {
					return fmt.Errorf("failed to authorize nvmeof client: %w", err)
				}
			case database.VolumeTransportTypeUNSPECIFIED:
				return fmt.Errorf("no transport specified for volume publish")
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}

		// Reconcile connection.
		isConnected := checkConnected(volumedb.Transport, targetID)
		if !isConnected {
			slogctx.Info(ctx, "connecting volume during sync", "volumeId", volumedb.ID)
			switch volumedb.Transport.Data().Type {
			case database.VolumeTransportTypeISCSI:
				err := iscsi.With(connectExecutor).ConnectTarget(ctx, iscsi.ConnectTargetArguments{
					TargetIQN:         iscsi.IQN(targetID),
					TargetAddress:     targetAddress,
					InitiatorIQN:      iscsi.IQN(clientID),
					InitiatorPassword: initiatorPassword,
					TargetPassword:    targetPassword,
				})
				if err != nil {
					return fmt.Errorf("failed to connect iscsi volume: %w", err)
				}
			case database.VolumeTransportTypeNVMEOF_TCP:
				err := nvmeof.With(connectExecutor).ConnectTarget(ctx, nvmeof.ConnectTargetArguments{
					TargetNQN:         nvmeof.NQN(targetID),
					TargetAddress:     targetAddress,
					InitiatorNQN:      nvmeof.NQN(clientID),
					InitiatorPassword: initiatorPassword,
					TargetPassword:    targetPassword,
				})
				if err != nil {
					return fmt.Errorf("failed to connect nvmeof volume: %w", err)
				}
			case database.VolumeTransportTypeUNSPECIFIED:
				return fmt.Errorf("no transport specified for volume publish")
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}
	} else {
		// Reconcile connection.
		isConnected := checkConnected(volumedb.Transport, targetID)
		if isConnected {
			slogctx.Info(ctx, "disconnecting volume during sync", "volumeId", volumedb.ID)
			switch volumedb.Transport.Data().Type {
			case database.VolumeTransportTypeISCSI:
				err := iscsi.With(connectExecutor).DisconnectTarget(ctx, iscsi.DisconnectTargetArguments{
					TargetIQN:     iscsi.IQN(targetID),
					TargetAddress: targetAddress,
				})
				if err != nil {
					return fmt.Errorf("failed to disconnect iscsi volume: %w", err)
				}
			case database.VolumeTransportTypeNVMEOF_TCP:
				err := nvmeof.With(connectExecutor).DisconnectTarget(ctx, nvmeof.DisconnectTargetArguments{
					TargetNQN: nvmeof.NQN(targetID),
				})
				if err != nil {
					return fmt.Errorf("failed to disconnect nvmeof volume: %w", err)
				}
			case database.VolumeTransportTypeUNSPECIFIED:
				return fmt.Errorf("no transport specified for volume publish")
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}

		// Reconcile authorization.
		isAuthorized := checkAuthorized(volumedb.Transport, targetID, clientID)
		if isAuthorized {
			slogctx.Info(ctx, "unauthorizing client during sync", "volumeId", volumedb.ID, "clientId", clientID)
			switch volumedb.Transport.Data().Type {
			case database.VolumeTransportTypeISCSI:
				err := iscsi.With(publishExecutor).Unauthorize(ctx, iscsi.UnauthorizeArguments{
					TargetIQN:    iscsi.IQN(targetID),
					InitiatorIQN: iscsi.IQN(clientID),
				})
				if err != nil {
					return fmt.Errorf("failed to unauthorize iscsi client: %w", err)
				}
			case database.VolumeTransportTypeNVMEOF_TCP:
				err := nvmeof.With(publishExecutor).Unauthorize(ctx, nvmeof.UnauthorizeArguments{
					TargetNQN:    nvmeof.NQN(targetID),
					InitiatorNQN: nvmeof.NQN(clientID),
				})
				if err != nil {
					return fmt.Errorf("failed to unauthorize nvmeof client: %w", err)
				}
			case database.VolumeTransportTypeUNSPECIFIED:
				return fmt.Errorf("no transport specified for volume publish")
			default:
				return fmt.Errorf("no transport specified on volume")
			}
		}
	}

	return nil
}

func (s *VolumeSyncer) syncStage(ctx context.Context, volumedb *database.Volume) error {
	if volumedb.ServerHost == "" || volumedb.ClientHost == "" || volumedb.StagingPath == "" {
		return nil
	}

	publishExecutor, publishHost, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
	if err != nil {
		return err
	}
	_ = publishExecutor
	connectExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
	if err != nil {
		return err
	}

	checkMounted := func(mountPath string) bool {
		isMounted, _ := mount.With(connectExecutor).IsMounted(ctx, mountPath)
		return isMounted
	}

	if volumedb.IsStaged() {
		isMounted := checkMounted(volumedb.StagingPath)
		if !isMounted {
			slogctx.Info(ctx, "staging volume during sync", "volumeId", volumedb.ID, "stagingPath", volumedb.StagingPath)

			getTargetID := func(volumedb *database.Volume, host *database.Host) string {
				switch volumedb.Transport.Data().Type {
				case database.VolumeTransportTypeISCSI:
					id, _ := host.VolumeIQN(volumedb.ID)
					return id
				case database.VolumeTransportTypeNVMEOF_TCP:
					id, _ := host.VolumeNQN(volumedb.ID)
					return id
				case database.VolumeTransportTypeUNSPECIFIED:
					return ""
				default:
					return ""
				}
			}
			targetID := getTargetID(volumedb, publishHost)
			targetAddress := publishHost.Connection.Data().Remote.Address
			devicePath, err := volumedb.DevicePathClient(targetAddress, targetID)
			if err != nil {
				return fmt.Errorf("failed to get device path: %w", err)
			}

			if volumedb.Mode == database.VolumeModeFILESYSTEM {
				fsType, err := fs.With(connectExecutor).GetFSType(ctx, devicePath)
				if err != nil {
					return fmt.Errorf("failed to get filesystem type: %w", err)
				}
				if fsType == "" {
					err = fs.With(connectExecutor).Format(ctx, fs.FormatArguments{
						Device:        devicePath,
						WaitForDevice: true,
					})
					if err != nil {
						return fmt.Errorf("failed to format device: %w", err)
					}
				}
			}

			_, err = literal.With(connectExecutor).Run(ctx, fmt.Sprintf("mkdir -m 0750 -p %s", volumedb.StagingPath))
			if err != nil {
				return fmt.Errorf("failed to create staging path: %w", err)
			}

			mountArgs := mount.MountArguments{
				SourcePath: devicePath,
				TargetPath: volumedb.StagingPath,
			}
			if volumedb.Mode == database.VolumeModeFILESYSTEM {
				mountArgs.FSType = "ext4"
				mountArgs.Options = []string{"defaults"}
			} else {
				mountArgs.Options = []string{"bind"}
			}

			err = mount.With(connectExecutor).Mount(ctx, mountArgs)
			if err != nil {
				return fmt.Errorf("failed to stage volume: %w", err)
			}
		}
	} else {
		isMounted := checkMounted(volumedb.StagingPath)
		if isMounted {
			slogctx.Info(ctx, "unstaging volume during sync", "volumeId", volumedb.ID)
			err := mount.With(connectExecutor).Umount(ctx, mount.UmountArguments{
				Path: volumedb.StagingPath,
			})
			if err != nil {
				return fmt.Errorf("failed to unstage volume: %w", err)
			}
		}
	}

	return nil
}

func (s *VolumeSyncer) syncMount(ctx context.Context, volumedb *database.Volume) error {
	if volumedb.ClientHost == "" || volumedb.StagingPath == "" {
		return nil
	}

	connectExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
	if err != nil {
		return err
	}

	checkMounted := func(mountPath string) bool {
		isMounted, _ := mount.With(connectExecutor).IsMounted(ctx, mountPath)
		return isMounted
	}

	// Reconcile TargetPaths.
	for _, targetPath := range volumedb.TargetPaths {
		isMounted := checkMounted(targetPath)
		if !isMounted {
			slogctx.Info(ctx, "mounting volume during sync", "volumeId", volumedb.ID, "targetPath", targetPath)

			if volumedb.Mode == database.VolumeModeBLOCK {
				_, err := literal.With(connectExecutor).Run(ctx, fmt.Sprintf("install -m 0644 /dev/null %s", targetPath))
				if err != nil {
					return fmt.Errorf("failed to touch mount path: %w", err)
				}
			} else {
				_, err := literal.With(connectExecutor).Run(ctx, fmt.Sprintf("mkdir -m 0750 -p %s", targetPath))
				if err != nil {
					return fmt.Errorf("failed to touch mount path: %w", err)
				}
			}

			err := mount.With(connectExecutor).Mount(ctx, mount.MountArguments{
				SourcePath: volumedb.StagingPath,
				TargetPath: targetPath,
				Options:    []string{"bind"},
			})
			if err != nil {
				return fmt.Errorf("failed to bind mount volume: %w", err)
			}

			if volumedb.Mode == database.VolumeModeFILESYSTEM {
				_, err = literal.With(connectExecutor).Run(ctx, fmt.Sprintf("chmod 0777 %s", targetPath))
				if err != nil {
					return fmt.Errorf("failed to chmod mount path: %w", err)
				}
			}
		}
	}

	if !volumedb.IsMounted() {
		for _, targetPath := range volumedb.TargetPaths {
			isMounted := checkMounted(targetPath)
			if isMounted {
				slogctx.Info(ctx, "unmounting volume during sync", "volumeId", volumedb.ID, "targetPath", targetPath)
				err := mount.With(connectExecutor).Umount(ctx, mount.UmountArguments{
					Path: targetPath,
				})
				if err != nil {
					return fmt.Errorf("failed to unmount volume: %w", err)
				}
			}
		}
	}

	return nil
}
