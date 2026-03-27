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
	"gorm.io/gorm"
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

type VolumeTransportType string

const (
	VolumeTransportTypeUNSPECIFIED VolumeTransportType = "UNSPECIFIED"
	VolumeTransportTypeISCSI       VolumeTransportType = "ISCSI"
	VolumeTransportTypeNVMEOF_TCP  VolumeTransportType = "NVMEOF_TCP"
)

type VolumeTransportISCSI struct {
	TargetAddress     string `json:"targetAddress,omitempty"`
	TargetIQN         string `json:"targetIQN,omitempty"`
	TargetPassword    string `json:"targetPassword,omitempty"`
	InitiatorIQN      string `json:"initiatorIQN,omitempty"`
	InitiatorPassword string `json:"initiatorPassword,omitempty"`
}

type VolumeTransportNVMEOF struct {
	TargetAddress     string `json:"targetAddress,omitempty"`
	TargetNQN         string `json:"targetNQN,omitempty"`
	TargetPassword    string `json:"targetPassword,omitempty"`
	InitiatorNQN      string `json:"initiatorNQN,omitempty"`
	InitiatorPassword string `json:"initiatorPassword,omitempty"`
}

type VolumeTransport struct {
	Type   VolumeTransportType    `json:"type"`
	ISCSI  *VolumeTransportISCSI  `json:"iscsi,omitempty"`
	NVMEOF *VolumeTransportNVMEOF `json:"nvmeof,omitempty"`
}

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
	Status        VolumeStatus
	ServerHost    string
	ClientHost    string
	Transport     datatypes.JSONType[VolumeTransport]
	StagingPath   string
	TargetPaths   datatypes.JSONSlice[string]
}

func (v *Volume) BeforeSave(tx *gorm.DB) error {
	return v.process(true)
}

func (v *Volume) BeforeUpdate(tx *gorm.DB) error {
	return v.process(true)
}

func (v *Volume) AfterSave(tx *gorm.DB) error {
	return v.process(false)
}

func (v *Volume) AfterUpdate(tx *gorm.DB) error {
	return v.process(false)
}

func (v *Volume) AfterFind(tx *gorm.DB) error {
	return v.process(false)
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

func (v *Volume) DevicePathClient(targetAddress string, targetID string) (string, error) {
	switch v.Transport.Data().Type {
	case VolumeTransportTypeISCSI:
		return BuildDevicePathISCSIClient(targetAddress, targetID), nil
	case VolumeTransportTypeNVMEOF_TCP:
		return BuildDevicePathNVMeOFClient(v.ID), nil
	case VolumeTransportTypeUNSPECIFIED:
		fallthrough
	default:
		return "", fmt.Errorf("unsupported transport: %s", v.Transport.Data().Type)
	}
}

func (v *Volume) DevicePathServer(targetID string) (string, error) {
	switch v.Transport.Data().Type {
	case VolumeTransportTypeISCSI:
		return BuildDevicePathISCSIServer(targetID), nil
	case VolumeTransportTypeNVMEOF_TCP:
		return BuildDevicePathNVMeOFServer(targetID), nil
	case VolumeTransportTypeUNSPECIFIED:
		fallthrough
	default:
		return "", fmt.Errorf("unsupported transport: %s", v.Transport.Data().Type)
	}
}

func (v *Volume) DevicePathZFS() string {
	return BuildDevicePathZFS(v.DatasetID)
}

func (v *Volume) process(encrypt bool) error {
	if len(encryptionKey) == 0 {
		return nil
	}

	fn := decryptString
	if encrypt {
		fn = encryptString
	}

	{
		transport := v.Transport.Data()
		modified := false
		if transport.ISCSI != nil {
			if transport.ISCSI.TargetPassword != "" {
				var err error
				transport.ISCSI.TargetPassword, err = fn(transport.ISCSI.TargetPassword)
				if err != nil {
					return err
				}
				modified = true
			}
			if transport.ISCSI.InitiatorPassword != "" {
				var err error
				transport.ISCSI.InitiatorPassword, err = fn(transport.ISCSI.InitiatorPassword)
				if err != nil {
					return err
				}
				modified = true
			}
		}
		if transport.NVMEOF != nil {
			if transport.NVMEOF.TargetPassword != "" {
				var err error
				transport.NVMEOF.TargetPassword, err = fn(transport.NVMEOF.TargetPassword)
				if err != nil {
					return err
				}
				modified = true
			}
			if transport.NVMEOF.InitiatorPassword != "" {
				var err error
				transport.NVMEOF.InitiatorPassword, err = fn(transport.NVMEOF.InitiatorPassword)
				if err != nil {
					return err
				}
				modified = true
			}
		}

		if modified {
			v.Transport = datatypes.NewJSONType(transport)
		}
	}

	return nil
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
