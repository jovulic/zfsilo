package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"connectrpc.com/grpcreflect"
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/app/internal/command"
	"github.com/jovulic/zfsilo/app/internal/config"
	converteriface "github.com/jovulic/zfsilo/app/internal/converter/iface"
	"github.com/jovulic/zfsilo/lib/selfcert"
	"github.com/samber/lo"
	"github.com/skovtunenko/graterm"
	slogctx "github.com/veqryn/slog-context"
	"gorm.io/gorm"
)

var WireSet = wire.NewSet(
	WireService,
	WireVolumeService,
	WireServer,
)

func WireService() *Service {
	return NewService()
}

func WireVolumeService(
	database *gorm.DB,
	converter converteriface.VolumeConverter,
	producer command.ProduceExecutor,
	consumers command.ConsumeExecutorMap,
) *VolumeService {
	return NewVolumeService(database, converter, producer, consumers)
}

func WireServer(
	ctx context.Context,
	conf config.Config,
	term *graterm.Terminator,
	service *Service,
	volumeService *VolumeService,
) (*http.Server, error) {
	cert, err := selfcert.GenerateCertificate()
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate: %w", err)
	}

	mux := http.NewServeMux()

	// Register services.
	{
		logInterceptor := newLogInterceptor(slogctx.FromCtx(ctx))

		type ConfigKey = struct {
			Identity string `json:"identity"`
			Token    string `json:"token"`
		}
		type Key = struct {
			identity string
			token    string
		}
		authnzInterceptor := newAuthnzInterceptor(
			lo.Map(conf.Service.Keys, func(item ConfigKey, index int) Key {
				return Key{
					identity: item.Identity,
					token:    item.Token,
				}
			}),
		)
		validateInterceptor := newValidateInterceptor()

		// Register root service.
		{
			path, handler := zfsilov1connect.NewServiceHandler(
				service,
				connect.WithInterceptors(
					logInterceptor,
					authnzInterceptor,
					validateInterceptor,
				),
			)
			mux.Handle(path, handler)
		}

		// Register volume service.
		{
			path, handler := zfsilov1connect.NewVolumeServiceHandler(
				volumeService,
				connect.WithInterceptors(
					logInterceptor,
					authnzInterceptor,
					validateInterceptor,
				),
			)
			mux.Handle(path, handler)
		}

		// Register grpc health.
		{
			checker := grpchealth.NewStaticChecker(
				zfsilov1connect.ServiceName,
				zfsilov1connect.VolumeServiceName,
			)
			mux.Handle(grpchealth.NewHandler(checker,
				connect.WithInterceptors(
					logInterceptor,
				),
			))
		}
	}

	// Register grpc reflection.
	{
		reflector := grpcreflect.NewStaticReflector(
			zfsilov1connect.ServiceName,
			zfsilov1connect.VolumeServiceName,
			grpchealth.HealthV1ServiceName,
		)
		mux.Handle(grpcreflect.NewHandlerV1(reflector))
		mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))
	}

	// Register the grpc ui route.
	grpcuiHandler := NewGRPCUIHandler(conf.Service.ExternalServerURI)
	mux.Handle("/", grpcuiHandler)

	// Register OpenAPI route.
	mux.Handle("/v1", NewV1OpenAPIHandler())

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
		WithOrder(6).
		WithName("grpcui-handler").
		Register(time.Minute, func(ctx context.Context) {
			if err := grpcuiHandler.Stop(ctx); err != nil {
				slogctx.Error(ctx, "failed to gracefully stop the grpcui handler", slogctx.Err(err))
			}
		})

	return server, nil
}
