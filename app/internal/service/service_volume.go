package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/app/internal/command"
	"github.com/jovulic/zfsilo/app/internal/command/fs"
	"github.com/jovulic/zfsilo/app/internal/command/iscsi"
	"github.com/jovulic/zfsilo/app/internal/command/literal"
	"github.com/jovulic/zfsilo/app/internal/command/mount"
	"github.com/jovulic/zfsilo/app/internal/command/nvmeof"
	"github.com/jovulic/zfsilo/app/internal/command/zfs"
	converteriface "github.com/jovulic/zfsilo/app/internal/converter/iface"
	"github.com/jovulic/zfsilo/app/internal/database"
	libcommand "github.com/jovulic/zfsilo/lib/command"
	structpb "google.golang.org/protobuf/types/known/structpb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	listVolumesDefaultPageSize = 25
	listVolumeMaxPageSize      = 100
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
		case "options":
			listValue, ok := value.GetKind().(*structpb.Value_ListValue)
			if !ok {
				return &FieldTypeError{
					FieldName:    key,
					ExpectedType: "list",
					ActualType:   fmt.Sprintf("%T", value.GetKind()),
				}
			}

			newOptions := make([]*zfsilov1.Volume_Option, 0, len(listValue.ListValue.Values))
			for i, v := range listValue.ListValue.Values {
				structValue, ok := v.GetKind().(*structpb.Value_StructValue)
				if !ok {
					return &FieldTypeError{
						FieldName:    fmt.Sprintf("%s[%d]", key, i),
						ExpectedType: "object",
						ActualType:   fmt.Sprintf("%T", v.GetKind()),
					}
				}
				fields := structValue.StructValue.GetFields()
				newOptions = append(newOptions, &zfsilov1.Volume_Option{
					Key:   fields["key"].GetStringValue(),
					Value: fields["value"].GetStringValue(),
				})
			}
			existingVolume.Options = newOptions
		default:
			// Silently ignore immutable, read-only, or unknown fields.
			// skip
		}
	}

	return nil
}

func getTargetID(host *database.Host, transport database.VolumeTransport, volumeID string) (string, error) {
	switch transport.Type {
	case database.VolumeTransportTypeISCSI:
		return host.VolumeIQN(volumeID)
	case database.VolumeTransportTypeNVMEOF_TCP:
		return host.VolumeNQN(volumeID)
	case database.VolumeTransportTypeUNSPECIFIED:
		fallthrough
	default:
		return "", fmt.Errorf("unsupported transport for target ID: %v", transport.Type)
	}
}

func getClientID(host *database.Host, transport database.VolumeTransport) (string, error) {
	switch transport.Type {
	case database.VolumeTransportTypeISCSI:
		return host.IQN()
	case database.VolumeTransportTypeNVMEOF_TCP:
		return host.NQN()
	case database.VolumeTransportTypeUNSPECIFIED:
		fallthrough
	default:
		return "", fmt.Errorf("unsupported transport for client ID: %v", transport.Type)
	}
}

func getTargetConnection(host *database.Host) (string, string) {
	address := ""
	conn := host.Connection.Data()
	if conn.Type == database.HostConnectionTypeRemote && conn.Remote != nil {
		address = conn.Remote.Address
	}
	return address, host.Key
}

func indexOf(slice []string, val string) int {
	for i, item := range slice {
		if item == val {
			return i
		}
	}
	return -1
}

type VolumeService struct {
	zfsilov1connect.UnimplementedVolumeServiceHandler

	database        *gorm.DB
	converter       converteriface.VolumeConverter
	executorFactory *command.ExecutorFactory
	syncer          *VolumeSyncer
}

func NewVolumeService(
	database *gorm.DB,
	converter converteriface.VolumeConverter,
	executorFactory *command.ExecutorFactory,
	syncer *VolumeSyncer,
) *VolumeService {
	return &VolumeService{
		database:        database,
		converter:       converter,
		executorFactory: executorFactory,
		syncer:          syncer,
	}
}

