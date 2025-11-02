package command

import (
	"fmt"

	"github.com/google/wire"
	"github.com/jovulic/zfsilo/app/internal/config"
	"github.com/jovulic/zfsilo/lib/command"
)

var WireSet = wire.NewSet(
	WireExecutor,
)

func WireExecutor(conf config.Config) (command.Executor, error) {
	switch conf.Command.Mode {
	case "local":
		return command.NewLocalExector(command.LocalExecutorConfig{
			RunAsRoot: conf.Command.RunAsRoot,
		}), nil
	case "remote":
		return command.NewRemoteExecutor(command.RemoteExecutorConfig{
			RunAsRoot: conf.Command.RunAsRoot,
			Address:   conf.Command.Remote.Address,
			Port:      conf.Command.Remote.Port,
			Username:  conf.Command.Remote.Username,
			Password:  conf.Command.Remote.Password,
		}), nil
	default:
		return nil, fmt.Errorf("unknown command mode: %s", conf.Command.Mode)
	}
}
