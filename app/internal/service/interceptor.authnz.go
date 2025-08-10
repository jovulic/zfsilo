package service

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	slogctx "github.com/veqryn/slog-context"
)

type authnzContextKey struct{}

type authnzContextValue struct {
	identity string
	token    string
}

func newAuthnzInterceptor(
	keys []struct {
		identity string
		token    string
	},
) connect.UnaryInterceptorFunc {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			authHeader := req.Header().Get("Authorization")
			if authHeader == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("no authorization header provided"))
			}

			// We currently only support bearer tokens, so that is what we assert here.
			if !strings.HasPrefix(authHeader, "Bearer") {
				return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authorization header is not a bearer token"))
			}

			// We intentionally iterate over a list and perform constant time compares.
			token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer"))
			identity := func() string {
				for _, key := range keys {
					if subtle.ConstantTimeCompare([]byte(token), []byte(key.token)) == 1 {
						return key.identity
					}
				}
				return ""
			}()
			if identity == "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("failed authorization"))
			}

			// Embed the authnz value into the context and extend the logger with
			// identity information if the logger exists.
			ctx = context.WithValue(ctx, authnzContextKey{}, authnzContextValue{
				identity: identity,
				token:    token,
			})
			ctx = slogctx.With(ctx, slog.String("identity", identity))

			return next(ctx, req)
		}
	})
}
