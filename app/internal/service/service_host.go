package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	converteriface "github.com/jovulic/zfsilo/app/internal/converter/iface"
	"github.com/jovulic/zfsilo/app/internal/database"
	structpb "google.golang.org/protobuf/types/known/structpb"
	"gorm.io/gorm"
)

const (
	listHostsDefaultPageSize = 25
	listHostsMaxPageSize     = 100
)

// applyHostUpdate modifies an existing Host object with fields from a
// protobuf Struct. It returns an error if any of the provided fields have an
// incorrect type.
func applyHostUpdate(
	existingHost *zfsilov1.Host,
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
		case "connection":
			structValue, ok := value.GetKind().(*structpb.Value_StructValue)
			if !ok {
				return &FieldTypeError{
					FieldName:    key,
					ExpectedType: "object",
					ActualType:   fmt.Sprintf("%T", value.GetKind()),
				}
			}
			fields := structValue.StructValue.GetFields()
			conn := &zfsilov1.Host_Connection{}
			var runAsRoot bool
			if v, ok := fields["run_as_root"]; ok {
				runAsRoot = v.GetBoolValue()
			}
			if _, ok := fields["local"]; ok {
				conn.Type = &zfsilov1.Host_Connection_Local_{
					Local: &zfsilov1.Host_Connection_Local{
						RunAsRoot: runAsRoot,
					},
				}
			} else if v, ok := fields["remote"]; ok {
				remoteFields := v.GetStructValue().GetFields()
				conn.Type = &zfsilov1.Host_Connection_Remote_{
					Remote: &zfsilov1.Host_Connection_Remote{
						Address:   remoteFields["address"].GetStringValue(),
						Port:      int32(remoteFields["port"].GetNumberValue()),
						Username:  remoteFields["username"].GetStringValue(),
						Password:  remoteFields["password"].GetStringValue(),
						RunAsRoot: runAsRoot,
					},
				}
			}
			existingHost.Connection = conn
		case "ids":
			listValue, ok := value.GetKind().(*structpb.Value_ListValue)
			if !ok {
				return &FieldTypeError{
					FieldName:    key,
					ExpectedType: "list",
					ActualType:   fmt.Sprintf("%T", value.GetKind()),
				}
			}
			newIDs := make([]string, 0, len(listValue.ListValue.Values))
			for _, v := range listValue.ListValue.Values {
				newIDs = append(newIDs, v.GetStringValue())
			}
			existingHost.Ids = newIDs
		case "key":
			existingHost.Key = value.GetStringValue()
		default:
			// Silently ignore immutable, read-only, or unknown fields.
			// skip
		}
	}

	return nil
}

type HostService struct {
	zfsilov1connect.UnimplementedHostServiceHandler

	database  *gorm.DB
	converter converteriface.HostConverter
}

func NewHostService(
	database *gorm.DB,
	converter converteriface.HostConverter,
) *HostService {
	return &HostService{
		database:  database,
		converter: converter,
	}
}

func (s *HostService) GetHost(ctx context.Context, req *connect.Request[zfsilov1.GetHostRequest]) (*connect.Response[zfsilov1.GetHostResponse], error) {
	hostdb, err := gorm.G[*database.Host](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("host does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get host: %w", err))
	}

	hostapi, err := s.converter.FromDBToAPI(hostdb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map host: %w", err))
	}

	return connect.NewResponse(&zfsilov1.GetHostResponse{Host: hostapi}), nil
}

