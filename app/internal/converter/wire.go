package converter

import (
	"github.com/google/wire"
	converteriface "github.com/jovulic/zfsilo/app/internal/converter/iface"
	converterimpl "github.com/jovulic/zfsilo/app/internal/converter/impl"
)

var WireSet = wire.NewSet(
	WireVolumeConverter,
)

func WireVolumeConverter() converteriface.VolumeConverter {
	return &converterimpl.VolumeConverterImpl{}
}
