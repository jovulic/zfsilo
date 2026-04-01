package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jovulic/zfsilo/app/internal/config"
	"github.com/jovulic/zfsilo/app/internal/database"
	slogctx "github.com/veqryn/slog-context"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type App struct {
	server *http.Server
	db     *gorm.DB
	conf   config.Config
}

func NewApp(
	server *http.Server,
	db *gorm.DB,
	conf config.Config,
) *App {
	return &App{
		server: server,
		db:     db,
		conf:   conf,
	}
}

func (a *App) Sync(ctx context.Context) error {
	return SyncHosts(ctx, a.db, a.conf)
}

func SyncHosts(ctx context.Context, db *gorm.DB, conf config.Config) error {
	configHostIDs := make(map[string]struct{})

	for _, cfgHost := range conf.Hosts {
		id := cfgHost.ID
		configHostIDs[id] = struct{}{}

		role := database.HostRole{}
		switch cfgHost.Role {
		case "SERVER":
			role.Type = database.HostRoleTypeServer
			role.Server = &database.HostRoleServer{
				Endpoint: cfgHost.Endpoint,
			}
		case "CLIENT":
			role.Type = database.HostRoleTypeClient
			role.Client = &database.HostRoleClient{}
		}

		host := &database.Host{
			ID:          id,
			Name:        "hosts/" + id,
			Identifiers: datatypes.NewJSONSlice(cfgHost.IDs),
			Key:         string(cfgHost.Key),
			ByConfig:    true,
			Role:        datatypes.NewJSONType(role),
		}

		conn := database.HostConnection{
			Type: database.HostConnectionType(cfgHost.Connection.Type),
		}
		if cfgHost.Connection.Type == "REMOTE" {
			conn.Remote = &database.HostConnectionRemote{
				Address:   cfgHost.Connection.Remote.Address,
				Port:      int32(cfgHost.Connection.Remote.Port),
				Username:  cfgHost.Connection.Remote.Username,
				Password:  string(cfgHost.Connection.Remote.Password),
				RunAsRoot: cfgHost.Connection.Remote.RunAsRoot,
			}
		} else {
			conn.Local = &database.HostConnectionLocal{
				RunAsRoot: cfgHost.Connection.Local.RunAsRoot,
			}
		}
		host.Connection = datatypes.NewJSONType(conn)

		err := db.Transaction(func(tx *gorm.DB) error {
			var existing database.Host
			err := tx.Where("id = ?", id).First(&existing).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return tx.Create(host).Error
				}
				return err
			}
			return tx.Model(&existing).Updates(host).Error
		})
		if err != nil {
			return fmt.Errorf("failed to sync host %s: %w", id, err)
		}
	}

	// Delete hosts maintained by config that are no longer in config.
	var hostsToDelete []database.Host
	err := db.Where("by_config = ?", true).Find(&hostsToDelete).Error
	if err != nil {
		return fmt.Errorf("failed to list hosts for deletion: %w", err)
	}

	for _, host := range hostsToDelete {
		if _, ok := configHostIDs[host.ID]; !ok {
			// Check if referenced by volumes.
			var count int64
			db.Model(&database.Volume{}).Where("server_host = ? OR client_host = ?", host.Name, host.Name).Count(&count)
			if count > 0 {
				slogctx.Error(ctx, "host is missing from config but still referenced by volumes, skipping deletion", slog.String("hostId", host.ID))
				continue
			}

			slogctx.Info(ctx, "deleting host no longer in config", slog.String("hostId", host.ID))
			if err := db.Delete(&host).Error; err != nil {
				return fmt.Errorf("failed to delete host %s: %w", host.ID, err)
			}
		}
	}

	return nil
}
