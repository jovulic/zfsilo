package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"connectrpc.com/grpcreflect"
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/app/internal/config"
	"github.com/jovulic/zfsilo/lib/selfcert"
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

	// Register the grpc ui.
	grpcuiHandler := NewGRPCUIHandler(conf.Service.ExternalServerURI)
	mux.Handle("/", grpcuiHandler)

	server := &http.Server{
		Addr:    conf.Service.BindAddress,
		Handler: mux,
	}
	ln, err := net.Listen("tcp", conf.Service.BindAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to bind server to address %s: %w", conf.Service.BindAddress, err)
	}
	tlsListener := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2"},
	})
	go func() {
		if err := server.Serve(tlsListener); err != http.ErrServerClosed {
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

	// Block execution until the http server is running.
	for {
		conn, err := net.Dial("tcp", conf.Service.BindAddress)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(time.Second)
	}
	slogctx.Debug(ctx, "http server is running")

	// Start the grpcui server. This is delayed after mux registration as
	// it attempts to connect to the server as part of initialization.
	if err := grpcuiHandler.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start the grpcui handler: %w", err)
	}
	term.
		WithOrder(4).
		WithName("grpcui-handler").
		Register(time.Minute, func(ctx context.Context) {
			if err := grpcuiHandler.Stop(ctx); err != nil {
				slogctx.Error(ctx, "failed to gracefully stop the grpcui handler", slogctx.Err(err))
			}
		})

	return server, nil
}
