package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/skovtunenko/graterm"
	slogctx "github.com/veqryn/slog-context"

	"github.com/jovulic/zfsilo/app/internal/config"
)

var Version string

var CLI struct {
	Start struct {
		Config string `help:"Path to config file. A value of \"-\" will cause it to read from stdin." name:"config" required:"" type:"path"`
	} `cmd:"" help:"Start zfsilo."`
}

func main() {
	ctx := context.Background()
	kongCtx := kong.Parse(&CLI,
		kong.Name("zfsilo"),
		kong.Description("A ZFS-based network storage layer over iSCSI."),
		kong.UsageOnError(),
	)
	switch kongCtx.Command() {
	case "start":
		configValue := CLI.Start.Config
		conf, err := config.BuildConfig(ctx, configValue)
		if err != nil {
			kongCtx.FatalIfErrorf(fmt.Errorf("failed to build config: %w", err))
		}

		logMode, err := mapLogMode(conf.Log.Mode)
		if err != nil {
			kongCtx.FatalIfErrorf(fmt.Errorf("failed to parse log mode: %w", err))
		}
		logLevel, err := conf.Log.Level.SlogLevel()
		if err != nil {
			kongCtx.FatalIfErrorf(fmt.Errorf("failed to parse log level: %w", err))
		}
		log := buildLogger(logMode, logLevel)
		slog.SetDefault(log)
		ctx = slogctx.NewCtx(ctx, log)
		ctx = slogctx.With(ctx, slog.String("version", Version))
		slogctx.Info(ctx, "application ready", slog.Any("config", conf))

		var term *graterm.Terminator
		term, ctx = graterm.NewWithSignals(ctx, syscall.SIGINT, syscall.SIGTERM)

		app, err := WireApp(ctx, conf, term)
		if err != nil {
			slogctx.Error(ctx, "failed to wire app", slogctx.Err(err))
			os.Exit(1)
		}

		_ = app

		if err := term.Wait(ctx, time.Minute); err != nil {
			slogctx.Error(ctx, "failed to gracefully terminate application", slogctx.Err(err))
			os.Exit(1)
		}
		slogctx.Info(ctx, "successfully terminated the application")
	}
}
