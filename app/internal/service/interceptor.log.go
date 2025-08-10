package service

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	ulid "github.com/oklog/ulid/v2"
	slogctx "github.com/veqryn/slog-context"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func newLogInterceptor(log *slog.Logger) connect.UnaryInterceptorFunc {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			ctx = slogctx.NewCtx(ctx, log)

			procedure := req.Spec().Procedure
			ctx = slogctx.With(ctx, slog.String("procedure", procedure))

			requestID := ulid.Make()
			ctx = slogctx.With(ctx, slog.String("requestId", requestID.String()))

			slogctx.Info(ctx, "request received")

			// We also log the request body, marshaled as JSON, if we are able to.
			{
				protoMsg, ok := req.Any().(proto.Message)
				if !ok {
					slogctx.Error(ctx, "request body for is not a proto.Message")
				} else {
					jsonBytes, err := protojson.Marshal(protoMsg)
					if err != nil {
						slogctx.Error(ctx, "failed to marshal request body to json", slogctx.Err(err), slog.Any("body", protoMsg))
					} else {
						slogctx.Info(ctx, "parsed body", slog.String("body", string(jsonBytes)))
					}
				}
			}

			startTime := time.Now()
			res, err := next(ctx, req)
			endTime := time.Now()

			responseTime := endTime.Sub(startTime)
			slogctx.Info(ctx, "request completed", slog.Duration("responseTime", responseTime))

			return res, err
		}
	})
}
