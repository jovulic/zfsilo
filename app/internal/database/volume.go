// Package database defines the database schemas and means of management.
package database

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
)

type VolumeOption struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type VolumeOptionList []VolumeOption

// Value implements the driver.Valuer interface.
func (vol *VolumeOptionList) Value() (driver.Value, error) {
	if vol == nil {
		return nil, nil
	}
	return json.Marshal(vol)
}

// Scan implements the sql.Scanner interface.
func (vol *VolumeOptionList) Scan(value any) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, vol)
}

//go:generate stringer -type=VolumeMode -linecomment volume.go
type VolumeMode int

const (
	VolumeModeUNSPECIFIED VolumeMode = iota // UNSPECIFIED
	VolumeModeBLOCK                         // BLOCK
	VolumeModeFILESYSTEM                    // FILESYSTEM
)

//go:generate stringer -type=VolumeStatus -linecomment volume.go
type VolumeStatus int

const (
	VolumeStatusUNSPECIFIED VolumeStatus = iota // UNSPECIFIED
	VolumeStatusINITIAL                         // INITIAL
	VolumeStatusPUBLISHED                       // PUBLISHED
	VolumeStatusCONNECTED                       // CONNECTED
	VolumeStatusMOUNTED                         // MOUNTED
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
	Status        VolumeStatus
	CapacityBytes int64 `gorm:"check:capacity_bytes > 0"`
	InitiatorIQN  string
	TargetIQN     string
	TargetAddress string
	MountPath     string
}

func (v *Volume) IsPublished() bool {
	return v.Status >= VolumeStatusPUBLISHED
}

func (v *Volume) IsConnected() bool {
	return v.Status >= VolumeStatusCONNECTED
}

func (v *Volume) IsMounted() bool {
	return v.Status >= VolumeStatusMOUNTED
}

func (v *Volume) DevicePathISCSIClient() string {
	return BuildDevicePathISCSIClient(v.TargetAddress, v.TargetIQN)
}

func (v *Volume) DevicePathISCSIServer() string {
	return BuildDevicePathISCSIServer(v.TargetIQN)
}

func (v *Volume) DevicePathZFS() string {
	return BuildDevicePathZFS(v.DatasetID)
}

func BuildDevicePathISCSIClient(address string, iqn string) string {
	return fmt.Sprintf("/dev/disk/by-path/ip-%s-iscsi-%s-lun-%d", address, iqn, 0)
}

func BuildDevicePathISCSIServer(iqn string) string {
	return fmt.Sprintf("/sys/kernel/config/target/iscsi/%s", iqn)
}

func BuildDevicePathZFS(datasetID string) string {
	return fmt.Sprintf("/dev/zvol/%s", datasetID)
}
