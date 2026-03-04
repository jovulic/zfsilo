// Package iscsi contains lib/command wrappers for executing and working with iSCSI.
package iscsi

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/jovulic/zfsilo/lib/command"
	"github.com/jovulic/zfsilo/lib/genericutil"
	"github.com/jovulic/zfsilo/lib/stringutil"
)

type IQN string

func (val IQN) String() string {
	return string(val)
}

type Host struct {
	domain    string
	ownerTime time.Time
	hostname  string
}

func NewHost(domain string, ownerTime time.Time, hostname string) *Host {
	return &Host{
		domain:    domain,
		ownerTime: ownerTime,
		hostname:  hostname,
	}
}

func (h *Host) IQN() IQN {
	value := fmt.Sprintf(
		"iqn.%s.%s.%s",
		h.ownerTime.Format("2006-01"),
		func(domain string) string {
			parts := strings.Split(domain, ".")

			// Need at least two parts for the "naming authority" in order to pass validation.
			if len(parts) == 1 {
				parts = append(parts, "local")
			}

			// Reverse the order of parts slice.
			// https://github.com/golang/go/wiki/SliceTricks#reversing
			for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
				parts[left], parts[right] = parts[right], parts[left]
			}

			return strings.Join(parts, ".")
		}(h.domain),
		h.hostname,
	)
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "-")
	return IQN(value)
}

func (h *Host) VolumeIQN(volumeID string) IQN {
	value := h.IQN().String()
	value = fmt.Sprintf("%s:%s", value, volumeID)
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "_", "-")
	return IQN(value)
}

// ISCSI provides an interface for interacting with iSCSI.
type ISCSI struct {
	executor command.Executor
}

// With creates a new ISCSI instance.
func With(executor command.Executor) ISCSI {
	return ISCSI{
		executor: executor,
	}
}

type PublishVolumeArguments struct {
	VolumeID   string
	DevicePath string
	TargetIQN  IQN
}

var publishVolumeTmpl = genericutil.Must(
	template.New("publish_volume").Parse(
		stringutil.Multiline(`
			# Create a backstore with the block device.
			cd /backstores/block
			create {{.VolumeID}} {{.DevicePath}}
			# Create the iSCSI target.
			cd /iscsi
			create {{.TargetIQN}}
			# Add LUN to the iSCSI target.
			cd /iscsi/{{.TargetIQN}}/tpg1/luns
			create /backstores/block/{{.VolumeID}}
			# Setup TPG attributes.
			cd /iscsi/{{.TargetIQN}}/tpg1
			set attribute demo_mode_write_protect=0
			set attribute generate_node_acls=0
			set attribute cache_dynamic_acls=0
			set attribute authentication=1
			# Navigate back to root.
			cd /
		`),
	),
)

func (i ISCSI) PublishVolume(ctx context.Context, args PublishVolumeArguments) error {
	var buf bytes.Buffer
	if err := publishVolumeTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render publish volume template: %w", err)
	}

	cmd := fmt.Sprintf("echo \"%s\" | targetcli", buf.String())

	result, err := i.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to publish volume '%s': %w, stderr: %s", args.VolumeID, err, stderr)
	}

	return nil
}

type AuthorizeArguments struct {
	TargetIQN         IQN
	TargetPassword    string
	InitiatorIQN      IQN
	InitiatorPassword string
}

var authorizeTmpl = genericutil.Must(
	template.New("authorize").Parse(
		stringutil.Multiline(`
			# Create ACL for the initiator.
			cd /iscsi/{{.TargetIQN}}/tpg1/acls
			create {{.InitiatorIQN}}
			# Setup ACL authentication.
			cd /iscsi/{{.TargetIQN}}/tpg1/acls/{{.InitiatorIQN}}
			set auth userid={{.InitiatorIQN}}
			set auth password={{.InitiatorPassword}}
			set auth mutual_userid={{.TargetIQN}}
			set auth mutual_password={{.TargetPassword}}
			# Navigate back to root.
			cd /
		`),
	),
)

func (i ISCSI) Authorize(ctx context.Context, args AuthorizeArguments) error {
	var buf bytes.Buffer
	if err := authorizeTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render authorize template: %w", err)
	}

	cmd := fmt.Sprintf("echo \"%s\" | targetcli", buf.String())

	result, err := i.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to authorize initiator '%s' for target '%s': %w, stderr: %s", args.InitiatorIQN, args.TargetIQN, err, stderr)
	}

	return nil
}

type UnauthorizeArguments struct {
	TargetIQN    IQN
	InitiatorIQN IQN
}

