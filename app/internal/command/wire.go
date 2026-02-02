package command

import (
	"fmt"

	"github.com/google/wire"
	"github.com/jovulic/zfsilo/app/internal/config"
	"github.com/jovulic/zfsilo/lib/command"
)

var WireSet = wire.NewSet(
	WireProduceTarget,
	WireConsumeTarget,
)

func buildExecutor(target config.ConfigCommandTarget) (command.Executor, error) {
	switch target.Mode {
	case "local":
		return command.NewLocalExecutor(command.LocalExecutorConfig{
			RunAsRoot: target.RunAsRoot,
		}), nil
	case "remote":
		return command.NewRemoteExecutor(command.RemoteExecutorConfig{
			RunAsRoot: target.RunAsRoot,
			Address:   target.Remote.Address,
			Port:      target.Remote.Port,
			Username:  target.Remote.Username,
			Password:  target.Remote.Password,
		}), nil
	default:
		return nil, fmt.Errorf("unknown command mode: %s", target.Mode)
	}
}

type ProduceExecutor command.Executor

func WireProduceTarget(conf config.Config) (ProduceExecutor, error) {
	return buildExecutor(conf.Command.ProduceTarget.ConfigCommandTarget)
}

type ConsumeExecutorMap map[string]command.Executor

func WireConsumeTarget(conf config.Config) (ConsumeExecutorMap, error) {
	rets := make(map[string]command.Executor)
	for i, target := range conf.Command.ConsumeTargets {
		ret, err := buildExecutor(target.ConfigCommandTarget)
		if err != nil {
			return nil, fmt.Errorf("failed to process consume target %d: %w", i, err)
		}
		rets[target.IQN] = ret
	}
	return rets, nil
}
