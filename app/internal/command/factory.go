package command

import (
	"fmt"

	"github.com/jovulic/zfsilo/app/internal/database"
	libcommand "github.com/jovulic/zfsilo/lib/command"
)

type ExecutorFactory struct{}

func NewExecutorFactory() *ExecutorFactory {
	return &ExecutorFactory{}
}

func (f *ExecutorFactory) BuildExecutor(host *database.Host) (libcommand.Executor, error) {
	conn := host.Connection.Data()
	switch conn.Type {
	case database.HostConnectionTypeLocal:
		if conn.Local == nil {
			return nil, fmt.Errorf("local configuration missing")
		}
		return libcommand.NewLocalExecutor(libcommand.LocalExecutorConfig{
			RunAsRoot: conn.Local.RunAsRoot,
		}), nil
	case database.HostConnectionTypeRemote:
		if conn.Remote == nil {
			return nil, fmt.Errorf("remote configuration missing")
		}
		return libcommand.NewRemoteExecutor(libcommand.RemoteExecutorConfig{
			RunAsRoot: conn.Remote.RunAsRoot,
			Address:   conn.Remote.Address,
			Port:      uint16(conn.Remote.Port),
			Username:  conn.Remote.Username,
			Password:  conn.Remote.Password,
		}), nil
	default:
		return nil, fmt.Errorf("unknown command mode: %s", conn.Type)
	}
}
