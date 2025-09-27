package service

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	converteriface "github.com/jovulic/zfsilo/app/internal/converter/iface"
	"github.com/jovulic/zfsilo/app/internal/database"
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

	database  *gorm.DB
	converter converteriface.VolumeConverter
}

func NewVolumeService(
	database *gorm.DB,
	converter converteriface.VolumeConverter,
) *VolumeService {
	return &VolumeService{
		database:  database,
		converter: converter,
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

	err = gorm.G[database.Volume](s.database).Create(ctx, &volumedb)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("volume already exists"))
	default:
		slogctx.Error(ctx, "failed to create volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
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
	switch {
	case err == nil:
		// okay
	case errors.Is(err, ErrFieldTypeError):
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("failed to update volume: %w", err))
	default:
		slogctx.Error(ctx, "failed to apply update to volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	volumedb, err = s.converter.FromAPIToDB(volumeapi)
	if err != nil {
		slogctx.Error(ctx, "failed to map volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	_, err = gorm.G[database.Volume](s.database).Updates(ctx, volumedb)
	if err != nil {
		slogctx.Error(ctx, "failed to update volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}

	return connect.NewResponse(&zfsilov1.UpdateVolumeResponse{Volume: volumeapi}), nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, req *connect.Request[zfsilov1.DeleteVolumeRequest]) (*connect.Response[zfsilov1.DeleteVolumeResponse], error) {
	_, err := gorm.G[database.Volume](s.database).Where("id = ?", req.Msg.Id).Delete(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("volume does not exist"))
	default:
		slogctx.Error(ctx, "failed to get volume", slogctx.Err(err))
		return nil, connect.NewError(connect.CodeUnknown, errors.New("unknown error"))
	}
	return connect.NewResponse(&zfsilov1.DeleteVolumeResponse{}), nil
}
