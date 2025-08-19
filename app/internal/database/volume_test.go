package database_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/jovulic/zfsilo/app/internal/database"
)

// setupTestDB initializes an in-memory SQLite database for testing.
func setupTestDB(t *testing.T) *gorm.DB {
	// Using "file::memory:?cache=shared" creates an in-memory database
	// that persists as long as one connection is open.
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to in-memory database: %v", err)
	}

	// Automigrate the schema.
	err = db.AutoMigrate(&database.Volume{})
	if err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	return db
}

// TestVolumeCRUD performs a full Create, Read, Update, Delete cycle.
func TestVolumeCRUD(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)

	// CREATE
	t.Run("Create", func(t *testing.T) {
		type CustomData struct {
			Owner string `json:"owner"`
		}
		customStruct := CustomData{Owner: "Test Suite"}
		structJSON, _ := json.Marshal(customStruct)

		newVolume := &database.Volume{
			ID:            "vol-test-123",
			Name:          "TestVolume",
			DatasetID:     "dset-test-456",
			Mode:          database.VolumeModeBLOCK,
			CapacityBytes: 512 * 1024 * 1024, // 512 MiB
			Options: datatypes.NewJSONType(database.VolumeOptionList{
				{Key: "snapshot", Value: "false"},
			}),
			Struct: structJSON,
		}

		err := gorm.G[database.Volume](db).Create(ctx, newVolume)
		assert.NoError(t, err, "Failed to create volume")

		// Verify it was created.
		var retrievedVolume database.Volume
		err = db.First(&retrievedVolume, "id = ?", "vol-test-123").Error
		assert.NoError(t, err)
		assert.Equal(t, "TestVolume", retrievedVolume.Name)
		assert.NotNil(t, retrievedVolume.CreateTime)
	})

	// READ
	t.Run("Read", func(t *testing.T) {
		retrievedVolume, err := gorm.G[database.Volume](db).Where("id = ?", "vol-test-123").First(ctx)
		assert.NoError(t, err)
		assert.Equal(t, "vol-test-123", retrievedVolume.ID)
		assert.Equal(t, database.VolumeModeBLOCK, retrievedVolume.Mode)
	})

	// UPDATE
	t.Run("Update", func(t *testing.T) {
		_, err := gorm.G[database.Volume](db).Where("id = ?", "vol-test-123").Update(ctx, "Name", "UpdatedTestVolume")
		assert.NoError(t, err)

		// Verify the update.
		updatedVolume, err := gorm.G[database.Volume](db).Where("id = ?", "vol-test-123").First(ctx)
		assert.NoError(t, err)
		assert.Equal(t, "UpdatedTestVolume", updatedVolume.Name)
		assert.NotNil(t, updatedVolume.UpdateTime)
	})

	// DELETE
	t.Run("Delete", func(t *testing.T) {
		_, err := gorm.G[database.Volume](db).Where("id = ?", "vol-test-123").Delete(ctx)
		assert.NoError(t, err)

		// Verify it's gone.
		var result database.Volume
		err = db.First(&result, "id = ?", "vol-test-123").Error
		assert.Error(t, err, "Expected an error when finding deleted record")
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})
}
