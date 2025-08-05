package service

import (
	"io"
	"net/http"

	slogctx "github.com/veqryn/slog-context"

	"github.com/jovulic/zfsilo/api"
)

func NewV1OpenAPIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		file, err := api.V1OpenAPI.Open("gen/openapi/zfsilo/v1/zfsilo.openapi.yaml")
		if err != nil {
			slogctx.Error(ctx, "unexpected error opening openapi specification", slogctx.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.Copy(w, file)
	})
}