func (s *VolumeService) GetVolume(ctx context.Context, req *connect.Request[zfsilov1.GetVolumeRequest]) (*connect.Response[zfsilov1.GetVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
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
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("failed to unmarshal page token: %w", err))
		}
		offset = pageToken.Offset
		limit = pageToken.Limit
	}

	// Execute the database query using the determined parameters.
	volumedbs, err := gorm.G[*database.Volume](s.database).
		Order("create_time desc").
		Offset(offset).
		Limit(limit).
		Find(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get volumes from database: %w", err))
	}

	// Convert database models to API models.
	volumeapis, err := s.converter.FromDBToAPIList(volumedbs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to map database volumes to API: %w", err))
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
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal next page token: %w", err))
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
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}

	volumedb.Status = database.VolumeStatusINITIAL

	err = s.database.Transaction(func(tx *gorm.DB) error {
		// Create database entry.
		err := gorm.G[*database.Volume](tx).Create(ctx, &volumedb)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// Check for specific database errors to return correct connect codes.
		// NOTE: GORM with SQLite may not always return ErrDuplicatedKey as a
		// wrapped error, so we also check the message.
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("volume already exists"))
		}
		// Return internal error for other DB errors.
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create volume: %w", err))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}

	return connect.NewResponse(&zfsilov1.CreateVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) UpdateVolume(ctx context.Context, req *connect.Request[zfsilov1.UpdateVolumeRequest]) (*connect.Response[zfsilov1.UpdateVolumeResponse], error) {
	idValue := req.Msg.Volume.GetFields()["id"]
	if idValue == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume id must be defined"))
	}
	id := idValue.GetStringValue()

	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}

	err = applyVolumeUpdate(volumeapi, req.Msg.Volume)
	if err != nil {
		var errField *FieldTypeError
		if errors.As(err, &errField) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("failed to update volume: %w", errField))
		}
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to apply update to volume: %w", err))
	}

	volumedb, err = s.converter.FromAPIToDB(volumeapi)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}

	// NOTE: We do not perform the update in a transaction as we have not written
	// any rollback capability currently.

	_, err = gorm.G[*database.Volume](s.database).Updates(ctx, volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update volume in database: %w", err))
	}

	// If the volume has a publish host, we update the ZFS properties.
	if volumedb.ServerHost != "" {
		executor, _, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get producer executor: %w", err))
		}

		// We update the size of the volume by zfs set.
		err = zfs.With(executor).SetProperty(ctx, zfs.SetPropertyArguments{
			Name:          volumedb.DatasetID,
			PropertyKey:   "volsize",
			PropertyValue: fmt.Sprintf("%d", volumedb.CapacityBytes),
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update volume size on producer: %w", err))
		}

		// We update the options by zfs set.
		for _, opt := range volumedb.Options.Data() {
			err = zfs.With(executor).SetProperty(ctx, zfs.SetPropertyArguments{
				Name:          volumedb.DatasetID,
				PropertyKey:   opt.Key,
				PropertyValue: opt.Value,
			})
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update volume property %s on producer: %w", opt.Key, err))
			}
		}
	}

	// We check if the volume has been connected, and if it has, we need to issue
	// a refresh on the consumer.
	if volumedb.IsConnected() && volumedb.ClientHost != "" {
		consumeExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get consumer executor: %w", err))
		}

		_, publishHost, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get publish host: %w", err))
		}

		transport := volumedb.Transport.Data()

		targetID, err := getTargetID(publishHost, transport, volumedb.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get target ID: %w", err))
		}

		targetAddress, _ := getTargetConnection(publishHost)

		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			err = iscsi.With(consumeExecutor).RescanTarget(ctx, iscsi.RescanTargetArguments{
				TargetIQN:     iscsi.IQN(targetID),
				TargetAddress: targetAddress,
			})
		case database.VolumeTransportTypeNVMEOF_TCP:
			err = nvmeof.With(consumeExecutor).RescanTarget(ctx, nvmeof.RescanTargetArguments{
				TargetNQN: nvmeof.NQN(targetID),
			})
		case database.VolumeTransportTypeUNSPECIFIED:
			fallthrough
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("no transport specified on volume"))
		}
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to perform rescan on consumer: %w", err))
		}

		// If the mode is filesystem we also need to resize the filesystem.
		if volumedb.Mode == database.VolumeModeFILESYSTEM {
			devicePath, err := volumedb.DevicePathClient(targetAddress, targetID)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get device path: %w", err))
			}
			err = fs.With(consumeExecutor).Resize(ctx, fs.ResizeArguments{
				Device: devicePath,
			})
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to perform resize on consumer: %w", err))
			}
		}
	}

	return connect.NewResponse(&zfsilov1.UpdateVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, req *connect.Request[zfsilov1.DeleteVolumeRequest]) (*connect.Response[zfsilov1.DeleteVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
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
		// Destroy ZFS volume if it was created.
		if volumedb.ServerHost != "" {
			executor, _, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
			if err != nil {
				return fmt.Errorf("failed to get producer executor: %w", err)
			}
			err = zfs.With(executor).DestroyVolume(ctx, zfs.DestroyVolumeArguments{
				Name: volumedb.DatasetID,
			})
			if err != nil {
				return fmt.Errorf("failed to destroy zfs volume: %w", err)
			}
		}

		// Delete from database.
		_, err = gorm.G[*database.Volume](tx).Where("id = ?", req.Msg.Id).Delete(ctx)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		if strings.Contains(err.Error(), "dataset is busy") {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("dataset is busy: %w", err))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete volume: %w", err))
	}

	return connect.NewResponse(&zfsilov1.DeleteVolumeResponse{}), nil
}

