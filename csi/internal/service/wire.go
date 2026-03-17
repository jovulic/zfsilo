package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/csi/internal/config"
	"github.com/jovulic/zfsilo/lib/selfcert"
	"github.com/skovtunenko/graterm"
	slogctx "github.com/veqryn/slog-context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

var WireSet = wire.NewSet(
	WireCSIService,
	WireServer,
)

func buildClientIDs(confs []config.ConfigServiceClientID) (map[string]string, error) {
	clientIDs := make(map[string]string)
	for _, conf := range confs {
		var id string
		switch conf.Type {
		case "VALUE":
			id = conf.Value
		case "PATH":
			dir := path.Dir(conf.Value)
			base := path.Base(conf.Value)
			filesystem := os.DirFS(dir)
			valueBytes, err := fs.ReadFile(filesystem, base)
			if err != nil {
				return nil, fmt.Errorf("failed to read client id file %s: %w", conf.Value, err)
			}

			// The value comes in the following [example] form, so we remove the prefix.
			// InitiatorName=iqn.2003-01.org.linux-iscsi.thinkone
			id = strings.TrimPrefix(string(valueBytes), "InitiatorName=")
			id = strings.TrimSpace(id)
		default:
			return nil, fmt.Errorf("unknown client id type %s", conf.Type)
		}

		if strings.HasPrefix(id, "iqn.") {
			clientIDs["iscsi"] = id
		} else if strings.HasPrefix(id, "nqn.") {
			clientIDs["nvmeof"] = id
		} else {
			return nil, fmt.Errorf("unsupported client id format: %s", id)
		}
	}
	return clientIDs, nil
}

func WireCSIService(
	ctx context.Context,
	conf config.Config,
	term *graterm.Terminator,
) (*CSIService, error) {
	clientIDs, err := buildClientIDs(conf.Service.ClientIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to build client ids: %w", err)
	}
	service := NewCSIService(CSIServiceConfig{
		Secret:              string(conf.Service.Secret),
		ZFSiloAddress:       conf.Service.ZFSiloAddress,
		TargetPortalAddress: conf.Service.TargetPortalAddress,
		ClientIDs:           clientIDs,
		KnownClientIDs:      conf.Service.KnownClientIDs,
	})
	if err := service.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start csi service: %w", err)
	}
	term.
		WithOrder(3).
		WithName("csi-service").
		Register(time.Minute, func(ctx context.Context) {
			if err := service.Stop(ctx); err != nil {
				slogctx.Error(ctx, "failed to stop csi service", slog.Any("error", err))
			}
		})
	return service, nil
}

func WireServer(
	ctx context.Context,
	conf config.Config,
	term *graterm.Terminator,
	csiService *CSIService,
) (*grpc.Server, error) {
	network, address := func(address string) (string, string) {
		matcher := regexp.MustCompile("^(?:([a-z0-9]+)://)?(.*)$")
		parts := matcher.FindStringSubmatch(address)
		proto, addr := parts[1], parts[2]
		if proto == "" {
			proto = "tcp"
		}
		return proto, addr
	}(conf.Service.BindAddress)

	var grpcServerOptions []grpc.ServerOption
	{
		grpcServerOptions = append(
			grpcServerOptions,
			grpc.ChainUnaryInterceptor(
				LogUnaryServerInterceptor(),
			),
		)
		// We only add a certificate when we are dealing with a tcp network.
		if network == "" || network == "tcp" {
			cert, err := selfcert.GenerateCertificate()
			if err != nil {
				return nil, fmt.Errorf("failed to generate certificate: %w", err)
			}
			grpcServerOptions = append(
				grpcServerOptions,
				grpc.Creds(credentials.NewTLS(&tls.Config{
					Certificates: []tls.Certificate{cert},
					NextProtos:   []string{"h2"},
				})),
			)
		}
	}
	server := grpc.NewServer(grpcServerOptions...)
	csi.RegisterIdentityServer(server, csiService)
	csi.RegisterControllerServer(server, csiService)
	csi.RegisterNodeServer(server, csiService)
	reflection.Register(server)

	ln, err := net.Listen(network, address)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener on %s://%s", network, address)
	}
	go func() {
		if err := server.Serve(ln); err != nil {
			slogctx.Error(ctx, "unexpected error starting grpc server", slog.Any("error", err))
		}
	}()
	term.
		WithOrder(5).
		WithName("grpc-server").
		Register(time.Minute, func(ctx context.Context) {
			server.GracefulStop()
		})

	slogctx.Debug(ctx, "grpc server is running")

	return server, nil
}
