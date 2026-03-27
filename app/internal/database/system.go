package database

const (
	SystemSettingEncryptionSalt = "encryption_salt"
)

// SystemSetting represents a generic key-value setting stored in the database.
type SystemSetting struct {
	ID    string `gorm:"primaryKey"`
	Value []byte
}
