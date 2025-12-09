// Package mount contains lib/command wrappers for executing mount commands.
package mount

import (
	"context"
	"fmt"
	"strings"

	"github.com/jovulic/zfsilo/lib/command"
)

// Mount provides an interface for running mount and umount commands.
type Mount struct {
	executor command.Executor
}

// NewMount creates a new Mount instance.
func NewMount(executor command.Executor) *Mount {
	return &Mount{
		executor: executor,
	}
}

// MountArguments represents the arguments for a mount operation.
type MountArguments struct {
	SourcePath string
	TargetPath string
	Options    []string
}

// Mount executes the mount command.
func (m *Mount) Mount(ctx context.Context, args MountArguments) error {
	cmd := fmt.Sprintf(
		"mount -o '%s' '%s' '%s'",
		strings.Join(args.Options, ","),
		args.SourcePath,
		args.TargetPath,
	)
	result, err := m.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to mount '%s' to '%s': %w, stderr: %s", args.SourcePath, args.TargetPath, err, stderr)
	}
	return nil
}

// UmountArguments represents the arguments for an umount operation.
type UmountArguments struct {
	Path string
}

// Umount executes the umount command.
func (m *Mount) Umount(ctx context.Context, args UmountArguments) error {
	cmd := fmt.Sprintf("umount '%s'", args.Path)
	result, err := m.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to umount '%s': %w, stderr: %s", args.Path, err, stderr)
	}
	return nil
}

// IsMounted checks if a directory is a mount point.
// It uses `mountpoint -q`, which returns 0 if the path is a mountpoint, and a
// non-zero value otherwise.
func (m *Mount) IsMounted(ctx context.Context, path string) (bool, error) {
	cmd := fmt.Sprintf("mountpoint -q %s", path)
	result, err := m.executor.Exec(ctx, cmd)
	if err != nil {
		// A non-zero exit code means it's not a mountpoint.
		if result != nil && result.ExitCode != 0 {
			return false, nil
		}
		// Any other error (e.g., command not found, permissions) is a real error.
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return false, fmt.Errorf("failed to check mountpoint for '%s': %w, stderr: %s", path, err, stderr)
	}
	// Exit code 0 means it *is* a mountpoint.
	return true, nil
}

