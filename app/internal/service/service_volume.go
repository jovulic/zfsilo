package service

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/app/internal/command"
	"github.com/jovulic/zfsilo/app/internal/command/fs"
	"github.com/jovulic/zfsilo/app/internal/command/iscsi"
	"github.com/jovulic/zfsilo/app/internal/command/zfs"
	converteriface "github.com/jovulic/zfsilo/app/internal/converter/iface"
	"github.com/jovulic/zfsilo/app/internal/database"
	"github.com/jovulic/zfsilo/lib/try"
	slogctx "github.com/veqryn/slog-context"
	structpb "google.golang.org/protobuf/types/known/structpb"
	"gorm.io/gorm"
)

// applyVolumeUpdate modifies an existing Volume object with fields from a
// protobuf Struct. It returns an error if any of the provided fields have an
// incorrect type.
func applyVolumeUpdate(
	existingVolume *zfsilov1.Volume,
	updates *structpb.Struct,
) error {
	if updates == nil || len(updates.GetFields()) == 0 {
		// Nothing to update.
		return nil
	}

	updateMap := updates.GetFields()

	// We loop over all fields explicitly handling any fields that can be
	// updated.
	for key, value := range updateMap {
		// We Use a switch to explicitly handle only the mutable fields.
		switch key {
		case "struct":
			nestedStruct, ok := value.GetKind().(*structpb.Value_StructValue)
			if !ok {
				return &FieldTypeError{
					FieldName:    key,
					ExpectedType: "object",
					ActualType:   fmt.Sprintf("%T", value.GetKind()),
				}
			}
			existingVolume.Struct = nestedStruct.StructValue
		case "capacity_bytes":
			numValue, ok := value.GetKind().(*structpb.Value_NumberValue)
			if !ok {
				return &FieldTypeError{
					FieldName:    key,
					ExpectedType: "number",
					ActualType:   fmt.Sprintf("%T", value.GetKind()),
				}
			}
			existingVolume.CapacityBytes = int64(numValue.NumberValue)
		default:
			// Silently ignore immutable, read-only, or unknown fields.
			// skip
		}
	}

	return nil
}

const (
	listVolumesDefaultPageSize = 25
	listVolumeMaxPageSize      = 100
)

type VolumeService struct {
	zfsilov1connect.UnimplementedVolumeServiceHandler

	database    *gorm.DB
	converter   converteriface.VolumeConverter
	producer    command.ProduceExecutor
	consumers   command.ConsumeExecutorMap
	host        *iscsi.Host
	credentials iscsi.Credentials
}

func NewVolumeService(
	database *gorm.DB,
	converter converteriface.VolumeConverter,
	producer command.ProduceExecutor,
	consumers command.ConsumeExecutorMap,
	host *iscsi.Host,
	credentials iscsi.Credentials,
) *VolumeService {
	return &VolumeService{
		database:    database,
		converter:   converter,
		producer:    producer,
		consumers:   consumers,
		host:        host,
		credentials: credentials,
	}
}

