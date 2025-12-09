package mkfs

import (
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/lib/command"
)

var WireSet = wire.NewSet(
	WireMkfs,
)

func WireMkfs(executor command.Executor) *Mkfs {
	return NewMkfs(executor)
}
