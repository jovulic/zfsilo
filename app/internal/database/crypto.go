package database

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/jovulic/zfsilo/lib/cryptoutil"
	"gorm.io/gorm"
)

var encryptionKey []byte

func InitCrypto(db *gorm.DB, secret string) error {
	if secret == "" {
		return nil
	}

	if err := db.AutoMigrate(&SystemSetting{}); err != nil {
		return fmt.Errorf("failed to migrate system settings: %w", err)
	}

	var saltSetting SystemSetting
	err := db.First(&saltSetting, "id = ?", SystemSettingEncryptionSalt).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Generate a new 16-byte salt.
			salt := make([]byte, 16)
			if _, err := io.ReadFull(rand.Reader, salt); err != nil {
				return fmt.Errorf("failed to generate encryption salt: %w", err)
			}

			saltSetting = SystemSetting{
				ID:    SystemSettingEncryptionSalt,
				Value: salt,
			}

			if err := db.Create(&saltSetting).Error; err != nil {
				return fmt.Errorf("failed to save encryption salt: %w", err)
			}
		} else {
			return fmt.Errorf("failed to fetch encryption salt: %w", err)
		}
	}

	encryptionKey = cryptoutil.NewKey([]byte(secret), saltSetting.Value)
	return nil
}

func encryptString(val string) (string, error) {
	if len(encryptionKey) == 0 {
		return val, nil
	}

	pt := []byte(val)
	ctBytes, err := cryptoutil.Encrypt(pt, encryptionKey)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(ctBytes), nil
}

func decryptString(val string) (string, error) {
	if len(encryptionKey) == 0 {
		return val, nil
	}

	ctBytes, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		return "", err
	}

	ptBytes, err := cryptoutil.Decrypt(ctBytes, encryptionKey)
	if err != nil {
		return "", err
	}

	return string(ptBytes), nil
}
