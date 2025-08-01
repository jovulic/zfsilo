package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/grpcreflect"
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/app/internal/config"
	"github.com/jovulic/zfsilo/lib/selfcert"
	"github.com/skovtunenko/graterm"
	slogctx "github.com/veqryn/slog-context"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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
	cert, err := selfcert.GenerateCertificate()
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate: %w", err)
	}

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

	server := &http.Server{
		Addr:    conf.Service.BindAddress,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
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
