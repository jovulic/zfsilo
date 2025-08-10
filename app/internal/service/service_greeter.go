package service

import (
	"context"
	"fmt"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
)

type GreeterService struct {
	zfsilov1connect.UnimplementedGreeterServiceHandler
}

func NewGreeterService() *GreeterService {
	return &GreeterService{}
}

func (g *GreeterService) SayHello(ctx context.Context, request *connect.Request[zfsilov1.SayHelloRequest]) (*connect.Response[zfsilov1.SayHelloResponse], error) {
	if err := protovalidate.Validate(request.Msg); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err))
	}

	name := request.Msg.GetName()

	return connect.NewResponse(
		&zfsilov1.SayHelloResponse{
			Message: fmt.Sprintf("Hello %s!", name),
		},
	), nil
}
