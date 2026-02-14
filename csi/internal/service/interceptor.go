package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	slogctx "github.com/veqryn/slog-context"
	"google.golang.org/grpc"
)

func LogUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		requestID := ulid.Make()
		ctx = slogctx.With(ctx, slog.String("requestId", requestID.String()))

		method := info.FullMethod
		ctx = slogctx.With(ctx, slog.String("method", method))

		slogctx.Info(ctx, "request received")

		startTime := time.Now()
		res, err := handler(ctx, req)
		endTime := time.Now()

		responseTime := endTime.Sub(startTime)
		slogctx.Info(ctx, "request completed", slog.Duration("responseTime", responseTime))

		return res, err
	}
}
