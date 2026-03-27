package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type HostConnectionType string

const (
	HostConnectionTypeLocal  HostConnectionType = "LOCAL"
	HostConnectionTypeRemote HostConnectionType = "REMOTE"
)

type HostConnectionLocal struct {
	RunAsRoot bool `json:"runAsRoot"`
}

type HostConnectionRemote struct {
	Address   string `json:"address"`
	Port      int32  `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	RunAsRoot bool   `json:"runAsRoot"`
}

type HostConnection struct {
	Type   HostConnectionType    `json:"type"`
	Local  *HostConnectionLocal  `json:"local,omitempty"`
	Remote *HostConnectionRemote `json:"remote,omitempty"`
}

//go:generate stringer -type=HostRole -linecomment host.go
type HostRole int

const (
	HostRoleUNSPECIFIED HostRole = iota // UNSPECIFIED
	HostRoleSERVER                      // SERVER
	HostRoleCLIENT                      // CLIENT
)

type Host struct {
	CreateTime  time.Time `gorm:"autoCreateTime"`
	UpdateTime  time.Time `gorm:"autoUpdateTime"`
	ID          string    `gorm:"primaryKey"`
	Name        string
	Role        HostRole
	Connection  datatypes.JSONType[HostConnection]
	Identifiers datatypes.JSONSlice[string]
	Key         string
	ByConfig    bool
}

func (h *Host) BeforeSave(tx *gorm.DB) error {
	return h.process(true)
}

func (h *Host) BeforeUpdate(tx *gorm.DB) error {
	return h.process(true)
}

func (h *Host) AfterSave(tx *gorm.DB) error {
	return h.process(false)
}

func (h *Host) AfterUpdate(tx *gorm.DB) error {
	return h.process(false)
}

func (h *Host) AfterFind(tx *gorm.DB) error {
	return h.process(false)
}

func (h *Host) IQN() (string, error) {
	for _, id := range h.Identifiers {
		if strings.HasPrefix(id, "iqn.") {
			return id, nil
		}
	}
	return "", errors.New("no iqn defined")
}

func (h *Host) NQN() (string, error) {
	for _, id := range h.Identifiers {
		if strings.HasPrefix(id, "nqn.") {
			return id, nil
		}
	}
	return "", errors.New("no nqn defined")
}

func (h *Host) VolumeIQN(volumeID string) (string, error) {
	iqn, err := h.IQN()
	if err != nil {
		return "", err
	}
	value := fmt.Sprintf("%s:%s", iqn, volumeID)
	return h.sanitize(value), nil
}

func (h *Host) VolumeNQN(volumeID string) (string, error) {
	nqn, err := h.NQN()
	if err != nil {
		return "", err
	}
	value := fmt.Sprintf("%s:%s", nqn, volumeID)
	return h.sanitize(value), nil
}

func (h *Host) process(encrypt bool) error {
	if len(encryptionKey) == 0 {
		return nil
	}

	fn := decryptString
	if encrypt {
		fn = encryptString
	}

	var err error

	{
		if h.Key != "" {
			h.Key, err = fn(h.Key)
			if err != nil {
				return err
			}
		}
	}

	{
		conn := h.Connection.Data()
		modified := false
		if conn.Remote != nil && conn.Remote.Password != "" {
			conn.Remote.Password, err = fn(conn.Remote.Password)
			if err != nil {
				return err
			}
			modified = true
		}

		if modified {
			h.Connection = datatypes.NewJSONType(conn)
		}
	}

	return nil
}

func (h *Host) sanitize(val string) string {
	val = strings.ToLower(val)
	val = strings.ReplaceAll(val, "_", "-")
	return val
}
