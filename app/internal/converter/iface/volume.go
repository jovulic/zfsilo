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
	//goverter:map ClientID ClientId
	//goverter:map TargetID TargetId
	//goverter:map Options | ConvertVolumeOptionsFromDBToAPI
	//goverter:map Mode | ConvertVolumeModeFromDBToAPI
	//goverter:map Status | ConvertVolumeStatusFromDBToAPI
	//goverter:map Transport | ConvertVolumeTransportFromDBToAPI
	FromDBToAPI(source *database.Volume) (*zfsilov1.Volume, error)
	FromDBToAPIList(source []*database.Volume) ([]*zfsilov1.Volume, error)

	//goverter:useZeroValueOnPointerInconsistency
	//goverter:map Id ID
	//goverter:map DatasetId DatasetID
	//goverter:map ClientId ClientID
	//goverter:map TargetId TargetID
	//goverter:map Options | ConvertVolumeOptionsFromAPIToDB
	//goverter:map Mode | ConvertVolumeModeFromAPIToDB
	//goverter:map Status | ConvertVolumeStatusFromAPIToDB
	//goverter:map Transport | ConvertVolumeTransportFromAPIToDB
	FromAPIToDB(source *zfsilov1.Volume) (*database.Volume, error)
	FromAPIToDBList(source []*zfsilov1.Volume) ([]*database.Volume, error)
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

func ConvertVolumeStatusFromAPIToDB(source zfsilov1.Volume_Status) database.VolumeStatus {
	return database.VolumeStatus(source)
}

func ConvertVolumeStatusFromDBToAPI(source database.VolumeStatus) zfsilov1.Volume_Status {
	return zfsilov1.Volume_Status(source)
}

func ConvertVolumeTransportFromAPIToDB(source *zfsilov1.Volume_Transport) database.VolumeTransport {
	if source == nil {
		return database.VolumeTransportUNSPECIFIED
	}
	return database.VolumeTransport(*source)
}

func ConvertVolumeTransportFromDBToAPI(source database.VolumeTransport) *zfsilov1.Volume_Transport {
	transport := zfsilov1.Volume_Transport(source)
	return &transport
}
