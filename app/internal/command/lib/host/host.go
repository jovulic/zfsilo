// Package host contains utility types to ease creating IQN or NQN names off
// host configuration.
package host

import (
	"errors"
	"fmt"
	"strings"
)

type Host struct {
	ids []string
}

func New(ids []string) *Host {
	return &Host{
		ids: ids,
	}
}

func (h *Host) IQN() (string, error) {
	for _, id := range h.ids {
		if strings.HasPrefix(id, "iqn.") {
			return id, nil
		}
	}
	return "", errors.New("no iqn defined")
}

func (h *Host) NQN() (string, error) {
	for _, id := range h.ids {
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

func (h *Host) sanitize(val string) string {
	val = strings.ToLower(val)
	val = strings.ReplaceAll(val, "_", "-")
	return val
}
