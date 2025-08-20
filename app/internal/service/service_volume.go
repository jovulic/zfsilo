package service

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	converteriface "github.com/jovulic/zfsilo/app/internal/converter/iface"
	"github.com/jovulic/zfsilo/app/internal/database"
	slogctx "github.com/veqryn/slog-context"
	"gorm.io/gorm"
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
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.zfsilov1.VolumeService.ListVolumes is not implemented"))
}

func (s *VolumeService) CreateVolume(ctx context.Context, req *connect.Request[zfsilov1.CreateVolumeRequest]) (*connect.Response[zfsilov1.CreateVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.zfsilov1.VolumeService.CreateVolume is not implemented"))
}

func (s *VolumeService) UpdateVolume(ctx context.Context, req *connect.Request[zfsilov1.UpdateVolumeRequest]) (*connect.Response[zfsilov1.UpdateVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.zfsilov1.VolumeService.UpdateVolume is not implemented"))
}

func (s *VolumeService) DeleteVolume(ctx context.Context, req *connect.Request[zfsilov1.DeleteVolumeRequest]) (*connect.Response[zfsilov1.DeleteVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.VolumeService.DeleteVolume is not implemented"))
}
