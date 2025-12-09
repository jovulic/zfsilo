// Package mkfs contains lib/command wrappers for executing and working with mkfs.
package mkfs

import (
	"context"
	"fmt"
	"time"

	"github.com/jovulic/zfsilo/lib/command"
)

// Mkfs provides an interface for running mkfs commands.
type Mkfs struct {
	executor command.Executor
}

// NewMkfs creates a new Mkfs instance.
func NewMkfs(executor command.Executor) *Mkfs {
	return &Mkfs{
		executor: executor,
	}
}

// ExistsArguments represents the arguments for checking if a device exists.
type ExistsArguments struct {
	Device       string
	Timeout      time.Duration
	PollInterval time.Duration
}

// Exists checks if a block device exists, polling until a timeout is reached.
func (m *Mkfs) Exists(ctx context.Context, args ExistsArguments) (bool, error) {
	timeout := args.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second // Default timeout
	}

	pollInterval := args.PollInterval
	if pollInterval == 0 {
		pollInterval = 500 * time.Millisecond // Default interval
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("timed out waiting for device %s to exist: %w", args.Device, ctx.Err())
		default:
			// Use stat to check for the device file.
			_, err := m.executor.Exec(ctx, fmt.Sprintf("stat %s", args.Device))
			if err == nil {
				return true, nil // Device found
			}
			time.Sleep(pollInterval)
		}
	}
}

// FormatArguments represents the arguments for formatting a device.
type FormatArguments struct {
	Device        string
	WaitForDevice bool
}

// Format executes mkfs.ext4 to format a device.
// The -F option forces overwrite of any existing filesystem.
// The -m 0 option reserves 0% of the blocks for the super-user.
func (m *Mkfs) Format(ctx context.Context, args FormatArguments) error {
	if args.WaitForDevice {
		exists, err := m.Exists(ctx, ExistsArguments{Device: args.Device})
		if err != nil {
			return fmt.Errorf("error while waiting for device %s: %w", args.Device, err)
		}
		if !exists {
			// This path should not be reached if Exists returns an error on timeout.
			return fmt.Errorf("device %s not found after waiting", args.Device)
		}
	}

	cmd := fmt.Sprintf("mkfs.ext4 -F -m0 '%s'", args.Device)
	result, err := m.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to format device '%s': %w, stderr: %s", args.Device, err, stderr)
	}
	return nil
}

// ClearArguments represents the arguments for clearing filesystem signatures from a device.
type ClearArguments struct {
	Device string
}

// Clear removes all known filesystem, RAID or partition table signatures from a device.
// The -a option removes all signatures.
func (m *Mkfs) Clear(ctx context.Context, args ClearArguments) error {
	cmd := fmt.Sprintf("wipefs -a %s", args.Device)
	result, err := m.executor.Exec(ctx, cmd)
	if err != nil {
		// wipefs may return a non-zero exit code (1) if no signatures were found,
		// which is not an error for us. Only treat other non-zero exit codes as errors.
		if result != nil && result.ExitCode == 1 {
			return nil // No signatures found, consider it successful for clearing purpose.
		}

		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to clear device '%s': %w, stderr: %s", args.Device, err, stderr)
	}
	return nil
}
