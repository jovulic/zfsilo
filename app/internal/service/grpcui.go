package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"

	"github.com/fullstorydev/grpcui/standalone"
	slogctx "github.com/veqryn/slog-context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type GRPCUIHandler struct {
	serverURI string
	mu        sync.Mutex
	setup     bool
	cc        *grpc.ClientConn
	delegate  http.Handler
}

func NewGRPCUIHandler(serverURI string) *GRPCUIHandler {
	return &GRPCUIHandler{
		serverURI: serverURI,
	}
}

func (g *GRPCUIHandler) Start(ctx context.Context) error {
	return g.init(ctx)
}

func (g *GRPCUIHandler) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.setup = false
	if err := g.cc.Close(); err != nil {
		return fmt.Errorf("failed to close client conn: %w", err)
	}
	return nil
}

func (g *GRPCUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !g.setup {
		if err := g.init(context.Background()); err != nil {
			slogctx.Error(ctx, "unexpected error initializing grpcui", slogctx.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	g.delegate.ServeHTTP(w, r)
}

func (g *GRPCUIHandler) init(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	cc, err := grpc.NewClient(g.serverURI,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	if err != nil {
		return fmt.Errorf("failed to create client conn: %w", err)
	}
	g.cc = cc

	handler, err := standalone.HandlerViaReflection(ctx, cc, g.serverURI)
	if err != nil {
		return fmt.Errorf("failed to create grpc ui handler: %w", err)
	}
	g.delegate = handler

	g.setup = true
	return nil
}
