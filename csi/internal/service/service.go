// Package service defines the application services.
package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"connectrpc.com/connect"
	"github.com/container-storage-interface/spec/lib/go/csi"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/csi/internal/extvar"
	"github.com/jovulic/zfsilo/lib/structutil"
	"google.golang.org/grpc/codes"
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

type CSIServiceConfig struct {
	Secret              string   `validate:"required"`
	ZFSiloAddress       string   `validate:"required"`
	TargetPortalAddress string   `validate:"required"`
	InitiatorIQN        string   `validate:"required"`
	KnownInitiatorIQNs  []string `validate:"required"`
}

// CSIService implements the CSI specification.
//
// specification: https://github.com/container-storage-interface/spec/blob/master/spec.md
type CSIService struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer

	secret              string
	zfsiloAddress       string
	targetPortalAddress string
	initiatorIQN        string
	knownInitiatorIQNs  []string

	lock          sync.Mutex
	started       bool
	volumeClient  zfsilov1connect.VolumeServiceClient
	serviceClient zfsilov1connect.ServiceClient
}

func (s *CSIService) toVolumeID(name string) string {
	return "vol_" + name
}

func (s *CSIService) toVolumeName(name string) string {
	return "volumes/" + s.toVolumeID(name)
}

func (s *CSIService) toDatasetID(name string, parentDatasetID string) string {
	return parentDatasetID + "/" + s.toVolumeID(name)
}

func (s *CSIService) authInterceptor() connect.Interceptor {
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if s.secret != "" {
				req.Header().Set("Authorization", "Bearer "+s.secret)
			}
			return next(ctx, req)
		}
	})
}

func NewCSIService(config CSIServiceConfig) *CSIService {
	if err := structutil.Apply(&config); err != nil {
		message := fmt.Sprintf("command: failed to process config: %s", err)
		panic(message)
	}
	return &CSIService{
		secret:              config.Secret,
		zfsiloAddress:       config.ZFSiloAddress,
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

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	s.volumeClient = zfsilov1connect.NewVolumeServiceClient(
		httpClient,
		s.zfsiloAddress,
		connect.WithInterceptors(s.authInterceptor()),
	)
	s.serviceClient = zfsilov1connect.NewServiceClient(
		httpClient,
		s.zfsiloAddress,
		connect.WithInterceptors(s.authInterceptor()),
	)

	s.started = true
	return nil
}

func (s *CSIService) Stop(ctx context.Context) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if !s.started {
		return nil
	}

	s.volumeClient = nil
	s.serviceClient = nil

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

func (s *CSIService) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if err := validateCreateVolumeRequest(req); err != nil {
		return nil, err
	}

	params := Parameters(req.GetParameters())
	name := req.GetName()
	id := s.toVolumeID(name)
	datasetID := s.toDatasetID(name, params.ParentDatasetID())

	// Determine mode. Default to filesystem if not specified.
	mode := zfsilov1.Volume_MODE_FILESYSTEM
	for _, cap := range req.GetVolumeCapabilities() {
		if cap.GetBlock() != nil {
			mode = zfsilov1.Volume_MODE_BLOCK
			break
		}
	}

	// Determine capacity. Defaults to 1GB.
	capacityBytes := req.GetCapacityRange().GetRequiredBytes()
	if capacityBytes == 0 {
		capacityBytes = 1 * 1024 * 1024 * 1024
	}

	options := params.Options()
	zfsOptions := make([]*zfsilov1.Volume_Option, 0, len(options))
	for _, opt := range options {
		zfsOptions = append(zfsOptions, &zfsilov1.Volume_Option{
			Key:   opt.Key,
			Value: opt.Value,
		})
	}

	resp, err := s.volumeClient.CreateVolume(ctx, connect.NewRequest(&zfsilov1.CreateVolumeRequest{
		Volume: &zfsilov1.Volume{
			Id:            id,
			Name:          s.toVolumeName(name),
			DatasetId:     datasetID,
			Mode:          mode,
			CapacityBytes: capacityBytes,
			Sparse:        params.Sparse(),
			Options:       zfsOptions,
		},
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeAlreadyExists {
			// Check if the volume already exists and is compatible.
			getResp, getErr := s.volumeClient.GetVolume(ctx, connect.NewRequest(&zfsilov1.GetVolumeRequest{Id: id}))
			if getErr != nil {
				// Return original "already exists" error if GetVolume fails.
				return nil, err
			}

			vol := getResp.Msg.Volume
			if vol.CapacityBytes == capacityBytes && vol.DatasetId == datasetID {
				return &csi.CreateVolumeResponse{
					Volume: &csi.Volume{
						VolumeId:      id,
						CapacityBytes: vol.CapacityBytes,
					},
				}, nil
			}
			return nil, status.Error(codes.AlreadyExists, "volume already exists with different parameters")
		}
		return nil, status.Errorf(codes.Internal, "failed to create volume: %v", err)
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      id,
			CapacityBytes: resp.Msg.Volume.CapacityBytes,
		},
	}, nil
}

func (s *CSIService) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if err := validateDeleteVolumeRequest(req); err != nil {
		return nil, err
	}

	id := req.GetVolumeId()

	_, err := s.volumeClient.DeleteVolume(ctx, connect.NewRequest(&zfsilov1.DeleteVolumeRequest{
		Id: id,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return &csi.DeleteVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to delete volume: %v", err)
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (s *CSIService) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if err := validateControllerPublishVolumeRequest(req); err != nil {
		return nil, err
	}

	// TODO: Idempotency check (Has this volume already been published to this node?)
	// TODO: Max volumes per node check
	// TODO: Backend attach logic

	return nil, status.Errorf(codes.Unimplemented, "method ControllerPublishVolume not implemented")
}

func (s *CSIService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if err := validateControllerUnpublishVolumeRequest(req); err != nil {
		return nil, err
	}

	// TODO: Idempotency Check (Is volume already detached?)
	// TODO: Check if NodeID is empty -> Detach from ALL nodes.
	// TODO: Check if NodeID is set -> Detach from specific node.

	return nil, status.Errorf(codes.Unimplemented, "method ControllerUnpublishVolume not implemented")
}

func (s *CSIService) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if err := validateValidateVolumeCapabilitiesRequest(req); err != nil {
		return nil, err
	}

	// TODO: Fetch volume metadata.
	// TODO: Compare requested capabilities against actual volume capabilities.
	// TODO: Compare requested context against actual context.

	return nil, status.Errorf(codes.Unimplemented, "method ValidateVolumeCapabilities not implemented")
}

func (s *CSIService) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	if err := validateListVolumesRequest(req); err != nil {
		return nil, err
	}

	// TODO: Pagination Logic
	// TODO: Fetch volumes from backend
	// TODO: Encode NextToken

	return nil, status.Errorf(codes.Unimplemented, "method ListVolumes not implemented")
}

func (s *CSIService) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	if err := validateGetCapacityRequest(req); err != nil {
		return nil, err
	}

	// TODO: Filter available capacity by provided Capabilities (if any)
	// TODO: Filter available capacity by provided Parameters (if any)
	// TODO: Filter available capacity by Topology (if provided)
	// TODO: Calculate available bytes

	return nil, status.Errorf(codes.Unimplemented, "method GetCapacity not implemented")
}

