//go:build wireinject

package main

import (
	"context"

	"github.com/google/wire"
	"github.com/jovulic/zfsilo/app/internal/config"
	"github.com/jovulic/zfsilo/app/internal/converter"
	"github.com/jovulic/zfsilo/app/internal/database"
	"github.com/jovulic/zfsilo/app/internal/service"
	"github.com/skovtunenko/graterm"
)

func WireApp(
	ctx context.Context,
	conf config.Config,
	term *graterm.Terminator,
) (*App, error) {
	wire.Build(service.WireSet, database.WireSet, converter.WireSet, NewApp)
	return new(App), nil
}
