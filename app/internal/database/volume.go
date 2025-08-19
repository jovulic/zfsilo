// Package database defines the database schemas and means of management.
package database

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
)

// VolumeOption stores a simple key-value pair.
type VolumeOption struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// VolumeOptionList is a slice of VolumeOption. It implements the Scanner and Valuer
// interfaces to be stored as JSON in the database.
type VolumeOptionList []VolumeOption

// Value implements the driver.Valuer interface, allowing VolumeOptionList to be
// saved as JSON.
func (vol *VolumeOptionList) Value() (driver.Value, error) {
	if vol == nil {
		return nil, nil
	}
	return json.Marshal(vol)
}

// Scan implements the sql.Scanner interface, allowing VolumeOptionList to be
// read from a JSON database value.
func (vol *VolumeOptionList) Scan(value any) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, vol)
}

//go:generate stringer -type=VolumeMode -linecomment
type VolumeMode int

const (
	VolumeModeUNSPECIFIED VolumeMode = iota // UNSPECIFIED
	VolumeModeBLOCK                         // BLOCK
	VolumeModeFILESYSTEM                    // FILESYSTEM
)

type Volume struct {
	Struct        datatypes.JSON
	CreateTime    time.Time `gorm:"autoCreateTime"`
	UpdateTime    time.Time `gorm:"autoUpdateTime"`
	ID            string    `gorm:"primaryKey"`
	Name          string
	DatasetID     string
	Options       datatypes.JSONType[VolumeOptionList]
	Sparse        bool
	Mode          VolumeMode
	CapacityBytes int64 `gorm:"check:capacity_bytes > 0"`
	InitiatorIQN  string
	TargetIQN     string
	TargetAddress string
	MountPath     string
}
