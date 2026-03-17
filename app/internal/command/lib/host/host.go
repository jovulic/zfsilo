// Package host contains utility types to ease creating IQN or NQN names off
// host configuration.
package host

import (
	"fmt"
	"strings"
	"time"
)

type Host struct {
	domain    string
	ownerTime time.Time
	hostname  string
}

func New(domain string, ownerTime time.Time, hostname string) *Host {
	return &Host{
		domain:    domain,
		ownerTime: ownerTime,
		hostname:  hostname,
	}
}

func (h *Host) IQN() string {
	value := fmt.Sprintf(
		"iqn.%s.%s.%s",
		h.ownerTime.Format("2006-01"),
		h.reverseDomain(),
		h.hostname,
	)
	return h.sanitize(value)
}

func (h *Host) VolumeIQN(volumeID string) string {
	value := fmt.Sprintf("%s:%s", h.IQN(), volumeID)
	return h.sanitize(value)
}

func (h *Host) NQN() string {
	value := fmt.Sprintf(
		"nqn.%s.%s:%s",
		h.ownerTime.Format("2006-01"),
		h.reverseDomain(),
		h.hostname,
	)
	return h.sanitize(value)
}

func (h *Host) VolumeNQN(volumeID string) string {
	value := fmt.Sprintf("%s:%s", h.NQN(), volumeID)
	return h.sanitize(value)
}

func (h *Host) reverseDomain() string {
	parts := strings.Split(h.domain, ".")
	if len(parts) == 1 {
		parts = append(parts, "local")
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return strings.Join(parts, ".")
}

func (h *Host) sanitize(val string) string {
	val = strings.ToLower(val)
	val = strings.ReplaceAll(val, "_", "-")
	return val
}
