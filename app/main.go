package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/go-playground/mold/v4/modifiers"
	"github.com/go-playground/validator/v10"
	"github.com/skovtunenko/graterm"
	slogctx "github.com/veqryn/slog-context"
)

var Version string

var CLI struct {
	Start struct {
		Config string `help:"Path to config file. A value of \"-\" will cause it to read from stdin." name:"config" required:"" type:"path"`
	} `cmd:"" help:"Start zfsilo."`
}

type SecretValueList []SecretValue

func (l SecretValueList) Value() []string {
	var rets []string
	for _, e := range l {
		// We convert to a []byte first to prevent clearing the value in the conv.
		ret := string([]byte(e))
		rets = append(rets, ret)
	}
	return rets
}

type SecretValue string

func (v SecretValue) Value() string {
	return string([]byte(v))
}

func (SecretValue) String() string {
	return "REDACTED"
}

func (SecretValue) MarshalJSON() ([]byte, error) {
	return json.Marshal("REDACTED")
}

type Config struct {
	Log struct {
		Mode  string `json:"mode"  mod:"default=JSON" validate:"oneof=JSON TEXT"`
		Level string `json:"level" mod:"default=INFO" validate:"oneof=DEBUG INFO WARN ERROR"`
	} `json:"log"`
}

func buildConfig(ctx context.Context, configValue string) (Config, error) {
	configData, err := func(config string) ([]byte, error) {
		if config == "-" {
			stdinReader := bufio.NewReader(os.Stdin)
			stdinBytes, err := io.ReadAll(stdinReader)
			if err != nil {
				return nil, fmt.Errorf("failed to read from stdin: %w", err)
			}
			return stdinBytes, nil
		} else {
			configPath := config
			configData, err := os.ReadFile(configPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read config file: %w", err)
			}
			return configData, nil
		}
	}(configValue)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config: %w", err)
	}

	// Unmarshal the config file into the config struct.
	// TODO: Detect type and support more than just json.
	var config Config
	if err := json.Unmarshal(configData, &config); err != nil {
		return Config{}, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply any tag information.
	t := modifiers.New()
	if err := t.Struct(ctx, &config); err != nil {
		return Config{}, fmt.Errorf("failed to process config file: %w", err)
	}

	v := validator.New()
	if err := v.StructCtx(ctx, &config); err != nil {
		return Config{}, fmt.Errorf("failed to validate config file: %w", err)
	}

	return config, nil
}

func mapLogLevel(level string) (slog.Level, error) {
	switch level {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "INFO":
		return slog.LevelInfo, nil
	case "WARN":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %s", level)
	}
}

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
		config, err := buildConfig(ctx, configValue)
		if err != nil {
			kongCtx.FatalIfErrorf(fmt.Errorf("failed to build config: %w", err))
		}

		logMode, err := mapLogMode(config.Log.Mode)
		if err != nil {
			kongCtx.FatalIfErrorf(fmt.Errorf("failed to parse log mode: %w", err))
		}
		logLevel, err := mapLogLevel(config.Log.Level)
		if err != nil {
			kongCtx.FatalIfErrorf(fmt.Errorf("failed to parse log level: %w", err))
		}
		log := buildLogger(logMode, logLevel)
		slog.SetDefault(log)
		ctx = slogctx.NewCtx(ctx, log)
		ctx = slogctx.With(ctx, slog.String("version", Version))
		slogctx.Info(ctx, "application ready", slog.Any("config", config))

		var term *graterm.Terminator
		term, ctx = graterm.NewWithSignals(ctx, syscall.SIGINT, syscall.SIGTERM)

		if err := term.Wait(ctx, time.Minute); err != nil {
			slogctx.Error(ctx, "failed to gracefully terminate application", slog.Any("error", err))
			os.Exit(1)
		}
		slogctx.Info(ctx, "successfully terminated the application")
	}
}
