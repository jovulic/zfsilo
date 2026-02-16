// Package zfs contains lib/command wrappers for executing and working with ZFS.
package zfs

import (
	"context"
	"fmt"
	"strings"

	"github.com/jovulic/zfsilo/lib/command"
)

// ZFS provides an interface for interacting with ZFS.
type ZFS struct {
	executor command.Executor
}

// With creates a new ZFS instance.
func With(executor command.Executor) ZFS {
	return ZFS{
		executor: executor,
	}
}

// CreateVolumeArguments represents the arguments for creating a ZFS volume.
type CreateVolumeArguments struct {
	Name    string
	Size    uint64
	Options map[string]string
	Sparse  bool
}

// CreateVolume creates a new ZFS volume.
//
// zfs create [-p] [-o property=value]... -V <size> <volume>.
func (z ZFS) CreateVolume(ctx context.Context, args CreateVolumeArguments) error {
	var cmd strings.Builder
	cmd.WriteString("zfs create")

	if args.Sparse {
		cmd.WriteString(" -s")
	}

	if len(args.Options) > 0 {
		for key, value := range args.Options {
			cmd.WriteString(fmt.Sprintf(" -o %s=%s", key, value))
		}
	}

	cmd.WriteString(fmt.Sprintf(" -V %d %s", args.Size, args.Name))

	result, err := z.executor.Exec(ctx, cmd.String())
	if err != nil {
		// The command can fail with a non-zero exit code. The `Exec` method in
		// `lib/command/command.go` returns an error for non-zero exit codes. It
		// also returns the result. We wrap the error with more context.
		return fmt.Errorf("failed to create volume '%s': %w, stderr: %s", args.Name, err, result.Stderr)
	}

	return nil
}

// DestroyVolumeArguments represents the arguments for destroying a ZFS volume.
type DestroyVolumeArguments struct {
	Name string
}

// DestroyVolume destroys a ZFS volume.
//
// zfs destroy [-r] <volume>.
func (z ZFS) DestroyVolume(ctx context.Context, args DestroyVolumeArguments) error {
	var cmd strings.Builder
	cmd.WriteString("zfs destroy")

	cmd.WriteString(fmt.Sprintf(" %s", args.Name))

	result, err := z.executor.Exec(ctx, cmd.String())
	if err != nil {
		return fmt.Errorf("failed to destroy volume '%s': %w, stderr: %s", args.Name, err, result.Stderr)
	}

	return nil
}

// VolumeExistsArguments represents the arguments for checking if a ZFS volume exists.
type VolumeExistsArguments struct {
	Name string
}

// VolumeExists checks if a ZFS volume exists.
func (z ZFS) VolumeExists(ctx context.Context, args VolumeExistsArguments) (bool, error) {
	// Use `zfs list -H -o name` to check for the volume.
	// The -H flag gives script-friendly output (no headers).
	// We pipe to grep to check for an exact match.
	cmd := fmt.Sprintf("zfs list -H -o name | grep -x %s", args.Name)
	res, err := z.executor.Exec(ctx, cmd)
	if err != nil {
		// grep exits with 1 if no match is found. The command executor returns
		// an error on non-zero exit codes. If stderr is empty and exit code is
		// 1, it means the volume was not found, which is not an error for us.
		if res != nil && res.ExitCode == 1 && res.Stderr == "" {
			return false, nil
		}
		// For other errors, we return them.
		return false, err
	}

	// If grep exits with 0, a match was found.
	return res.ExitCode == 0, nil
}

// SetPropertyArguments represents the arguments for setting a ZFS property.
type SetPropertyArguments struct {
	Name          string
	PropertyKey   string
	PropertyValue string
}

// SetProperty sets a property on a ZFS dataset.
//
// zfs set <property>=<value> <dataset>.
func (z ZFS) SetProperty(ctx context.Context, args SetPropertyArguments) error {
	cmd := fmt.Sprintf("zfs set '%s'='%s' '%s'", args.PropertyKey, args.PropertyValue, args.Name)

	result, err := z.executor.Exec(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to set property '%s' on '%s': %w, stderr: %s", args.PropertyKey, args.Name, err, result.Stderr)
	}

	return nil
}

// GetPropertyArguments represents the arguments for getting a ZFS property.
type GetPropertyArguments struct {
	Name        string
	PropertyKey string
}

// GetProperty gets a property from a ZFS dataset.
//
// zfs get -Hp -o value <property> <dataset>.
func (z ZFS) GetProperty(ctx context.Context, args GetPropertyArguments) (string, error) {
	cmd := fmt.Sprintf("zfs get -Hp -o value '%s' '%s'", args.PropertyKey, args.Name)

	result, err := z.executor.Exec(ctx, cmd)
	if err != nil {
		if result != nil {
			stderr := strings.ReplaceAll(result.Stderr, "\n", "")
			if strings.Contains(stderr, "dataset does not exist") {
				return "", fmt.Errorf("dataset does not exist: %s", stderr)
			}
			if strings.Contains(stderr, "dataset is busy") {
				return "", fmt.Errorf("dataset is busy: %s", stderr)
			}
			return "", fmt.Errorf("failed to get property '%s' on '%s': %w, stderr: %s", args.PropertyKey, args.Name, err, result.Stderr)
		}
		return "", fmt.Errorf("failed to execute command: %w", err)
	}

	valueString := strings.TrimRight(result.Stdout, "\n")
	if valueString == "-" {
		return "", fmt.Errorf("property not set")
	}

	return valueString, nil
}
