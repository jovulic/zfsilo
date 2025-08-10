package service

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
)

type VolumeService struct {
	zfsilov1connect.UnimplementedVolumeServiceHandler
}

func NewVolumeService() *VolumeService {
	return &VolumeService{}
}

func (s *VolumeService) GetVolume(ctx context.Context, req *connect.Request[zfsilov1.GetVolumeRequest]) (*connect.Response[zfsilov1.GetVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.zfsilov1.VolumeService.GetVolume is not implemented"))
}

func (s *VolumeService) ListVolumes(ctx context.Context, req *connect.Request[zfsilov1.ListVolumesRequest]) (*connect.Response[zfsilov1.ListVolumesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.zfsilov1.VolumeService.ListVolumes is not implemented"))
}

func (s *VolumeService) UpdateVolume(ctx context.Context, req *connect.Request[zfsilov1.UpdateVolumeRequest]) (*connect.Response[zfsilov1.UpdateVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.zfsilov1.VolumeService.UpdateVolume is not implemented"))
}

func (s *VolumeService) DeleteVolume(ctx context.Context, req *connect.Request[zfsilov1.DeleteVolumeRequest]) (*connect.Response[zfsilov1.DeleteVolumeResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.VolumeService.DeleteVolume is not implemented"))
}
