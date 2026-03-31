package converteriface

import (
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/app/internal/database"
	"gorm.io/datatypes"
)

//goverter:converter
//goverter:output:file ../impl/host.go
//goverter:output:package converterimpl
//goverter:extend ConvertTimeToTimestamp
//goverter:extend ConvertTimestampToTime
//goverter:extend ConvertHostRoleFromDBToAPI
//goverter:extend ConvertHostRoleFromAPIToDB
type HostConverter interface {
	//goverter:ignore state sizeCache unknownFields
	//goverter:map ID Id
	//goverter:map Connection | ConvertHostConnectionFromDBToAPI
	//goverter:map Identifiers Ids
	FromDBToAPI(source *database.Host) (*zfsilov1.Host, error)
	FromDBToAPIList(source []*database.Host) ([]*zfsilov1.Host, error)

	//goverter:useZeroValueOnPointerInconsistency
	//goverter:map Id ID
	//goverter:map Connection | ConvertHostConnectionFromAPIToDB
	//goverter:map Ids Identifiers
	FromAPIToDB(source *zfsilov1.Host) (*database.Host, error)
	FromAPIToDBList(source []*zfsilov1.Host) ([]*database.Host, error)
}

func ConvertHostConnectionFromAPIToDB(source *zfsilov1.Host_Connection) datatypes.JSONType[database.HostConnection] {
	if source == nil {
		return datatypes.NewJSONType(database.HostConnection{})
	}
	dest := database.HostConnection{}
	if local := source.GetLocal(); local != nil {
		dest.Type = database.HostConnectionTypeLocal
		dest.Local.RunAsRoot = local.RunAsRoot
	} else if remote := source.GetRemote(); remote != nil {
		dest.Type = database.HostConnectionTypeRemote
		dest.Remote = &database.HostConnectionRemote{
			Address:   remote.Address,
			Port:      remote.Port,
			Username:  remote.Username,
			Password:  remote.Password,
			RunAsRoot: remote.RunAsRoot,
		}
	}
	return datatypes.NewJSONType(dest)
}

func ConvertHostConnectionFromDBToAPI(source datatypes.JSONType[database.HostConnection]) *zfsilov1.Host_Connection {
	data := source.Data()
	dest := &zfsilov1.Host_Connection{}
	switch data.Type {
	case database.HostConnectionTypeLocal:
		if data.Local != nil {
			dest.Type = &zfsilov1.Host_Connection_Local_{
				Local: &zfsilov1.Host_Connection_Local{
					RunAsRoot: data.Local.RunAsRoot,
				},
			}
		}
	case database.HostConnectionTypeRemote:
		if data.Remote != nil {
			dest.Type = &zfsilov1.Host_Connection_Remote_{
				Remote: &zfsilov1.Host_Connection_Remote{
					Address:   data.Remote.Address,
					Port:      data.Remote.Port,
					Username:  data.Remote.Username,
					Password:  data.Remote.Password,
					RunAsRoot: data.Remote.RunAsRoot,
				},
			}
		}
	}
	return dest
}

func ConvertHostRoleFromAPIToDB(source *zfsilov1.Host_Role) datatypes.JSONType[database.HostRole] {
	if source == nil {
		return datatypes.NewJSONType(database.HostRole{})
	}
	dest := database.HostRole{}
	if server := source.GetServer(); server != nil {
		dest.Type = database.HostRoleTypeServer
		dest.Server = &database.HostRoleServer{
			Endpoint: server.Endpoint,
		}
	} else if client := source.GetClient(); client != nil {
		dest.Type = database.HostRoleTypeClient
		dest.Client = &database.HostRoleClient{}
	}
	return datatypes.NewJSONType(dest)
}

func ConvertHostRoleFromDBToAPI(source datatypes.JSONType[database.HostRole]) *zfsilov1.Host_Role {
	data := source.Data()
	dest := &zfsilov1.Host_Role{}
	switch data.Type {
	case database.HostRoleTypeServer:
		if data.Server != nil {
			dest.Type = &zfsilov1.Host_Role_Server_{
				Server: &zfsilov1.Host_Role_Server{
					Endpoint: data.Server.Endpoint,
				},
			}
		}
	case database.HostRoleTypeClient:
		if data.Client != nil {
			dest.Type = &zfsilov1.Host_Role_Client_{
				Client: &zfsilov1.Host_Role_Client{},
			}
		}
	}
	return dest
}
