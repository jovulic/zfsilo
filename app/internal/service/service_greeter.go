package service

import (
	"context"
	"fmt"

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

func (g *GreeterService) SayHello(ctx context.Context, req *connect.Request[zfsilov1.SayHelloRequest]) (*connect.Response[zfsilov1.SayHelloResponse], error) {
	name := req.Msg.GetName()
	return connect.NewResponse(
		&zfsilov1.SayHelloResponse{
			Message: fmt.Sprintf("Hello %s!", name),
		},
	), nil
}
