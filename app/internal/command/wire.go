package command

import (
	"fmt"

	"github.com/google/wire"
	"github.com/jovulic/zfsilo/app/internal/command/lib/host"
	"github.com/jovulic/zfsilo/app/internal/config"
	"github.com/jovulic/zfsilo/lib/command"
)

var WireSet = wire.NewSet(
	WireProduceTarget,
	WireConsumeTarget,
)

func buildExecutor(target config.ConfigCommandTarget) (command.Executor, error) {
	switch target.Type {
	case "LOCAL":
		return command.NewLocalExecutor(command.LocalExecutorConfig{
			RunAsRoot: target.RunAsRoot,
		}), nil
	case "REMOTE":
		return command.NewRemoteExecutor(command.RemoteExecutorConfig{
			RunAsRoot: target.RunAsRoot,
			Address:   target.Remote.Address,
			Port:      target.Remote.Port,
			Username:  target.Remote.Username,
			Password:  string(target.Remote.Password),
		}), nil
	default:
		return nil, fmt.Errorf("unknown command mode: %s", target.Type)
	}
}

type ProduceTarget struct {
	Executor command.Executor
	Host     *host.Host
	Password string
}

func WireProduceTarget(conf config.Config) (ProduceTarget, error) {
	executor, err := buildExecutor(conf.Command.ProduceTarget.ConfigCommandTarget)
	if err != nil {
		return ProduceTarget{}, fmt.Errorf("failed to build produce executor: %w", err)
	}

	h := conf.Command.ProduceTarget.Host
	if h.Hostname == "" {
		return ProduceTarget{}, fmt.Errorf("produce target hostname is not set")
	}

	return ProduceTarget{
		Executor: executor,
		Host:     host.New(h.Domain, h.OwnerTime, h.Hostname),
		Password: string(conf.Command.ProduceTarget.Password),
	}, nil
}

type ConsumeTarget struct {
	Executor command.Executor
	ID       string // Generic ID (IQN or NQN)
	Password string
}

type ConsumeTargetMap map[string]ConsumeTarget

func WireConsumeTarget(conf config.Config) (ConsumeTargetMap, error) {
	rets := make(ConsumeTargetMap)
	for i, target := range conf.Command.ConsumeTargets {
		executor, err := buildExecutor(target.ConfigCommandTarget)
		if err != nil {
			return nil, fmt.Errorf("failed to process consume target %d: %w", i, err)
		}
		rets[target.ClientID] = ConsumeTarget{
			Executor: executor,
			ID:       target.ClientID,
			Password: string(target.Password),
		}
	}
	return rets, nil
}
