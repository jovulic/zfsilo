// Package database defines the database schemas and means of management.
package database

import (
	"crypto/sha256"
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
	VolumeStatusSTAGED                          // STAGED
	VolumeStatusMOUNTED                         // MOUNTED
)

//go:generate stringer -type=VolumeTransport -linecomment volume.go
type VolumeTransport int

const (
	VolumeTransportUNSPECIFIED VolumeTransport = iota // UNSPECIFIED
	VolumeTransportISCSI                              // ISCSI
	VolumeTransportNVMEOF_TCP                         // NVMEOF_TCP
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
	Transport     VolumeTransport
	CapacityBytes int64 `gorm:"check:capacity_bytes > 0"`
	ClientID      string
	TargetID      string
	TargetAddress string
	StagingPath   string
	TargetPaths   datatypes.JSONSlice[string]
}

func (v *Volume) IsPublished() bool {
	return v.Status >= VolumeStatusPUBLISHED
}

func (v *Volume) IsConnected() bool {
	return v.Status >= VolumeStatusCONNECTED
}

func (v *Volume) IsStaged() bool {
	return v.Status >= VolumeStatusSTAGED
}

func (v *Volume) IsMounted() bool {
	return v.Status >= VolumeStatusMOUNTED
}

func (v *Volume) DevicePathClient() (string, error) {
	switch v.Transport {
	case VolumeTransportISCSI:
		return BuildDevicePathISCSIClient(v.TargetAddress, v.TargetID), nil
	case VolumeTransportNVMEOF_TCP:
		return BuildDevicePathNVMeOFClient(v.ID), nil
	case VolumeTransportUNSPECIFIED:
		fallthrough
	default:
		return "", fmt.Errorf("unsupported transport: %s", v.Transport)
	}
}

func (v *Volume) DevicePathServer() (string, error) {
	switch v.Transport {
	case VolumeTransportISCSI:
		return BuildDevicePathISCSIServer(v.TargetID), nil
	case VolumeTransportNVMEOF_TCP:
		return BuildDevicePathNVMeOFServer(v.TargetID), nil
	case VolumeTransportUNSPECIFIED:
		fallthrough
	default:
		return "", fmt.Errorf("unsupported transport: %s", v.Transport)
	}
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

func BuildDevicePathNVMeOFClient(volumeID string) string {
	// NVMe serial numbers are limited to 20 characters. We use a truncated
	// SHA-256 hash of the VolumeID to stay within the limit, matching the logic
	// in the nvmeof command package.
	hash := sha256.Sum256([]byte(volumeID))
	serial := fmt.Sprintf("%x", hash)[:20]

	// NVMe-oF devices appear in /dev/disk/by-id/nvme-Linux_<serial>_1.
	return fmt.Sprintf("/dev/disk/by-id/nvme-Linux_%s_1", serial)
}

func BuildDevicePathNVMeOFServer(nqn string) string {
	return fmt.Sprintf("/sys/kernel/config/nvmet/subsystems/%s", nqn)
}

func BuildDevicePathZFS(datasetID string) string {
	return fmt.Sprintf("/dev/zvol/%s", datasetID)
}
