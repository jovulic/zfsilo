// Package service defines the application services.
package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/jovulic/zfsilo/csi/internal/extvar"
	"github.com/jovulic/zfsilo/lib/structutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type ParameterOption struct {
	Key   string
	Value string
}

type Parameters map[string]string

func (dict Parameters) ParentDatasetID() string {
	return dict["parent_dataset_id"]
}

func (dict Parameters) Options() []ParameterOption {
	var options []ParameterOption
	for key, value := range dict {
		if key, ok := strings.CutPrefix(key, "o_"); ok {
			option := ParameterOption{
				Key:   key,
				Value: value,
			}
			options = append(options, option)
		}
	}
	return options
}

func (dict Parameters) Sparse() bool {
	value := dict["sparse"]
	return value == "true"
}

func validateCapacityRange(capacityRange *csi.CapacityRange) error {
	if capacityRange == nil {
		return nil
	}

	requiredBytes := capacityRange.RequiredBytes
	limitBytes := capacityRange.LimitBytes

	if limitBytes > 0 && requiredBytes > limitBytes {
		return errors.New("required bytes is greater than limit bytes")
	}

	return nil
}

func validateVolumeCapabilities(volumeCapabilities []*csi.VolumeCapability) error {
	for _, volumeCapability := range volumeCapabilities {
		switch t := volumeCapability.AccessType.(type) {
		case *csi.VolumeCapability_Mount:
			// okay
		case *csi.VolumeCapability_Block:
			// okay
		default:
			return fmt.Errorf("unsupported access type %T", t)
		}

		accessMode := volumeCapability.AccessMode.Mode
		switch accessMode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
			// okay
		default:
			return fmt.Errorf("unsupported access mode =%s", accessMode.String())
		}
	}

	return nil
}

type CSIServiceConfig struct {
	Secret              string   `validate:"required"`
	StoreAddress        string   `validate:"required"`
	TargetPortalAddress string   `validate:"required"`
	InitiatorIQN        string   `validate:"required"`
	KnownInitiatorIQNs  []string `validate:"required"`
}

type CSIService struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer

	secret              string
	storeAddress        string
	targetPortalAddress string
	initiatorIQN        string
	knownInitiatorIQNs  []string

	lock    sync.Mutex
	started bool
	conn    *grpc.ClientConn
}

func NewCSIService(config CSIServiceConfig) *CSIService {
	if err := structutil.Apply(&config); err != nil {
		message := fmt.Sprintf("command: failed to process config: %s", err)
		panic(message)
	}
	return &CSIService{
		secret:              config.Secret,
		storeAddress:        config.StoreAddress,
		targetPortalAddress: config.TargetPortalAddress,
		initiatorIQN:        config.InitiatorIQN,
		knownInitiatorIQNs:  config.KnownInitiatorIQNs,
	}
}

func (s *CSIService) Start(ctx context.Context) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.started {
		return nil
	}

	{
		conn, err := grpc.NewClient(
			s.storeAddress,
			grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})),
		)
		if err != nil {
			return fmt.Errorf("failed to dial %s: %w", s.storeAddress, err)
		}
		s.conn = conn
	}

	s.started = true
	return nil
}

func (s *CSIService) Stop(ctx context.Context) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if !s.started {
		return nil
	}

	if err := s.conn.Close(); err != nil {
		return fmt.Errorf("failed to close client conn: %w", err)
	}
	s.conn = nil

	s.started = false
	return nil
}

func (s *CSIService) GetPluginInfo(context.Context, *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          "zfsilo-csi",
		VendorVersion: extvar.Version,
		Manifest:      nil,
	}, nil
}

func (s *CSIService) GetPluginCapabilities(context.Context, *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_ONLINE,
					},
				},
			},
		},
	}, nil
}

func (s *CSIService) Probe(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{
		Ready: &wrapperspb.BoolValue{Value: true},
	}, nil
}

func (s *CSIService) CreateVolume(context.Context, *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateVolume not implemented")
}

func (s *CSIService) DeleteVolume(context.Context, *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteVolume not implemented")
}

func (s *CSIService) ControllerPublishVolume(context.Context, *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerPublishVolume not implemented")
}

func (s *CSIService) ControllerUnpublishVolume(context.Context, *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerUnpublishVolume not implemented")
}

func (s *CSIService) ValidateVolumeCapabilities(context.Context, *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ValidateVolumeCapabilities not implemented")
}

func (s *CSIService) ListVolumes(context.Context, *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListVolumes not implemented")
}

func (s *CSIService) GetCapacity(context.Context, *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetCapacity not implemented")
}

func (s *CSIService) ControllerGetCapabilities(context.Context, *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerGetCapabilities not implemented")
}

func (s *CSIService) CreateSnapshot(context.Context, *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateSnapshot not implemented")
}

func (s *CSIService) DeleteSnapshot(context.Context, *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method DeleteSnapshot not implemented")
}

func (s *CSIService) ListSnapshots(context.Context, *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListSnapshots not implemented")
}

func (s *CSIService) GetSnapshot(context.Context, *csi.GetSnapshotRequest) (*csi.GetSnapshotResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSnapshot not implemented")
}

func (s *CSIService) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerExpandVolume not implemented")
}

func (s *CSIService) ControllerGetVolume(context.Context, *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerGetVolume not implemented")
}

func (s *CSIService) ControllerModifyVolume(context.Context, *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ControllerModifyVolume not implemented")
}

func (s *CSIService) NodeStageVolume(context.Context, *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NodeStageVolume not implemented")
}

func (s *CSIService) NodeUnstageVolume(context.Context, *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NodeUnstageVolume not implemented")
}

func (s *CSIService) NodePublishVolume(context.Context, *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NodePublishVolume not implemented")
}

func (s *CSIService) NodeUnpublishVolume(context.Context, *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NodeUnpublishVolume not implemented")
}

func (s *CSIService) NodeGetVolumeStats(context.Context, *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NodeGetVolumeStats not implemented")
}

func (s *CSIService) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NodeExpandVolume not implemented")
}

func (s *CSIService) NodeGetCapabilities(context.Context, *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NodeGetCapabilities not implemented")
}

func (s *CSIService) NodeGetInfo(context.Context, *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method NodeGetInfo not implemented")
}
