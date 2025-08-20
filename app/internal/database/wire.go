package database

import (
	"context"
	"fmt"

	"github.com/google/wire"
	"github.com/jovulic/zfsilo/app/internal/config"
	slogctx "github.com/veqryn/slog-context"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var WireSet = wire.NewSet(
	WireDatabase,
)

func WireDatabase(
	ctx context.Context,
	config config.Config,
) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(config.Database.DSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	slogctx.Info(ctx, "running database automigrate")
	if err := db.AutoMigrate(&Volume{}); err != nil {
		return nil, fmt.Errorf("failed to perform automigrate: %w", err)
	}

	return db, nil
}
