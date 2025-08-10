package service

import (
	"context"
	"fmt"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

func newValidateInterceptor() connect.UnaryInterceptorFunc {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			protoMsg, ok := req.Any().(proto.Message)
			if !ok {
				connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("request body is malformed"))
			}
			if err := protovalidate.Validate(protoMsg); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid request: %w", err))
			}
			return next(ctx, req)
		}
	})
}