func (s *HostService) ListHosts(ctx context.Context, req *connect.Request[zfsilov1.ListHostsRequest]) (*connect.Response[zfsilov1.ListHostsResponse], error) {
	// Determine the offset and limit parameters.
	var offset, limit int

	pageSize := int(req.Msg.PageSize)
	if pageSize <= 0 {
		pageSize = listHostsDefaultPageSize
	}
	if pageSize > listHostsMaxPageSize {
		pageSize = listHostsMaxPageSize
	}

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

	// Execute the database query.
	hostdbs, err := gorm.G[*database.Host](s.database).
		Order("create_time desc").
		Offset(offset).
		Limit(limit).
		Find(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get hosts from database: %w", err))
	}

	// Convert database models to API models.
	hostapis, err := s.converter.FromDBToAPIList(hostdbs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to map database hosts to API: %w", err))
	}

	// Determine the next page token.
	var nextPageTokenString string
	if len(hostapis) == limit {
		nextPageToken := PageToken{
			Offset: offset + len(hostapis),
			Limit:  limit,
		}
		tokenStr, err := nextPageToken.Marshal()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal next page token: %w", err))
		}
		nextPageTokenString = tokenStr
	}

	return connect.NewResponse(&zfsilov1.ListHostsResponse{
		Hosts:         hostapis,
		NextPageToken: nextPageTokenString,
	}), nil
}

func (s *HostService) CreateHost(ctx context.Context, req *connect.Request[zfsilov1.CreateHostRequest]) (*connect.Response[zfsilov1.CreateHostResponse], error) {
	hostdb, err := s.converter.FromAPIToDB(req.Msg.Host)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map host: %w", err))
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		// Create database entry.
		err := gorm.G[*database.Host](tx).Create(ctx, &hostdb)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("host already exists"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create host: %w", err))
	}

	hostapi, err := s.converter.FromDBToAPI(hostdb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map host: %w", err))
	}

	return connect.NewResponse(&zfsilov1.CreateHostResponse{Host: hostapi}), nil
}

func (s *HostService) UpdateHost(ctx context.Context, req *connect.Request[zfsilov1.UpdateHostRequest]) (*connect.Response[zfsilov1.UpdateHostResponse], error) {
	idValue := req.Msg.Host.GetFields()["id"]
	if idValue == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("host id must be defined"))
	}
	id := idValue.GetStringValue()

	hostdb, err := gorm.G[*database.Host](s.database).Where("id = ?", id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("host does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get host: %w", err))
	}

	hostapi, err := s.converter.FromDBToAPI(hostdb)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map host: %w", err))
	}

	err = applyHostUpdate(hostapi, req.Msg.Host)
	if err != nil {
		var errField *FieldTypeError
		if errors.As(err, &errField) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("failed to update host: %w", errField))
		}
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to apply update to host: %w", err))
	}

	hostdb, err = s.converter.FromAPIToDB(hostapi)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to map host: %w", err))
	}

	_, err = gorm.G[*database.Host](s.database).Updates(ctx, hostdb)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update host in database: %w", err))
	}

	return connect.NewResponse(&zfsilov1.UpdateHostResponse{Host: hostapi}), nil
}

func (s *HostService) DeleteHost(ctx context.Context, req *connect.Request[zfsilov1.DeleteHostRequest]) (*connect.Response[zfsilov1.DeleteHostResponse], error) {
	hostdb, err := gorm.G[*database.Host](s.database).Where("id = ?", req.Msg.Id).First(ctx)
	switch {
	case err == nil:
		// okay
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, connect.NewError(connect.CodeNotFound, errors.New("host does not exist"))
	default:
		return nil, connect.NewError(connect.CodeUnknown, fmt.Errorf("failed to get host: %w", err))
	}

	// Check if host is referenced by any volumes.
	count, err := gorm.G[*database.Volume](s.database).Where("server_host = ? OR client_host = ?", hostdb.Name, hostdb.Name).Count(ctx, "id")
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check volume references: %w", err))
	}
	if count > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("host is still referenced by %d volumes", count))
	}

	err = s.database.Transaction(func(tx *gorm.DB) error {
		_, err = gorm.G[*database.Host](tx).Where("id = ?", req.Msg.Id).Delete(ctx)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete host: %w", err))
	}

	return connect.NewResponse(&zfsilov1.DeleteHostResponse{}), nil
}
