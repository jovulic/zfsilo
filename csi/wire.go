//go:build wireinject

package main

import (
	"context"

	"github.com/google/wire"
	"github.com/jovulic/zfsilo/csi/internal/config"
	"github.com/jovulic/zfsilo/csi/internal/service"
	"github.com/skovtunenko/graterm"
)

func WireApp(
	ctx context.Context,
	conf config.Config,
	term *graterm.Terminator,
) (*App, error) {
	wire.Build(NewApp, service.WireSet)
	return new(App), nil
}
