// Package fs contains lib/command wrappers for executing and working with the
// filesystem.
package fs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jovulic/zfsilo/lib/command"
)

// FS provides an interface for running mkfs commands.
type FS struct {
	executor command.Executor
}

// With creates a new Mkfs instance.
func With(executor command.Executor) FS {
	return FS{
		executor: executor,
	}
}

// WaitForDeviceArguments represents the arguments for waiting for a device.
type WaitForDeviceArguments struct {
	Device       string
	Timeout      time.Duration
	PollInterval time.Duration
}

// WaitForDevice waits for a block device matching the pattern to exist,
// polling until a timeout is reached. It returns the absolute path of the
// first discovered device.
func (m FS) WaitForDevice(ctx context.Context, args WaitForDeviceArguments) (string, error) {
	timeout := args.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second // default timeout
	}

	pollInterval := args.PollInterval
	if pollInterval == 0 {
		pollInterval = 500 * time.Millisecond // default interval
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out waiting for device %s to exist: %w", args.Device, ctx.Err())
		default:
			// Use ls to find the device file.
			result, err := m.executor.Exec(ctx, fmt.Sprintf("ls -1 %s 2>/dev/null | head -n 1", args.Device))
			if err == nil {
				path := strings.TrimSpace(result.Stdout)
				if path != "" {
					return path, nil // device found
				}
			}
			time.Sleep(pollInterval)
		}
	}
}

// ResolveDevice finds the exact path of a device given a shell glob pattern.
func (m FS) ResolveDevice(ctx context.Context, pattern string) (string, error) {
	result, err := m.executor.Exec(ctx, fmt.Sprintf("ls -1 %s 2>/dev/null | head -n 1", pattern))
	if err != nil {
		return "", fmt.Errorf("failed to list device: %w", err)
	}
	path := strings.TrimSpace(result.Stdout)
	if path == "" {
		return "", fmt.Errorf("device not found matching %s", pattern)
	}
	return path, nil
}

// FormatArguments represents the arguments for formatting a device.
type FormatArguments struct {
	Device        string
	WaitForDevice bool
}

// Format executes mkfs.ext4 to format a device.
// The -F option forces overwrite of any existing filesystem.
// The -m 0 option reserves 0% of the blocks for the super-user.
func (m FS) Format(ctx context.Context, args FormatArguments) error {
	device := args.Device
	if args.WaitForDevice {
		resolved, err := m.WaitForDevice(ctx, WaitForDeviceArguments{Device: args.Device})
		if err != nil {
			return fmt.Errorf("error while waiting for device %s: %w", args.Device, err)
		}
		device = resolved
	}

	cmd := fmt.Sprintf("mkfs.ext4 -F -m0 '%s'", device)
	result, err := m.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to format device '%s': %w, stderr: %s", device, err, stderr)
	}
	return nil
}

// GetFSType returns the filesystem type of a device.
// It uses `blkid -o value -s TYPE`.
func (m FS) GetFSType(ctx context.Context, device string) (string, error) {
	cmd := fmt.Sprintf("blkid -o value -s TYPE '%s'", device)
	result, err := m.executor.Exec(ctx, cmd)
	if err != nil {
		// blkid returns 2 if no filesystem is found.
		if result != nil && result.ExitCode == 2 {
			return "", nil
		}
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return "", fmt.Errorf("failed to get filesystem type for device '%s': %w, stderr: %s", device, err, stderr)
	}
	return result.Stdout, nil
}

// ClearArguments represents the arguments for clearing filesystem signatures from a device.
type ClearArguments struct {
	Device string
}

// Clear removes all known filesystem, RAID or partition table signatures from a device.
// The -a option removes all signatures.
func (m FS) Clear(ctx context.Context, args ClearArguments) error {
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

// ResizeArguments represents the arguments for resizing a filesystem.
type ResizeArguments struct {
	Device string
}

// Resize executes resize2fs to resize a filesystem on a device.
func (m FS) Resize(ctx context.Context, args ResizeArguments) error {
	cmd := fmt.Sprintf("resize2fs '%s'", args.Device)
	result, err := m.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to resize filesystem on device '%s': %w, stderr: %s", args.Device, err, stderr)
	}
	return nil
}
