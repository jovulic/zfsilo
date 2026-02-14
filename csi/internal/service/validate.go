package service

import (
	"path/filepath"
	"regexp"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	volumeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9-_]+$`)
)

// validateVolumeCapability checks a single capability for valid access mode and type.
func validateVolumeCapability(c *csi.VolumeCapability) error {
	if c.GetAccessMode() == nil {
		return status.Error(codes.InvalidArgument, "volume capability access mode not defined")
	}
	if c.GetAccessMode().GetMode() == csi.VolumeCapability_AccessMode_UNKNOWN {
		return status.Error(codes.InvalidArgument, "volume capability access mode unknown")
	}
	if c.GetAccessType() == nil {
		return status.Error(codes.InvalidArgument, "volume capability access type not defined")
	}

	switch t := c.AccessType.(type) {
	case *csi.VolumeCapability_Mount:
		// okay
	case *csi.VolumeCapability_Block:
		// okay
	default:
		return status.Errorf(codes.InvalidArgument, "unsupported access type %T", t)
	}

	accessMode := c.AccessMode.Mode
	switch accessMode {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
		// okay
	default:
		return status.Errorf(codes.InvalidArgument, "unsupported access  mode %s", accessMode)
	}

	return nil
}

// validateVolumeCapabilities checks a list of capabilities.
func validateVolumeCapabilities(caps []*csi.VolumeCapability) error {
	if len(caps) == 0 {
		return status.Error(codes.InvalidArgument, "volume capabilities must be provided")
	}
	for _, c := range caps {
		if err := validateVolumeCapability(c); err != nil {
			return err
		}
	}
	return nil
}

// validateCapacityRange checks if the capacity range is logically valid.
func validateCapacityRange(capRange *csi.CapacityRange) error {
	if capRange == nil {
		return nil // It's optional
	}

	reqBytes := capRange.GetRequiredBytes()
	limitBytes := capRange.GetLimitBytes()

	if reqBytes < 0 {
		return status.Error(codes.InvalidArgument, "required bytes must not be negative")
	}
	if limitBytes < 0 {
		return status.Error(codes.InvalidArgument, "limit bytes must not be negative")
	}

	// Logic check: Limit cannot be less than required if both are set
	if limitBytes > 0 && limitBytes < reqBytes {
		return status.Errorf(codes.InvalidArgument,
			"limit bytes (%v) cannot be less than required bytes (%v)", limitBytes, reqBytes)
	}
	return nil
}

// validateVolumeName checks for basic name requirements.
func validateVolumeName(name string) error {
	if name == "" {
		return status.Error(codes.InvalidArgument, "volume name cannot be empty")
	}
	if !volumeNameRegex.MatchString(name) {
		return status.Errorf(codes.InvalidArgument, "volume name contains invalid characters: %s", name)
	}
	return nil
}

// validateVolumeID checks that the volume ID is not empty.
func validateVolumeID(id string) error {
	if id == "" {
		return status.Error(codes.InvalidArgument, "volume id cannot be empty")
	}
	return nil
}

// validateNodeID checks that the node ID is not empty.
func validateNodeID(id string) error {
	if id == "" {
		return status.Error(codes.InvalidArgument, "node id cannot be empty")
	}
	return nil
}

// validateMaxEntries checks that the max entries value is non-negative.
func validateMaxEntries(maxEntries int32) error {
	if maxEntries < 0 {
		return status.Errorf(codes.InvalidArgument, "max entries must not be negative: %d", maxEntries)
	}
	return nil
}

// validateTargetPath checks that the path is not empty and is absolute.
func validateTargetPath(path string) error {
	if path == "" {
		return status.Error(codes.InvalidArgument, "target path cannot be empty")
	}
	if !filepath.IsAbs(path) {
		return status.Errorf(codes.InvalidArgument, "target path must be absolute: %s", path)
	}
	return nil
}

func validateCreateVolumeRequest(req *csi.CreateVolumeRequest) error {
	if err := validateVolumeName(req.GetName()); err != nil {
		return err
	}

	if err := validateVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return err
	}

	if err := validateCapacityRange(req.GetCapacityRange()); err != nil {
		return err
	}

	if Parameters(req.GetParameters()).ParentDatasetID() == "" {
		return status.Error(codes.InvalidArgument, "parameters[parent_dataset_id] is empty")
	}

	return nil
}

func validateDeleteVolumeRequest(req *csi.DeleteVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	return nil
}

func validateControllerPublishVolumeRequest(req *csi.ControllerPublishVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	if err := validateNodeID(req.GetNodeId()); err != nil {
		return err
	}

	if err := validateVolumeCapability(req.GetVolumeCapability()); err != nil {
		return err
	}

	return nil
}

func validateControllerUnpublishVolumeRequest(req *csi.ControllerUnpublishVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	// NodeID is OPTIONAL.
	// If empty, it implies "unpublish from all nodes". If NOT empty, we validate
	// it using our standard helper.
	if req.GetNodeId() != "" {
		if err := validateNodeID(req.GetNodeId()); err != nil {
			return err
		}
	}

	return nil
}

func validateValidateVolumeCapabilitiesRequest(req *csi.ValidateVolumeCapabilitiesRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	if err := validateVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return err
	}

	return nil
}

func validateListVolumesRequest(req *csi.ListVolumesRequest) error {
	// MaxEntries is OPTIONAL, but if set, must be non-negative.
	if err := validateMaxEntries(req.GetMaxEntries()); err != nil {
		return err
	}

	return nil
}

func validateGetCapacityRequest(req *csi.GetCapacityRequest) error {
	// VolumeCapabilities is OPTIONAL, but if set they must be valid.
	if len(req.GetVolumeCapabilities()) > 0 {
		if err := validateVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
			return err
		}
	}

	return nil
}

func validateControllerExpandVolumeRequest(req *csi.ControllerExpandVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	if req.GetCapacityRange() == nil {
		return status.Error(codes.InvalidArgument, "capacity range cannot be empty")
	}
	if err := validateCapacityRange(req.GetCapacityRange()); err != nil {
		return err
	}

	// VolumeCapability is OPTIONAL, if set it must be valid.
	if req.GetVolumeCapability() != nil {
		if err := validateVolumeCapability(req.GetVolumeCapability()); err != nil {
			return err
		}
	}

	return nil
}

func validateControllerGetVolumeRequest(req *csi.ControllerGetVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	return nil
}

func validateControllerModifyVolumeRequest(req *csi.ControllerModifyVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	return nil
}

func validateNodePublishVolumeRequest(req *csi.NodePublishVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	if err := validateTargetPath(req.GetTargetPath()); err != nil {
		return err
	}

	if err := validateVolumeCapability(req.GetVolumeCapability()); err != nil {
		return err
	}

	// StagingTargetPath is OPTIONAL, if it is set, it must be a valid absolute
	// path.
	if req.GetStagingTargetPath() != "" {
		if err := validateTargetPath(req.GetStagingTargetPath()); err != nil {
			return err
		}
	}

	return nil
}

func validateNodeUnpublishVolumeRequest(req *csi.NodeUnpublishVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	if err := validateTargetPath(req.GetTargetPath()); err != nil {
		return err
	}

	return nil
}

func validateNodeGetVolumeStatsRequest(req *csi.NodeGetVolumeStatsRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	// The path where the volume is mounted.
	if err := validateTargetPath(req.GetVolumePath()); err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid volume path: %v", err)
	}

	// StagingTargetPath is OPTIONAL, if set, it must be a valid absolute path.
	if req.GetStagingTargetPath() != "" {
		if err := validateTargetPath(req.GetStagingTargetPath()); err != nil {
			return status.Errorf(codes.InvalidArgument, "invalid staging target path: %v", err)
		}
	}

	return nil
}

func validateNodeExpandVolumeRequest(req *csi.NodeExpandVolumeRequest) error {
	if err := validateVolumeID(req.GetVolumeId()); err != nil {
		return err
	}

	// The path where the volume is currently published.
	if err := validateTargetPath(req.GetVolumePath()); err != nil {
		return status.Errorf(codes.InvalidArgument, "Invalid volume_path: %v", err)
	}

	// CapacityRange is OPTIONAL, if set, it must be logically valid.
	if req.GetCapacityRange() != nil {
		if err := validateCapacityRange(req.GetCapacityRange()); err != nil {
			return err
		}
	}

	// StagingTargetPath is OPTIONAL, if set, it must be a valid absolute path.
	if req.GetStagingTargetPath() != "" {
		if err := validateTargetPath(req.GetStagingTargetPath()); err != nil {
			return status.Errorf(codes.InvalidArgument, "Invalid staging_target_path: %v", err)
		}
	}

	// VolumeCapability is OPTIONAL, if set, it must be a valid capability
	// structure.
	if req.GetVolumeCapability() != nil {
		if err := validateVolumeCapability(req.GetVolumeCapability()); err != nil {
			return err
		}
	}

	return nil
}
