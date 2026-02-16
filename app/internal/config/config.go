// Package config defines the application configuration.
//
// The configuration is referenced by the various internal packages. In
// particular it is used with `wire` to feed configuration to the various
// packages.
package config

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/go-playground/mold/v4/modifiers"
	"github.com/go-playground/validator/v10"
)

type LogLevel string

func (ll LogLevel) SlogLevel() (slog.Level, error) {
	switch ll {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "INFO":
		return slog.LevelInfo, nil
	case "WARN":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %s", ll)
	}
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

type ConfigCommandTarget struct {
	Type      string `json:"type"      mod:"default=LOCAL" validate:"oneof=LOCAL REMOTE"`
	RunAsRoot bool   `json:"runAsRoot"`
	Remote    struct {
		Address  string `json:"address"  validate:"required_if=Mode remote"`
		Port     uint16 `json:"port"     mod:"default=22"                   validate:"required_if=Mode REMOTE"`
		Username string `json:"username" validate:"required_if=Mode remote"`
		Password string `json:"password" validate:"required_if=Mode remote"`
	} `json:"remote"`
}

type ConfigCommandTargetProduce struct {
	ConfigCommandTarget
}

type ConfigCommandTargetConsume struct {
	ConfigCommandTargetProduce

	IQN string `json:"iqn"`
}

type ConfigCommandHost struct {
	// NOTE: The linux-iscsi.org domain establish time of 01 2006.
	Domain    string    `json:"domain"    mod:"default=linux-iscsi.org"`
	OwnerTime time.Time `json:"ownerTime" mod:"default=2006-01-01T00:00:00Z"`
	Hostname  string    `json:"hostname"  validate:"required"`
}

type Config struct {
	Log struct {
		Level  LogLevel `json:"level"  mod:"default=INFO" validate:"oneof=DEBUG INFO WARN ERROR"`
		Format string   `json:"format" mod:"default=JSON" validate:"oneof=JSON TEXT"`
	} `json:"log"`
	Service struct {
		BindAddress       string `json:"bindAddress"       mod:"default=:8080"`
		ExternalServerURI string `json:"externalServerURI" validate:"required"`
		Keys              []struct {
			Identity string `json:"identity"`
			Token    string `json:"token"`
		} `json:"keys"`
	} `json:"service"`
	Database struct {
		DSN string `json:"dsn" validate:"required"`
	} `json:"database"`
	Command struct {
		ProduceTarget  ConfigCommandTargetProduce   `json:"produceTarget"`
		ConsumeTargets []ConfigCommandTargetConsume `json:"consumeTargets"`
		Host           ConfigCommandHost            `json:"host"`
		Credentials    struct {
			UserID         string `json:"userId"`
			Password       string `json:"password"`
			MutualUserID   string `json:"mutualUserId"`
			MutualPassword string `json:"mutualPassword"`
		} `json:"credentials"`
	} `json:"command"`
}

func BuildConfig(ctx context.Context, configValue string) (Config, error) {
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