func (s *VolumeService) PublishVolume(ctx context.Context, req *connect.Request[zfsilov1.PublishVolumeRequest]) (*connect.Response[zfsilov1.PublishVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	switch {
	case volumedb.IsPublished():
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
		}
		return connect.NewResponse(&zfsilov1.PublishVolumeResponse{Volume: volumeapi}), nil
	case volumedb.IsConnected():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is connected"))
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is mounted"))
	}

	// Use requested transport or default to ISCSI
	var transport database.VolumeTransport
	switch req.Msg.Transport {
	case zfsilov1.Volume_TRANSPORT_ISCSI:
		transport.Type = database.VolumeTransportTypeISCSI
	case zfsilov1.Volume_TRANSPORT_NVMEOF_TCP:
		transport.Type = database.VolumeTransportTypeNVMEOF_TCP
	case zfsilov1.Volume_TRANSPORT_UNSPECIFIED:
		fallthrough
	default:
		transport.Type = database.VolumeTransportTypeISCSI
	}
	volumedb.Transport = datatypes.NewJSONType(transport)
	volumedb.ServerHost = strings.TrimPrefix(req.Msg.ServerHost, "hosts/")
	volumedb.Status = database.VolumeStatusPUBLISHED

	err = s.database.Transaction(func(tx *gorm.DB) error {
		executor, host, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
		if err != nil {
			return err
		}

		transport := volumedb.Transport.Data()
		targetID, err := getTargetID(host, transport, volumedb.ID)
		if err != nil {
			return fmt.Errorf("failed to generate target ID: %w", err)
		}

		targetAddress, targetPassword := getTargetConnection(host)
		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			transport.ISCSI = &database.VolumeTransportISCSI{
				TargetAddress:  targetAddress,
				TargetIQN:      targetID,
				TargetPassword: targetPassword,
			}
		case database.VolumeTransportTypeNVMEOF_TCP:
			transport.NVMEOF = &database.VolumeTransportNVMEOF{
				TargetAddress:  targetAddress,
				TargetNQN:      targetID,
				TargetPassword: targetPassword,
			}
		case database.VolumeTransportTypeUNSPECIFIED:
			return fmt.Errorf("no transport specified for publish")
		}
		volumedb.Transport = datatypes.NewJSONType(transport)

		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		// Ensure ZFS volume exists.
		exists, err := zfs.With(executor).VolumeExists(ctx, zfs.VolumeExistsArguments{
			Name: volumedb.DatasetID,
		})
		if err != nil {
			return fmt.Errorf("failed to check zfs volume: %w", err)
		}
		if !exists {
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
		}

		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			err = iscsi.With(executor).PublishVolume(ctx, iscsi.PublishVolumeArguments{
				VolumeID:   volumedb.ID,
				DevicePath: fmt.Sprintf("/dev/zvol/%s", volumedb.DatasetID),
				TargetIQN:  iscsi.IQN(targetID),
			})
		case database.VolumeTransportTypeNVMEOF_TCP:
			err = nvmeof.With(executor).PublishVolume(ctx, nvmeof.PublishVolumeArguments{
				VolumeID:   volumedb.ID,
				DevicePath: fmt.Sprintf("/dev/zvol/%s", volumedb.DatasetID),
				TargetNQN:  nvmeof.NQN(targetID),
			})
		case database.VolumeTransportTypeUNSPECIFIED:
			fallthrough
		default:
			return fmt.Errorf("no transport specified on volume")
		}
		if err != nil {
			return fmt.Errorf("failed to publish volume: %w", err)
		}
		return nil
	})
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound || strings.Contains(err.Error(), "not found") {
			if _, ok := err.(*connect.Error); ok {
				return nil, err
			}
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to publish volume: %w", err))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.PublishVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) UnpublishVolume(ctx context.Context, req *connect.Request[zfsilov1.UnpublishVolumeRequest]) (*connect.Response[zfsilov1.UnpublishVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	switch {
	case !volumedb.IsPublished():
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
		}
		return connect.NewResponse(&zfsilov1.UnpublishVolumeResponse{Volume: volumeapi}), nil
	case volumedb.IsConnected():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is connected"))
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is mounted"))
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		executor, host, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
		if err != nil {
			return err
		}

		previousTransport := volumedb.Transport.Data()
		targetID, err := getTargetID(host, previousTransport, volumedb.ID)
		if err != nil {
			return fmt.Errorf("failed to generate target ID: %w", err)
		}

		volumedb.ServerHost = ""
		volumedb.Transport = datatypes.NewJSONType(database.VolumeTransport{Type: database.VolumeTransportTypeUNSPECIFIED})
		volumedb.Status = database.VolumeStatusINITIAL

		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		switch previousTransport.Type {
		case database.VolumeTransportTypeISCSI:
			err = iscsi.With(executor).UnpublishVolume(ctx, iscsi.UnpublishVolumeArguments{
				VolumeID:  volumedb.ID,
				TargetIQN: iscsi.IQN(targetID),
			})
		case database.VolumeTransportTypeNVMEOF_TCP:
			err = nvmeof.With(executor).UnpublishVolume(ctx, nvmeof.UnpublishVolumeArguments{
				TargetNQN: nvmeof.NQN(targetID),
			})
		case database.VolumeTransportTypeUNSPECIFIED:
			fallthrough
		default:
			return fmt.Errorf("no transport specified on volume")
		}
		if err != nil {
			return fmt.Errorf("failed to unpublish volume: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to unpublish volume: %w", err))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.UnpublishVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) ConnectVolume(ctx context.Context, req *connect.Request[zfsilov1.ConnectVolumeRequest]) (*connect.Response[zfsilov1.ConnectVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	switch {
	case !volumedb.IsPublished():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not published"))
	case volumedb.IsConnected():
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
		}
		return connect.NewResponse(&zfsilov1.ConnectVolumeResponse{Volume: volumeapi}), nil
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is mounted"))
	}

	volumedb.ClientHost = strings.TrimPrefix(req.Msg.ClientHost, "hosts/")
	volumedb.Status = database.VolumeStatusCONNECTED

	err = s.database.Transaction(func(tx *gorm.DB) error {
		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		producerExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
		if err != nil {
			return err
		}

		consumerExecutor, connectHost, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
		if err != nil {
			return err
		}

		transport := volumedb.Transport.Data()
		var targetID, targetAddress, targetPassword string
		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			targetID = transport.ISCSI.TargetIQN
			targetAddress = transport.ISCSI.TargetAddress
			targetPassword = transport.ISCSI.TargetPassword
		case database.VolumeTransportTypeNVMEOF_TCP:
			targetID = transport.NVMEOF.TargetNQN
			targetAddress = transport.NVMEOF.TargetAddress
			targetPassword = transport.NVMEOF.TargetPassword
		case database.VolumeTransportTypeUNSPECIFIED:
			return fmt.Errorf("no transport specified for volume connection")
		}

		clientID, err := getClientID(connectHost, transport)
		if err != nil {
			return fmt.Errorf("failed to get client ID: %w", err)
		}

		_, consumerPassword := getTargetConnection(connectHost)

		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			transport.ISCSI.InitiatorIQN = clientID
			transport.ISCSI.InitiatorPassword = consumerPassword
		case database.VolumeTransportTypeNVMEOF_TCP:
			transport.NVMEOF.InitiatorNQN = clientID
			transport.NVMEOF.InitiatorPassword = consumerPassword
		case database.VolumeTransportTypeUNSPECIFIED:
			return fmt.Errorf("no transport specified for initiator connection")
		}
		volumedb.Transport = datatypes.NewJSONType(transport)

		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			// Authorize client on the producer side.
			err = iscsi.With(producerExecutor).Authorize(ctx, iscsi.AuthorizeArguments{
				TargetIQN:         iscsi.IQN(targetID),
				TargetPassword:    targetPassword,
				InitiatorIQN:      iscsi.IQN(clientID),
				InitiatorPassword: consumerPassword,
			})
			if err != nil {
				return fmt.Errorf("failed to authorize client: %w", err)
			}

			err = iscsi.With(consumerExecutor).ConnectTarget(ctx, iscsi.ConnectTargetArguments{
				TargetAddress:     targetAddress,
				TargetIQN:         iscsi.IQN(targetID),
				TargetPassword:    targetPassword,
				InitiatorIQN:      iscsi.IQN(clientID),
				InitiatorPassword: consumerPassword,
			})
		case database.VolumeTransportTypeNVMEOF_TCP:
			// Authorize client on the producer side.
			err = nvmeof.With(producerExecutor).Authorize(ctx, nvmeof.AuthorizeArguments{
				TargetNQN:         nvmeof.NQN(targetID),
				TargetPassword:    targetPassword,
				InitiatorNQN:      nvmeof.NQN(clientID),
				InitiatorPassword: consumerPassword,
			})
			if err != nil {
				return fmt.Errorf("failed to authorize client: %w", err)
			}

			err = nvmeof.With(consumerExecutor).ConnectTarget(ctx, nvmeof.ConnectTargetArguments{
				TargetAddress:     targetAddress,
				TargetNQN:         nvmeof.NQN(targetID),
				TargetPassword:    targetPassword,
				InitiatorNQN:      nvmeof.NQN(clientID),
				InitiatorPassword: consumerPassword,
			})
		case database.VolumeTransportTypeUNSPECIFIED:
			fallthrough
		default:
			return fmt.Errorf("no transport specified on volume")
		}
		if err != nil {
			return fmt.Errorf("failed to connect volume: %w", err)
		}
		return nil
	})
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound || strings.Contains(err.Error(), "not found") {
			if _, ok := err.(*connect.Error); ok {
				return nil, err
			}
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.ConnectVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) DisconnectVolume(ctx context.Context, req *connect.Request[zfsilov1.DisconnectVolumeRequest]) (*connect.Response[zfsilov1.DisconnectVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	switch {
	case !volumedb.IsPublished():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not published"))
	case !volumedb.IsConnected():
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
		}
		return connect.NewResponse(&zfsilov1.DisconnectVolumeResponse{Volume: volumeapi}), nil
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is mounted"))
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		producerExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
		if err != nil {
			return err
		}

		consumerExecutor, connectHost, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
		if err != nil {
			return err
		}

		transport := volumedb.Transport.Data()
		var targetID, targetAddress string
		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			targetID = transport.ISCSI.TargetIQN
			targetAddress = transport.ISCSI.TargetAddress
		case database.VolumeTransportTypeNVMEOF_TCP:
			targetID = transport.NVMEOF.TargetNQN
			targetAddress = transport.NVMEOF.TargetAddress
		case database.VolumeTransportTypeUNSPECIFIED:
			return fmt.Errorf("no transport specified for volume staging")
		}

		clientID, err := getClientID(connectHost, transport)
		if err != nil {
			return fmt.Errorf("failed to get client ID: %w", err)
		}

		// Clear initiator details
		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			transport.ISCSI.InitiatorIQN = ""
			transport.ISCSI.InitiatorPassword = ""
		case database.VolumeTransportTypeNVMEOF_TCP:
			transport.NVMEOF.InitiatorNQN = ""
			transport.NVMEOF.InitiatorPassword = ""
		case database.VolumeTransportTypeUNSPECIFIED:
			return fmt.Errorf("no transport specified for initiator detail clearing")
		}
		volumedb.Transport = datatypes.NewJSONType(transport)

		volumedb.ClientHost = ""
		volumedb.Status = database.VolumeStatusPUBLISHED

		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			err = iscsi.With(consumerExecutor).DisconnectTarget(ctx, iscsi.DisconnectTargetArguments{
				TargetIQN:     iscsi.IQN(targetID),
				TargetAddress: targetAddress,
			})
			if err != nil {
				return fmt.Errorf("failed to disconnect volume: %w", err)
			}

			// Unauthorize client on the producer side.
			err = iscsi.With(producerExecutor).Unauthorize(ctx, iscsi.UnauthorizeArguments{
				TargetIQN:    iscsi.IQN(targetID),
				InitiatorIQN: iscsi.IQN(clientID),
			})
		case database.VolumeTransportTypeNVMEOF_TCP:
			err = nvmeof.With(consumerExecutor).DisconnectTarget(ctx, nvmeof.DisconnectTargetArguments{
				TargetNQN: nvmeof.NQN(targetID),
			})
			if err != nil {
				return fmt.Errorf("failed to disconnect volume: %w", err)
			}

			// Unauthorize client on the producer side.
			err = nvmeof.With(producerExecutor).Unauthorize(ctx, nvmeof.UnauthorizeArguments{
				TargetNQN:    nvmeof.NQN(targetID),
				InitiatorNQN: nvmeof.NQN(clientID),
			})
		case database.VolumeTransportTypeUNSPECIFIED:
			fallthrough
		default:
			return fmt.Errorf("no transport specified on volume")
		}
		if err != nil {
			return fmt.Errorf("failed to unauthorize client: %w", err)
		}

		return nil
	})
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound || strings.Contains(err.Error(), "not found") {
			if _, ok := err.(*connect.Error); ok {
				return nil, err
			}
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.DisconnectVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) StageVolume(ctx context.Context, req *connect.Request[zfsilov1.StageVolumeRequest]) (*connect.Response[zfsilov1.StageVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	switch {
	case !volumedb.IsPublished():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not published"))
	case !volumedb.IsConnected():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not connected"))
	case volumedb.IsStaged():
		if volumedb.StagingPath == req.Msg.StagingPath {
			volumeapi, err := s.converter.FromDBToAPI(volumedb)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
			}
			return connect.NewResponse(&zfsilov1.StageVolumeResponse{Volume: volumeapi}), nil
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("volume is already staged at %s", volumedb.StagingPath))
	}

	volumedb.StagingPath = req.Msg.StagingPath
	volumedb.Status = database.VolumeStatusSTAGED

	err = s.database.Transaction(func(tx *gorm.DB) error {
		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		consumerExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
		if err != nil {
			return err
		}

		transport := volumedb.Transport.Data()
		var targetID, targetAddress string
		switch transport.Type {
		case database.VolumeTransportTypeISCSI:
			targetID = transport.ISCSI.TargetIQN
			targetAddress = transport.ISCSI.TargetAddress
		case database.VolumeTransportTypeNVMEOF_TCP:
			targetID = transport.NVMEOF.TargetNQN
			targetAddress = transport.NVMEOF.TargetAddress
		case database.VolumeTransportTypeUNSPECIFIED:
			return fmt.Errorf("no transport specified for volume staging")
		}

		// Wait for block device to appear on the client side.
		devicePath, err := volumedb.DevicePathClient(targetAddress, targetID)
		if err != nil {
			return fmt.Errorf("failed to get device path: %w", err)
		}
		exists, err := fs.With(consumerExecutor).Exists(ctx, fs.ExistsArguments{
			Device:  devicePath,
			Timeout: 30 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("failed to check for block device %s: %w", devicePath, err)
		}
		if !exists {
			return fmt.Errorf("block device %s not found on client", devicePath)
		}

		if volumedb.Mode == database.VolumeModeFILESYSTEM {
			// Check if filesystem exists.
			fsType, err := fs.With(consumerExecutor).GetFSType(ctx, devicePath)
			if err != nil {
				return fmt.Errorf("failed to get filesystem type: %w", err)
			}

			if fsType == "" {
				// Format the device.
				err = fs.With(consumerExecutor).Format(ctx, fs.FormatArguments{
					Device:        devicePath,
					WaitForDevice: false,
				})
				if err != nil {
					return fmt.Errorf("failed to format device: %w", err)
				}
			}
		}

		// Create staging path.
		_, err = literal.With(consumerExecutor).Run(ctx, fmt.Sprintf("mkdir -m 0750 -p %s", volumedb.StagingPath))
		if err != nil {
			return fmt.Errorf("failed to create staging path: %w", err)
		}

		// Mount volume to staging path.
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

		err = mount.With(consumerExecutor).Mount(ctx, mountArgs)
		if err != nil {
			return fmt.Errorf("failed to mount volume to staging path: %w", err)
		}

		return nil
	})
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound || strings.Contains(err.Error(), "not found") {
			if _, ok := err.(*connect.Error); ok {
				return nil, err
			}
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.StageVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) UnstageVolume(ctx context.Context, req *connect.Request[zfsilov1.UnstageVolumeRequest]) (*connect.Response[zfsilov1.UnstageVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	switch {
	case !volumedb.IsStaged():
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
		}
		return connect.NewResponse(&zfsilov1.UnstageVolumeResponse{Volume: volumeapi}), nil
	case volumedb.IsMounted():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is still mounted to one or more target paths"))
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		previousStagingPath := volumedb.StagingPath
		volumedb.StagingPath = ""
		volumedb.Status = database.VolumeStatusCONNECTED

		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		consumerExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
		if err != nil {
			return err
		}

		err = mount.With(consumerExecutor).Umount(ctx, mount.UmountArguments{
			Path: previousStagingPath,
		})
		if err != nil {
			return fmt.Errorf("failed to umount volume from staging path: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to unstage volume: %w", err))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.UnstageVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) MountVolume(ctx context.Context, req *connect.Request[zfsilov1.MountVolumeRequest]) (*connect.Response[zfsilov1.MountVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	switch {
	case !volumedb.IsPublished():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not published"))
	case !volumedb.IsConnected():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not connected"))
	case !volumedb.IsStaged():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not staged"))
	}

	// Check if already mounted to this path.
	if indexOf(volumedb.TargetPaths, req.Msg.MountPath) != -1 {
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
		}
		return connect.NewResponse(&zfsilov1.MountVolumeResponse{Volume: volumeapi}), nil
	}

	volumedb.TargetPaths = append(volumedb.TargetPaths, req.Msg.MountPath)
	volumedb.Status = database.VolumeStatusMOUNTED

	err = s.database.Transaction(func(tx *gorm.DB) error {
		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		consumerExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
		if err != nil {
			return err
		}

		// Create mount path.
		if volumedb.Mode == database.VolumeModeBLOCK {
			_, err := literal.With(consumerExecutor).Run(ctx, fmt.Sprintf("install -m 0644 /dev/null %s", req.Msg.MountPath))
			if err != nil {
				return fmt.Errorf("failed to touch mount path: %w", err)
			}
		} else {
			_, err := literal.With(consumerExecutor).Run(ctx, fmt.Sprintf("mkdir -m 0750 -p %s", req.Msg.MountPath))
			if err != nil {
				return fmt.Errorf("failed to touch mount path: %w", err)
			}
		}

		// Perform bind mount from staging path to mount path.
		err = mount.With(consumerExecutor).Mount(ctx, mount.MountArguments{
			SourcePath: volumedb.StagingPath,
			TargetPath: req.Msg.MountPath,
			Options:    []string{"bind"},
		})
		if err != nil {
			return fmt.Errorf("failed to bind mount volume: %w", err)
		}

		if volumedb.Mode == database.VolumeModeFILESYSTEM {
			// TODO: I should properly expose the volume to non-root users.
			_, err = literal.With(consumerExecutor).Run(ctx, fmt.Sprintf("chmod 0777 %s", req.Msg.MountPath))
			if err != nil {
				return fmt.Errorf("failed to chmod mount path: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound || strings.Contains(err.Error(), "not found") {
			if _, ok := err.(*connect.Error); ok {
				return nil, err
			}
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.MountVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) UnmountVolume(ctx context.Context, req *connect.Request[zfsilov1.UnmountVolumeRequest]) (*connect.Response[zfsilov1.UnmountVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	index := indexOf(volumedb.TargetPaths, req.Msg.MountPath)
	if index == -1 {
		// Already unmounted from this path or never mounted.
		volumeapi, err := s.converter.FromDBToAPI(volumedb)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
		}
		return connect.NewResponse(&zfsilov1.UnmountVolumeResponse{Volume: volumeapi}), nil
	}

	// Remove from list.
	volumedb.TargetPaths = append(volumedb.TargetPaths[:index], volumedb.TargetPaths[index+1:]...)
	if len(volumedb.TargetPaths) == 0 {
		volumedb.Status = database.VolumeStatusSTAGED
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		_, err = gorm.G[*database.Volume](tx).Updates(ctx, volumedb)
		if err != nil {
			return fmt.Errorf("failed to update volume in database: %w", err)
		}

		consumerExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
		if err != nil {
			return err
		}

		err = mount.With(consumerExecutor).Umount(ctx, mount.UmountArguments{
			Path: req.Msg.MountPath,
		})
		if err != nil {
			return fmt.Errorf("failed to umount volume: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to unmount volume: %w", err))
	}

	volumeapi, err := s.converter.FromDBToAPI(volumedb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.UnmountVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) StatsVolume(ctx context.Context, req *connect.Request[zfsilov1.StatsVolumeRequest]) (*connect.Response[zfsilov1.StatsVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	switch {
	case !volumedb.IsPublished():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not published"))
	case !volumedb.IsConnected():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not connected"))
	case !volumedb.IsStaged():
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("volume is not staged"))
	}

	producerExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ServerHost)
	if err != nil {
		return nil, err
	}

	consumerExecutor, _, err := s.getExecutorForHost(ctx, volumedb.ClientHost)
	if err != nil {
		return nil, err
	}

	var usage []*zfsilov1.StatsVolumeResponse_Stats_Usage
	switch volumedb.Mode {
	case database.VolumeModeBLOCK:
		var values []int64
		for _, prop := range []string{"used", "usedds"} {
			valueString, err := zfs.With(producerExecutor).GetProperty(ctx, zfs.GetPropertyArguments{
				Name:        volumedb.DatasetID,
				PropertyKey: prop,
			})
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get stats property %s: %w", prop, err))
			}

			value, err := strconv.ParseInt(valueString, 10, 64)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse property %s: %w", prop, err))
			}
			values = append(values, value)
		}
		usage = append(usage, &zfsilov1.StatsVolumeResponse_Stats_Usage{
			Total:     values[0],
			Used:      values[1],
			Available: values[0] - values[1],
			Unit:      zfsilov1.StatsVolumeResponse_Stats_Usage_UNIT_BYTES,
		})
	case database.VolumeModeFILESYSTEM:
		// Use staging path for stats.
		statsPath := volumedb.StagingPath

		valueString, err := literal.With(consumerExecutor).Run(ctx, fmt.Sprintf(
			"df -BK '%s' --output=size,used,avail,itotal,iused,iavail | sed 1d",
			statsPath,
		))
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get stats: %w", err))
		}

		valueParts := strings.Fields(valueString)

		totalBytes, err := strconv.ParseInt(strings.TrimSuffix(valueParts[0], "K"), 10, 64)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse part[0]=%s: %w", valueParts[0], err))
		}
		totalBytes *= 1024
		usedBytes, err := strconv.ParseInt(strings.TrimSuffix(valueParts[1], "K"), 10, 64)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse part[1]=%s: %w", valueParts[1], err))
		}
		usedBytes *= 1024
		availableBytes, err := strconv.ParseInt(strings.TrimSuffix(valueParts[2], "K"), 10, 64)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse part[2]=%s: %w", valueParts[2], err))
		}
		availableBytes *= 1024
		totalInodes, err := strconv.ParseInt(valueParts[3], 10, 64)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse part[3]=%s: %w", valueParts[3], err))
		}
		usedInodes, err := strconv.ParseInt(valueParts[4], 10, 64)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse part[4]=%s: %w", valueParts[4], err))
		}
		availableInodes, err := strconv.ParseInt(valueParts[5], 10, 64)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse part[5]=%s: %w", valueParts[5], err))
		}

		usage = append(
			usage,
			&zfsilov1.StatsVolumeResponse_Stats_Usage{
				Total:     totalBytes,
				Used:      usedBytes,
				Available: availableBytes,
				Unit:      zfsilov1.StatsVolumeResponse_Stats_Usage_UNIT_BYTES,
			},
			&zfsilov1.StatsVolumeResponse_Stats_Usage{
				Total:     totalInodes,
				Used:      usedInodes,
				Available: availableInodes,
				Unit:      zfsilov1.StatsVolumeResponse_Stats_Usage_UNIT_INODES,
			},
		)
	case database.VolumeModeUNSPECIFIED:
		fallthrough
	default:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unsupported volume mode: %s", volumedb.Mode))
	}

	return connect.NewResponse(&zfsilov1.StatsVolumeResponse{Stats: &zfsilov1.StatsVolumeResponse_Stats{
		Usage: usage,
	}}), nil
}

