// Package service implements the interface defined in API.
package service

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
)

type Service struct {
	zfsilov1connect.UnimplementedServiceHandler
}

func NewService() *Service {
	return &Service{}
}

func (s *Service) GetCapacity(ctx context.Context, req *connect.Request[zfsilov1.GetCapacityRequest]) (*connect.Response[zfsilov1.GetCapacityResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("zfsilo.v1.Service.GetCapacity is not implemented"))
}
