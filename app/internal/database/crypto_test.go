package database_test

import (
	"testing"

	"github.com/jovulic/zfsilo/app/internal/database"
	"github.com/stretchr/testify/assert"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEncryption(t *testing.T) {
	// Initialize in-memory database.
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	assert.NoError(t, err)

	// Automigrate.
	err = db.AutoMigrate(&database.Host{}, &database.Volume{})
	assert.NoError(t, err)

	// Set encryption key.
	key := "test-secret-key-that-is-long-enough"
	err = database.InitCrypto(db, key)
	assert.NoError(t, err)

	t.Run("Host encryption", func(t *testing.T) {
		plainPassword := "super-secret-password"
		host := &database.Host{
			ID:  "host-1",
			Key: plainPassword,
			Connection: datatypes.NewJSONType(database.HostConnection{
				Type: database.HostConnectionTypeRemote,
				Remote: &database.HostConnectionRemote{
					Password: plainPassword,
				},
			}),
		}

		// Create.
		err := db.Create(host).Error
		assert.NoError(t, err)

		// After Create, the model in memory should still have the plain password
		// because AfterSave hook decrypts it back.
		assert.Equal(t, plainPassword, host.Key)
		assert.Equal(t, plainPassword, host.Connection.Data().Remote.Password)

		// Read back.
		var retrieved database.Host
		err = db.First(&retrieved, "id = ?", "host-1").Error
		assert.NoError(t, err)
		assert.Equal(t, plainPassword, retrieved.Key)
		assert.Equal(t, plainPassword, retrieved.Connection.Data().Remote.Password)

		// Check raw database content to ensure it's encrypted.
		var rawKey string
		err = db.Raw("SELECT key FROM hosts WHERE id = ?", "host-1").Scan(&rawKey).Error
		assert.NoError(t, err)
		assert.NotEqual(t, plainPassword, rawKey)
		assert.NotEmpty(t, rawKey)

		var rawConnection string
		err = db.Raw("SELECT connection FROM hosts WHERE id = ?", "host-1").Scan(&rawConnection).Error
		assert.NoError(t, err)
		assert.NotContains(t, rawConnection, plainPassword)
	})

	t.Run("Volume encryption", func(t *testing.T) {
		plainPassword := "volume-secret"
		volume := &database.Volume{
			ID:            "vol-1",
			CapacityBytes: 1024 * 1024,
			Transport: datatypes.NewJSONType(database.VolumeTransport{
				Type: database.VolumeTransportTypeISCSI,
				ISCSI: &database.VolumeTransportISCSI{
					TargetPassword:    plainPassword,
					InitiatorPassword: plainPassword,
				},
			}),
		}

		// Create.
		err := db.Create(volume).Error
		assert.NoError(t, err)

		// After Create, model in memory should be plain.
		assert.Equal(t, plainPassword, volume.Transport.Data().ISCSI.TargetPassword)

		// Read back.
		var retrieved database.Volume
		err = db.First(&retrieved, "id = ?", "vol-1").Error
		assert.NoError(t, err)
		assert.Equal(t, plainPassword, retrieved.Transport.Data().ISCSI.TargetPassword)

		// Check raw database content.
		var rawTransport string
		err = db.Raw("SELECT transport FROM volumes WHERE id = ?", "vol-1").Scan(&rawTransport).Error
		assert.NoError(t, err)
		assert.NotContains(t, rawTransport, plainPassword)
	})
}
