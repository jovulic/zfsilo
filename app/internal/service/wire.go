package service

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/grpcreflect"
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/app/internal/config"
	"github.com/skovtunenko/graterm"
	slogctx "github.com/veqryn/slog-context"
)

var WireSet = wire.NewSet(
	WireGreeterService,
	WireServer,
)

func WireGreeterService() *GreeterService {
	return NewGreeterService()
}

func WireServer(
	ctx context.Context,
	conf config.Config,
	term *graterm.Terminator,
	greeterService *GreeterService,
) (*http.Server, error) {
	server := new(http.Server)
	server.Addr = conf.Service.BindAddress

	mux := http.NewServeMux()

	// Register services.
	{
		path, handler := zfsilov1connect.NewGreeterServiceHandler(
			greeterService,
		)
		mux.Handle(path, handler)
	}

	// Register grpc reflection.
	{
		reflector := grpcreflect.NewStaticReflector(
			zfsilov1connect.GreeterServiceName,
		)
		mux.Handle(grpcreflect.NewHandlerV1(reflector))
		mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))
	}

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slogctx.Error(ctx, "unexpected error starting http server", slog.Any("error", err))
		}
	}()
	term.
		WithOrder(5).
		WithName("http-server").
		Register(time.Minute, func(ctx context.Context) {
			if err := server.Shutdown(ctx); err != nil {
				slogctx.Error(ctx, "failed to shutdown http server", slog.Any("error", err))
			}
		})

	return server, nil
}