func (s *VolumeService) SyncVolume(ctx context.Context, req *connect.Request[zfsilov1.SyncVolumeRequest]) (*connect.Response[zfsilov1.SyncVolumeResponse], error) {
	volumedb, err := gorm.G[*database.Volume](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get volume: %w", err))
	}

	if err = s.syncer.Sync(ctx, volumedb); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to sync volume: %w", err))
	}
	return connect.NewResponse(&zfsilov1.SyncVolumeResponse{}), nil
}

func (s *VolumeService) SyncVolumes(ctx context.Context, _ *connect.Request[zfsilov1.SyncVolumesRequest]) (*connect.Response[zfsilov1.SyncVolumesResponse], error) {
	volumedbs, err := gorm.G[*database.Volume](s.database).Find(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list volumes for sync: %w", err))
	}

	var syncErrors []string
	for _, volumedb := range volumedbs {
		err := s.syncer.Sync(ctx, volumedb)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("volume %s: %s", volumedb.ID, err))
		}
	}

	if len(syncErrors) > 0 {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to sync volumes: %s", strings.Join(syncErrors, "; ")))
	}
	return connect.NewResponse(&zfsilov1.SyncVolumesResponse{}), nil
}

func (s *VolumeService) getExecutorForHost(ctx context.Context, hostID string) (libcommand.Executor, *database.Host, error) {
	if hostID == "" {
		return nil, nil, connect.NewError(connect.CodeInvalidArgument, errors.New("host ID is empty"))
	}
	// We search by ID, name, or any of the IDs in the JSON list.
	// NOTE: We use SQLite specific JSON function here.
	host, err := gorm.G[*database.Host](s.database).Where("id = ? OR name = ? OR EXISTS (SELECT 1 FROM json_each(identifiers) WHERE value = ?)", hostID, hostID, hostID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("host %s not found", hostID))
		}
		return nil, nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get host %s: %w", hostID, err))
	}
	executor, err := s.executorFactory.BuildExecutor(host)
	if err != nil {
		return nil, nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build executor for host %s: %w", hostID, err))
	}
	return executor, host, nil
}
