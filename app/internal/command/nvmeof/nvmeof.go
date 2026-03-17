// Package nvmeof contains lib/command wrappers for executing and working with NVMe-oF.
package nvmeof

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/jovulic/zfsilo/lib/command"
	"github.com/jovulic/zfsilo/lib/genericutil"
	"github.com/jovulic/zfsilo/lib/stringutil"
)

type NQN string

func (val NQN) String() string {
	return string(val)
}

// NVMeOF provides an interface for interacting with NVMe-oF.
type NVMeOF struct {
	executor command.Executor
}

// With creates a new NVMeOF instance.
func With(executor command.Executor) NVMeOF {
	return NVMeOF{
		executor: executor,
	}
}

type PublishVolumeArguments struct {
	VolumeID   string
	DevicePath string
	TargetNQN  NQN
}

var publishVolumeTmpl = genericutil.Must(
	template.New("publish_volume").Parse(
		stringutil.Multiline(`
			# Navigate to subsystems
			cd /subsystems
			create {{.TargetNQN}}
			# Disallow any host to connect (enforce ACLs)
			cd /subsystems/{{.TargetNQN}}
			set attr allow_any_host=0
			# Add a namespace (1) to the subsystem
			cd namespaces
			create 1
			cd 1
			set device path={{.DevicePath}}
			enable
			# Create a port if it doesn't exist, and configure it
			cd /ports
			# Using create will navigate into the port if it exists or create it.
			create 1
			cd /ports/1
			set addr trtype=tcp
			set addr traddr=0.0.0.0
			set addr trsvcid=4420
			set addr adrfam=ipv4
			# Bind the subsystem to the port
			cd /ports/1/subsystems
			create {{.TargetNQN}}
			# Navigate back to root
			cd /
		`),
	),
)

func (n NVMeOF) PublishVolume(ctx context.Context, args PublishVolumeArguments) error {
	var buf bytes.Buffer
	if err := publishVolumeTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render publish volume template: %w", err)
	}

	cmd := fmt.Sprintf("echo \"%s\" | nvmetcli", buf.String())

	result, err := n.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to publish volume '%s': %w, stderr: %s", args.VolumeID, err, stderr)
	}

	return nil
}

type UnpublishVolumeArguments struct {
	TargetNQN NQN
}

var unpublishVolumeTmpl = genericutil.Must(
	template.New("unpublish_volume").Parse(
		stringutil.Multiline(`
			# Unbind the subsystem from the port
			cd /ports/1/subsystems
			delete {{.TargetNQN}}
			# Delete the subsystem (which will also delete its namespaces)
			cd /subsystems
			delete {{.TargetNQN}}
			# Navigate back to root
			cd /
		`),
	),
)

func (n NVMeOF) UnpublishVolume(ctx context.Context, args UnpublishVolumeArguments) error {
	var buf bytes.Buffer
	if err := unpublishVolumeTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render unpublish volume template: %w", err)
	}

	cmd := fmt.Sprintf("echo \"%s\" | nvmetcli", buf.String())

	result, err := n.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to unpublish volume target '%s': %w, stderr: %s", args.TargetNQN, err, stderr)
	}

	return nil
}

type AuthorizeArguments struct {
	TargetNQN         NQN
	InitiatorNQN      NQN
	InitiatorPassword string // DH-HMAC-CHAP key
	TargetPassword    string // Optional mutual auth DH-HMAC-CHAP key
}

func (n NVMeOF) Authorize(ctx context.Context, args AuthorizeArguments) error {
	// Setup NVMe-oF authentication and ACLs using direct configfs (sysfs) commands.
	// nvmetcli is avoided here due to version compatibility issues with DH-HMAC-CHAP.
	cmd := fmt.Sprintf(
		stringutil.Multiline(`
			mkdir -p /sys/kernel/config/nvmet/hosts/%[1]s &&
			ln -sf /sys/kernel/config/nvmet/hosts/%[1]s /sys/kernel/config/nvmet/subsystems/%[2]s/allowed_hosts/%[1]s &&
			echo \"%[3]s\" > /sys/kernel/config/nvmet/hosts/%[1]s/dhchap_key
		`),
		args.InitiatorNQN, args.TargetNQN, args.InitiatorPassword,
	)

	if args.TargetPassword != "" {
		cmd += fmt.Sprintf(" && echo \"%s\" > /sys/kernel/config/nvmet/hosts/%s/dhchap_ctrl_key", args.TargetPassword, args.InitiatorNQN)
	}

	if _, err := n.executor.Exec(ctx, cmd); err != nil {
		return fmt.Errorf("failed to authorize initiator '%s' for target '%s' via sysfs: %w", args.InitiatorNQN, args.TargetNQN, err)
	}

	return nil
}

