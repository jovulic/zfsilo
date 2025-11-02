package zfs

import (
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/lib/command"
)

var WireSet = wire.NewSet(
	WireZFS,
)

func WireZFS(executor command.Executor) *ZFS {
	return NewZFS(executor)
}
