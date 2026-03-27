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
	"github.com/jovulic/zfsilo/app/internal/database"
	"gorm.io/gorm"
)

type Service struct {
	zfsilov1connect.UnimplementedServiceHandler

	database        *gorm.DB
	executorFactory *command.ExecutorFactory
}

func NewService(
	database *gorm.DB,
	executorFactory *command.ExecutorFactory,
) *Service {
	return &Service{
		database:        database,
		executorFactory: executorFactory,
	}
}

func (s *Service) GetCapacity(ctx context.Context, req *connect.Request[zfsilov1.GetCapacityRequest]) (*connect.Response[zfsilov1.GetCapacityResponse], error) {
	hosts, err := gorm.G[*database.Host](s.database).Where("role = ?", database.HostRoleSERVER).Find(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to find server hosts: %w", err))
	}

	var totalAvail int64

	for _, host := range hosts {
		executor, err := s.executorFactory.BuildExecutor(host)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build executor for host %s: %w", host.ID, err))
		}

		availString, err := zfs.With(executor).GetProperty(ctx, zfs.GetPropertyArguments{
			Name:        "tank",
			PropertyKey: "avail",
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get dataset available capacity for host %s: %w", host.ID, err))
		}

		avail, err := strconv.ParseInt(availString, 10, 64)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to parse dataset available capacity for host %s: %w", host.ID, err))
		}

		totalAvail += avail
	}

	return connect.NewResponse(&zfsilov1.GetCapacityResponse{AvailableCapacityBytes: totalAvail}), nil
}