type UnauthorizeArguments struct {
	TargetNQN    NQN
	InitiatorNQN NQN
}

func (n NVMeOF) Unauthorize(ctx context.Context, args UnauthorizeArguments) error {
	// Remove NVMe-oF authentication and ACLs using direct configfs (sysfs) commands.
	cmd := fmt.Sprintf(
		stringutil.Multiline(`
			rm -f /sys/kernel/config/nvmet/subsystems/%s/allowed_hosts/%s &&
			rmdir /sys/kernel/config/nvmet/hosts/%s
		`),
		args.TargetNQN, args.InitiatorNQN, args.InitiatorNQN,
	)

	if _, err := n.executor.Exec(ctx, cmd); err != nil {
		return fmt.Errorf("failed to unauthorize initiator '%s' for target '%s' via sysfs: %w", args.InitiatorNQN, args.TargetNQN, err)
	}

	return nil
}

type ConnectTargetArguments struct {
	TargetNQN         NQN
	TargetAddress     string
	InitiatorNQN      NQN
	InitiatorPassword string // DH-HMAC-CHAP key
	TargetPassword    string // Optional mutual auth DH-HMAC-CHAP key
}

var connectTargetTmpl = genericutil.Must(
	template.New("connect_target").Parse(
		stringutil.Multiline(`
			( nvme connect -t tcp -n '{{.TargetNQN}}' -a "{{.TargetAddress}}" -s '4420' -q '{{.InitiatorNQN}}' {{if .InitiatorPassword}}-S '{{.InitiatorPassword}}'{{end}} {{if .TargetPassword}}-C '{{.TargetPassword}}'{{end}} )
		`),
	),
)

func (n NVMeOF) ConnectTarget(ctx context.Context, args ConnectTargetArguments) error {
	var buf bytes.Buffer
	if err := connectTargetTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render connect target template: %w", err)
	}

	cmd := strings.ReplaceAll(buf.String(), "\n", " ")

	result, err := n.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to connect target '%s': %w, stderr: %s", args.TargetNQN, err, stderr)
	}

	return nil
}

type DisconnectTargetArguments struct {
	TargetNQN NQN
}

var disconnectTargetTmpl = genericutil.Must(
	template.New("disconnect_target").Parse(
		stringutil.Multiline(`
			( nvme disconnect -n '{{.TargetNQN}}' )
		`),
	),
)

func (n NVMeOF) DisconnectTarget(ctx context.Context, args DisconnectTargetArguments) error {
	var buf bytes.Buffer
	if err := disconnectTargetTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render disconnect target template: %w", err)
	}

	cmd := strings.ReplaceAll(buf.String(), "\n", " ")

	result, err := n.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to disconnect target '%s': %w, stderr: %s", args.TargetNQN, err, stderr)
	}

	return nil
}

type RescanTargetArguments struct {
	TargetNQN NQN
}

func (n NVMeOF) RescanTarget(ctx context.Context, args RescanTargetArguments) error {
	var cmd string
	if args.TargetNQN != "" {
		// Find the device associated with the NQN and rescan it.
		// We grep for the device name (e.g., nvme0) in the output of list-subsys.
		cmd = fmt.Sprintf(
			"DEV=$(nvme list-subsys -n '%s' | grep -oE 'nvme[0-9]+' | head -n 1) && [ -n \"$DEV\" ] && nvme ns-rescan /dev/$DEV",
			args.TargetNQN,
		)
	} else {
		// Rescan all NVMe controllers.
		cmd = "for dev in /dev/nvme[0-9]; do nvme ns-rescan $dev; done"
	}

	result, err := n.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to rescan nvme: %w, stderr: %s", err, stderr)
	}

	return nil
}
