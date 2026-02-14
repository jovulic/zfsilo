// Package service implements the interface defined in API.
package service

import (
	"context"
	"fmt"
	"strconv"

	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/app/internal/command"
	"github.com/jovulic/zfsilo/app/internal/command/zfs"
)

type Service struct {
	zfsilov1connect.UnimplementedServiceHandler

	producer command.ProduceExecutor
}

func NewService(
	producer command.ProduceExecutor,
) *Service {
	return &Service{
		producer: producer,
	}
}

func (s *Service) GetCapacity(ctx context.Context, req *connect.Request[zfsilov1.GetCapacityRequest]) (*connect.Response[zfsilov1.GetCapacityResponse], error) {
	availString, err := zfs.With(s.producer).GetProperty(ctx, zfs.GetPropertyArguments{
		Name:        "tank",
		PropertyKey: "avail",
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get dataset available capacity: %w", err))
	}

	avail, err := strconv.ParseInt(availString, 10, 64)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse dataset available capacity: %w", err))
	}

	return connect.NewResponse(&zfsilov1.GetCapacityResponse{AvailableCapacityBytes: avail}), nil
}