var unauthorizeTmpl = genericutil.Must(
	template.New("unauthorize").Parse(
		stringutil.Multiline(`
			# Delete ACL for the initiator.
			cd /iscsi/{{.TargetIQN}}/tpg1/acls
			delete {{.InitiatorIQN}}
			# Navigate back to root.
			cd /
		`),
	),
)

func (i ISCSI) Unauthorize(ctx context.Context, args UnauthorizeArguments) error {
	var buf bytes.Buffer
	if err := unauthorizeTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render unauthorize template: %w", err)
	}

	cmd := fmt.Sprintf("echo \"%s\" | targetcli", buf.String())

	result, err := i.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to unauthorize initiator '%s' for target '%s': %w, stderr: %s", args.InitiatorIQN, args.TargetIQN, err, stderr)
	}

	return nil
}

type UnpublishVolumeArguments struct {
	TargetIQN IQN
	VolumeID  string
}

var unpublishVolumeTmpl = genericutil.Must(
	template.New("unpublish_volume").Parse(
		stringutil.Multiline(`
			# Delete ISCSI target.
			cd /iscsi
			delete {{.TargetIQN}}
			# Delete backstore device.
			cd /backstores/block
			delete {{.VolumeID}}
			# Navigate back to root.
			cd /
		`),
	),
)

func (i ISCSI) UnpublishVolume(ctx context.Context, args UnpublishVolumeArguments) error {
	var buf bytes.Buffer
	if err := unpublishVolumeTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render unpublish volume template: %w", err)
	}

	cmd := fmt.Sprintf("echo \"%s\" | targetcli", buf.String())

	result, err := i.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to unpublish volume '%s': %w, stderr: %s", args.VolumeID, err, stderr)
	}

	return nil
}

type ConnectTargetArguments struct {
	TargetAddress     string
	TargetIQN         IQN
	TargetPassword    string
	InitiatorIQN      IQN
	InitiatorPassword string
}

var connectTargetTmpl = genericutil.Must(
	template.New("connect_target").Parse(
		stringutil.Multiline(`
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --op new ) &&
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --op update --name node.session.auth.authmethod --value CHAP ) &&
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --op update --name node.session.auth.username --value '{{.InitiatorIQN}}' ) &&
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --op update --name node.session.auth.password --value '{{.InitiatorPassword}}' ) &&
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --op update --name node.session.auth.username_in --value '{{.TargetIQN}}' ) &&
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --op update --name node.session.auth.password_in --value '{{.TargetPassword}}' ) &&
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --login )
		`),
	),
)

func (i ISCSI) ConnectTarget(ctx context.Context, args ConnectTargetArguments) error {
	var buf bytes.Buffer
	if err := connectTargetTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render connect target template: %w", err)
	}

	cmd := strings.ReplaceAll(buf.String(), "\n", " ")

	result, err := i.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to connect target '%s': %w, stderr: %s", args.TargetIQN, err, stderr)
	}

	return nil
}

type DisconnectTargetArguments struct {
	TargetIQN     IQN
	TargetAddress string
}

var disconnectTargetTmpl = genericutil.Must(
	template.New("disconnect_target").Parse(
		stringutil.Multiline(`
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --logout ) &&
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --op delete )
		`),
	),
)

func (i ISCSI) DisconnectTarget(ctx context.Context, args DisconnectTargetArguments) error {
	var buf bytes.Buffer
	if err := disconnectTargetTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render disconnect target template: %w", err)
	}

	cmd := strings.ReplaceAll(buf.String(), "\n", " ")

	result, err := i.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to disconnect target '%s': %w, stderr: %s", args.TargetIQN, err, stderr)
	}

	return nil
}

type RescanTargetArguments struct {
	TargetIQN     IQN
	TargetAddress string
}

var rescanTargetTmpl = genericutil.Must(
	template.New("rescan_target").Parse(
		stringutil.Multiline(`
			( iscsiadm --mode node --targetname '{{.TargetIQN}}' --portal "{{.TargetAddress}}" --rescan )
		`),
	),
)

func (i ISCSI) RescanTarget(ctx context.Context, args RescanTargetArguments) error {
	var buf bytes.Buffer
	if err := rescanTargetTmpl.Execute(&buf, args); err != nil {
		return fmt.Errorf("failed to render rescan target template: %w", err)
	}

	cmd := strings.ReplaceAll(buf.String(), "\n", " ")

	result, err := i.executor.Exec(ctx, cmd)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = result.Stderr
		}
		return fmt.Errorf("failed to rescan target '%s': %w, stderr: %s", args.TargetIQN, err, stderr)
	}

	return nil
}