func (s *VolumeService) GetVolume(ctx context.Context, req *connect.Request[zfsilov1.GetVolumeRequest]) (*connect.Response[zfsilov1.GetVolumeResponse], error) {
	volumedb, err := gorm.G[database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		slogctx.Error(ctx, "failed to get volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	return connect.NewResponse(&zfsilov1.GetVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) ListVolumes(ctx context.Context, req *connect.Request[zfsilov1.ListVolumesRequest]) (*connect.Response[zfsilov1.ListVolumesResponse], error) {
	// Determine the offset and limit parameters.
	var offset, limit int

	pageSize := int(req.Msg.PageSize)
	if pageSize <= 0 {
		pageSize = listVolumesDefaultPageSize
	}
	if pageSize > listVolumeMaxPageSize {
		pageSize = listVolumeMaxPageSize
	}

	// The page token is empty on the first reuqest and populated on subsequent
	// requests.
	if req.Msg.PageToken == "" {
		offset = 0
		limit = pageSize
	} else {
		pageToken, err := UnmarshalPageToken(req.Msg.PageToken)
		if err != nil {
			slogctx.Error(ctx, "failed to unmarshal page token", slogctx.Err(err))
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid page token"))
		}
		offset = pageToken.Offset
		limit = pageToken.Limit
	}

	// Execute the database query using the determined parameters.
	volumedbs, err := gorm.G[database.Volume](s.database).
		Order("create_time desc").
		Offset(offset).
		Limit(limit).
		Find(ctx)
	if err != nil {
		slogctx.Error(ctx, "failed to get volumes from database", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to retrieve volumes"))
	}

	// Convert database models to API models.
	volumeapis, err := s.converter.FromDBToAPIList(volumedbs)
	if err != nil {
		slogctx.Error(ctx, "failed to map database volumes to API", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to process volumes"))
	}

	// Determine the next page token and build the response. If we are at the
	// limit, we might have another page. If we are below we are finished and do
	// not need to create a next page token.
	var nextPageTokenString string
	if len(volumeapis) == limit {
		nextPageToken := PageToken{
			Offset: offset + len(volumeapis),
			Limit:  limit,
		}
		tokenStr, err := nextPageToken.Marshal()
		if err != nil {
			slogctx.Error(ctx, "failed to marshal next page token", slogctx.Err(err))
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create next page token"))
		}
		nextPageTokenString = tokenStr
	}

	return connect.NewResponse(&zfsilov1.ListVolumesResponse{
		Volumes:       volumeapis,
		NextPageToken: nextPageTokenString,
	}), nil
}

func (s *VolumeService) CreateVolume(ctx context.Context, req *connect.Request[zfsilov1.CreateVolumeRequest]) (*connect.Response[zfsilov1.CreateVolumeResponse], error) {
	volumedb, err := s.converter.FromAPIToDB(req.Msg.Volume)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		// Create database entry.
		err := gorm.G[database.Volume](tx).Create(ctx, &volumedb)
		if err != nil {
			return err
		}

		err = try.Do(ctx, func(stack *try.UndoStack) error {
			// Create ZFS volume.
			opts := make(map[string]string)
			for _, option := range req.Msg.Volume.Options {
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
			stack.Push(func(ctx context.Context) error {
				err := zfs.With(s.producer).DestroyVolume(ctx, zfs.DestroyVolumeArguments{
					Name: volumedb.DatasetID,
				})
				if err != nil {
					return fmt.Errorf("failed to destroy zfs volume: %w", err)
				}
				return nil
			})

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
		})
		if err != nil {
			slogctx.Error(ctx, "failed to create the zfs volume", slogctx.Err(err))
			return fmt.Errorf("failed to create the zfs volume: %w", err)
		}

		return nil
	})
	if err != nil {
		// Check for specific database errors to return correct connect codes.
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("volume already exists"))
		}
		// For ZFS errors or other DB errors, return internal error.
		slogctx.Error(ctx, "failed to create volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create volume: %w", err))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	return connect.NewResponse(&zfsilov1.CreateVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) UpdateVolume(ctx context.Context, req *connect.Request[zfsilov1.UpdateVolumeRequest]) (*connect.Response[zfsilov1.UpdateVolumeResponse], error) {
	idValue := req.Msg.Volume.GetFields()["id"]
	if idValue == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume id must be defined"))
	}
	id := idValue.GetStringValue()

	volumedb, err := gorm.G[database.Volume](s.database).Where("id = ?", id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		slogctx.Error(ctx, "failed to get volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	err = applyVolumeUpdate(volumeapi, req.Msg.Volume)
	if err != nil {
		var errField *FieldTypeError
		if errors.As(err, &errField) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("failed to update volume: %w", errField))
		}
		slogctx.Error(ctx, "failed to apply update to volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	volumedb, err = s.converter.FromAPIToDB(volumeapi)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	// NOTE: We do not perform the update in a transaction as we have not written
	// any rollback capability currently.

	_, err = gorm.G[database.Volume](s.database).Updates(ctx, volumedb)
	if err != nil {
		slogctx.Error(ctx, "failed to update volume in database", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update volume"))
	}

	// We update the size of the volume by zfs set.
	err = zfs.With(s.producer).SetProperty(ctx, zfs.SetPropertyArguments{
		Name:          volumedb.DatasetID,
		PropertyKey:   "volsize",
		PropertyValue: fmt.Sprintf("%d", volumedb.CapacityBytes),
	})
	if err != nil {
		slogctx.Error(ctx, "failed to update volume on producer", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update volume"))
	}

	// We check if the volume has been published, and if it has, we need to issue
	// a refresh on the consumer.
	if volumedb.IsPublished() {
		target, ok := s.consumers[volumedb.InitiatorIQN]
		if !ok {
			slogctx.Error(ctx, "unknown initiator", slogctx.Err(err))
			return nil, connect.NewError(connect.CodeInternal, errors.New("unknown initiator"))
		}

		err = iscsi.With(target).RescanTarget(ctx, iscsi.RescanTargetArguments{
			TargetIQN:     iscsi.IQN(volumedb.TargetIQN),
			TargetAddress: volumedb.TargetAddress,
		})
		if err != nil {
			slogctx.Error(ctx, "failed to perform rescan on consumer", slogctx.Err(err))
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to rescan"))
		}

		// If the mode is filesystem we also need to resize the filesystem.
		if volumedb.Mode == database.VolumeModeFILESYSTEM {
			err = fs.With(target).Resize(ctx, fs.ResizeArguments{
				Device: volumedb.DevicePathISCSI(),
			})
			if err != nil {
				slogctx.Error(ctx, "failed to perform resize on consumer", slogctx.Err(err))
				return nil, connect.NewError(connect.CodeInternal, errors.New("failed to resize"))
			}
		}
	}

	return connect.NewResponse(&zfsilov1.UpdateVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, req *connect.Request[zfsilov1.DeleteVolumeRequest]) (*connect.Response[zfsilov1.DeleteVolumeResponse], error) {
	volumedb, err := gorm.G[database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		slogctx.Error(ctx, "failed to get volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	switch {
	case volumedb.IsPublished():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is published"))
	case volumedb.IsConnected():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is connected"))
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is mounted"))
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		// Destroy ZFS volume.
		err = zfs.With(s.producer).DestroyVolume(ctx, zfs.DestroyVolumeArguments{
			Name: volumedb.DatasetID,
		})
		if err != nil {
			return fmt.Errorf("failed to destroy zfs volume: %w", err)
		}

		// Delete from database.
		_, err = gorm.G[database.Volume](tx).Where("id = ?", req.Msg.Id).Delete(ctx)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		slogctx.Error(ctx, "failed to delete volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete volume: %w", err))
	}

	return connect.NewResponse(&zfsilov1.DeleteVolumeResponse{}), nil
}

func (s *VolumeService) PublishVolume(ctx context.Context, req *connect.Request[zfsilov1.PublishVolumeRequest]) (*connect.Response[zfsilov1.PublishVolumeResponse], error) {
	volumedb, err := gorm.G[database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		slogctx.Error(ctx, "failed to get volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	switch {
	case volumedb.IsPublished():
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
			return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
		}
		return connect.NewResponse(&zfsilov1.PublishVolumeResponse{Volume: volumeapi}), nil
	case volumedb.IsConnected():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is connected"))
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is mounted"))
	}

	if volumedb.IsPublished() {
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
			return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
		}
		return connect.NewResponse(&zfsilov1.PublishVolumeResponse{Volume: volumeapi}), nil
	}

	volumedb.TargetIQN = s.host.VolumeIQN(volumedb.ID).String()

	err = s.database.Transaction(func(tx *gorm.DB) error {
		_, err = gorm.G[database.Volume](s.database).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		err = iscsi.With(s.producer).PublishVolume(ctx, iscsi.PublishVolumeArguments{
			VolumeID:    volumedb.ID,
			DevicePath:  fmt.Sprintf("/dev/zvol/%s", volumedb.DatasetID),
			TargetIQN:   iscsi.IQN(volumedb.TargetIQN),
			Credentials: s.credentials,
		})
		if err != nil {
			return fmt.Errorf("failed to publish volume: %w", err)
		}
		return nil
	})
	if err != nil {
		slogctx.Error(ctx, "failed to publish volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to publish volume"))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}
	return connect.NewResponse(&zfsilov1.PublishVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) UnpublishVolume(ctx context.Context, req *connect.Request[zfsilov1.UnpublishVolumeRequest]) (*connect.Response[zfsilov1.UnpublishVolumeResponse], error) {
	volumedb, err := gorm.G[database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		slogctx.Error(ctx, "failed to get volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	switch {
	case !volumedb.IsPublished():
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
			return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
		}
		return connect.NewResponse(&zfsilov1.UnpublishVolumeResponse{Volume: volumeapi}), nil
	case volumedb.IsConnected():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is connected"))
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is mounted"))
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		previousTargetIQN := volumedb.TargetIQN
		volumedb.TargetIQN = ""

		_, err = gorm.G[database.Volume](s.database).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		err = iscsi.With(s.producer).UnpublishVolume(ctx, iscsi.UnpublishVolumeArguments{
			VolumeID:  volumedb.ID,
			TargetIQN: iscsi.IQN(previousTargetIQN),
		})
		if err != nil {
			return fmt.Errorf("failed to unpublish volume: %w", err)
		}
		return nil
	})
	if err != nil {
		slogctx.Error(ctx, "failed to unpublish volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to unpublish volume"))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}
	return connect.NewResponse(&zfsilov1.UnpublishVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) ConnectVolume(ctx context.Context, req *connect.Request[zfsilov1.ConnectVolumeRequest]) (*connect.Response[zfsilov1.ConnectVolumeResponse], error) {
	volumedb, err := gorm.G[database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		slogctx.Error(ctx, "failed to get volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	switch {
	case !volumedb.IsPublished():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not published"))
	case volumedb.IsConnected():
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
			return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
		}
		return connect.NewResponse(&zfsilov1.ConnectVolumeResponse{Volume: volumeapi}), nil
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is mounted"))
	}

	volumedb.InitiatorIQN = req.Msg.InitiatorIqn
	volumedb.TargetAddress = req.Msg.TargetAddress

	err = s.database.Transaction(func(tx *gorm.DB) error {
		_, err = gorm.G[database.Volume](s.database).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		consumer, ok := s.consumers[volumedb.InitiatorIQN]
		if !ok {
			return fmt.Errorf("unable to lookup initiator %s", volumedb.InitiatorIQN)
		}
		err = iscsi.With(consumer).ConnectTarget(ctx, iscsi.ConnectTargetArguments{
			TargetIQN:     iscsi.IQN(volumedb.TargetIQN),
			TargetAddress: volumedb.TargetAddress,
			Credentials:   s.credentials,
		})
		if err != nil {
			return fmt.Errorf("failed to connect volume: %w", err)
		}
		return nil
	})
	if err != nil {
		slogctx.Error(ctx, "failed to connect volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to connect volume"))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}
	return connect.NewResponse(&zfsilov1.ConnectVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) DisconnectVolume(context.Context, *connect.Request[zfsilov1.DisconnectVolumeRequest]) (*connect.Response[zfsilov1.DisconnectVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.VolumeService.DisconnectVolume is not implemented"))
}

func (s *VolumeService) MountVolume(context.Context, *connect.Request[zfsilov1.MountVolumeRequest]) (*connect.Response[zfsilov1.MountVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.VolumeService.MountVolume is not implemented"))
}

func (s *VolumeService) UnmountVolume(context.Context, *connect.Request[zfsilov1.UnmountVolumeRequest]) (*connect.Response[zfsilov1.UnmountVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.VolumeService.UnmountVolume is not implemented"))
}

func (s *VolumeService) StatsVolume(context.Context, *connect.Request[zfsilov1.StatsVolumeRequest]) (*connect.Response[zfsilov1.StatsVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.VolumeService.StatsVolume is not implemented"))
}

func (s *VolumeService) SyncVolume(context.Context, *connect.Request[zfsilov1.SyncVolumeRequest]) (*connect.Response[zfsilov1.SyncVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.VolumeService.SyncVolume is not implemented"))
}

func (s *VolumeService) SyncVolumes(context.Context, *connect.Request[zfsilov1.SyncVolumesRequest]) (*connect.Response[zfsilov1.SyncVolumesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.VolumeService.SyncVolumes is not implemented"))
}
