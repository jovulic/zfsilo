package main

import (
	"fmt"
	"log/slog"
	"os"

	slogctx "github.com/veqryn/slog-context"
)

type LogMode int

const (
	LogModeJSON LogMode = iota
	LogModeText
)

func mapLogMode(mode string) (LogMode, error) {
	switch mode {
	case "JSON":
		return LogModeJSON, nil
	case "TEXT":
		return LogModeText, nil
	default:
		return 0, fmt.Errorf("unsupported log mode %s", mode)
	}
}

func buildLogger(mode LogMode, level slog.Level) *slog.Logger {
	switch mode {
	case LogModeJSON:
		handler := slogctx.NewHandler(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
				Level: level,
			}),
			nil,
		)
		return slog.New(handler)
	case LogModeText:
		handler := slogctx.NewHandler(
			slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: level,
			}),
			nil,
		)
		return slog.New(handler)
	default:
		panic("unreachable")
	}
}