func (s *CSIService) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_MODIFY_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (s *CSIService) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Errorf(codes.InvalidArgument, "method CreateSnapshot not supported")
}

func (s *CSIService) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Errorf(codes.InvalidArgument, "method DeleteSnapshot not supported")
}

func (s *CSIService) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Errorf(codes.InvalidArgument, "method ListSnapshots not supported")
}

func (s *CSIService) GetSnapshot(ctx context.Context, req *csi.GetSnapshotRequest) (*csi.GetSnapshotResponse, error) {
	return nil, status.Errorf(codes.InvalidArgument, "method GetSnapshot not supported")
}

func (s *CSIService) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	if err := validateControllerExpandVolumeRequest(req); err != nil {
		return nil, err
	}

	// TODO: Idempotency Check (Is volume already >= requested size?)
	// TODO: Check if volume is online/offline (capabilities check)
	// TODO: Backend expansion logic
	// TODO: Determine if node expansion is required

	return nil, status.Errorf(codes.Unimplemented, "method ControllerExpandVolume not implemented")
}

func (s *CSIService) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	if err := validateControllerGetVolumeRequest(req); err != nil {
		return nil, err
	}

	// TODO: Fetch volume status
	// TODO: Return volume status (including condition if supported)

	return nil, status.Errorf(codes.Unimplemented, "method ControllerGetVolume not implemented")
}

func (s *CSIService) ControllerModifyVolume(ctx context.Context, req *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	if err := validateControllerModifyVolumeRequest(req); err != nil {
		return nil, err
	}

	// TODO: Idempotency Check (Are these parameters already applied?)
	// TODO: Verify support for specific keys (Are "iops" or "tier" supported?)
	// TODO: Backend modification logic

	return nil, status.Errorf(codes.Unimplemented, "method ControllerModifyVolume not implemented")
}

func (s *CSIService) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return nil, status.Errorf(codes.InvalidArgument, "method NodeStageVolume not supported")
}

func (s *CSIService) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Errorf(codes.InvalidArgument, "method NodeUnstageVolume not supported")
}

func (s *CSIService) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if err := validateNodePublishVolumeRequest(req); err != nil {
		return nil, err
	}

	// TODO: Check if StagingTargetPath is required (based on Plugin Capabilities)
	// TODO: Idempotency Check (Is it already mounted?)
	// TODO: Mount Logic (Bind mount, format, etc.)

	return nil, status.Errorf(codes.Unimplemented, "method NodePublishVolume not implemented")
}

func (s *CSIService) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if err := validateNodeUnpublishVolumeRequest(req); err != nil {
		return nil, err
	}

	// TODO: Idempotency Check (Is it already unmounted?)
	// TODO: Unmount Logic (syscall.Unmount, remove mount point directory)

	return nil, status.Errorf(codes.Unimplemented, "method NodeUnpublishVolume not implemented")
}

func (s *CSIService) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	if err := validateNodeGetVolumeStatsRequest(req); err != nil {
		return nil, err
	}

	// TODO: Check if path exists
	// TODO: Run 'df' or 'statfs' syscall on the path
	// TODO: Run inode check

	return nil, status.Errorf(codes.Unimplemented, "method NodeGetVolumeStats not implemented")
}

func (s *CSIService) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	if err := validateNodeExpandVolumeRequest(req); err != nil {
		return nil, err
	}

	// TODO: Resize the filesystem (e.g. resize2fs, xfs_growfs)
	// TODO: Check if volume is block or mount
	// TODO: Handle offline expansion if necessary

	return nil, status.Errorf(codes.Unimplemented, "method NodeExpandVolume not implemented")
}

func (s *CSIService) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (s *CSIService) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{NodeId: s.initiatorIQN}, nil
}
