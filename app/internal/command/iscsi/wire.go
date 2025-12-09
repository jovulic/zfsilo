package iscsi

import (
	"github.com/google/wire"
	"github.com/jovulic/zfsilo/lib/command"
)

var WireSet = wire.NewSet(
	WireISCSI,
)

func WireISCSI(executor command.Executor) *ISCSI {
	return NewISCSI(executor)
}
