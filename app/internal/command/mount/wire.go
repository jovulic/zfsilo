package mount

import (
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/lib/command"
)

var WireSet = wire.NewSet(
	WireMount,
)

func WireMount(executor command.Executor) *Mount {
	return NewMount(executor)
}
