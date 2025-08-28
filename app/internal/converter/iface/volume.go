package converteriface

import (
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/app/internal/database"
	"gorm.io/datatypes"
)

//go:generate goverter gen ./...

//goverter:converter
//goverter:output:file ../impl/volume.go
//goverter:output:package converterimpl
//goverter:extend ConvertFromJSONToStruct
//goverter:extend ConvertFromStructToJSON
//goverter:extend ConvertTimeToTimestamp
//goverter:extend ConvertTimestampToTime
type VolumeConverter interface {
	//goverter:ignore state sizeCache unknownFields
	//goverter:map ID Id
	//goverter:map DatasetID DatasetId
	//goverter:map InitiatorIQN InitiatorIqn
	//goverter:map TargetIQN TargetIqn
	//goverter:map Options | ConvertVolumeOptionsFromDBToAPI
	//goverter:map Mode | ConvertVolumeModeFromDBToAPI
	FromDBToAPI(source database.Volume) (*zfsilov1.Volume, error)
	FromDBToAPIList(source []database.Volume) ([]*zfsilov1.Volume, error)

	//goverter:useZeroValueOnPointerInconsistency
	//goverter:map Id ID
	//goverter:map DatasetId DatasetID
	//goverter:map InitiatorIqn InitiatorIQN
	//goverter:map TargetIqn TargetIQN
	//goverter:map Options | ConvertVolumeOptionsFromAPIToDB
	//goverter:map Mode | ConvertVolumeModeFromAPIToDB
	FromAPIToDB(source *zfsilov1.Volume) (database.Volume, error)
	FromAPIToDBList(source []*zfsilov1.Volume) ([]database.Volume, error)
}

func ConvertVolumeOptionsFromAPIToDB(source []*zfsilov1.Volume_Option) datatypes.JSONType[database.VolumeOptionList] {
	var destination database.VolumeOptionList
	for _, item := range source {
		destination = append(destination, database.VolumeOption{
			Key:   item.Key,
			Value: item.Value,
		})
	}
	return datatypes.NewJSONType(destination)
}

func ConvertVolumeOptionsFromDBToAPI(source datatypes.JSONType[database.VolumeOptionList]) []*zfsilov1.Volume_Option {
	var destination []*zfsilov1.Volume_Option
	for _, item := range source.Data() {
		destination = append(destination, &zfsilov1.Volume_Option{
			Key:   item.Key,
			Value: item.Value,
		})
	}
	return destination
}

func ConvertVolumeModeFromAPIToDB(source zfsilov1.Volume_Mode) database.VolumeMode {
	return database.VolumeMode(source)
}

func ConvertVolumeModeFromDBToAPI(source database.VolumeMode) zfsilov1.Volume_Mode {
	return zfsilov1.Volume_Mode(source)
}
