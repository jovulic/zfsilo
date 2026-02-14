// Package service defines the application services.
package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"slices"
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
	"google.golang.org/protobuf/types/known/structpb"
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

	nodeID := req.GetNodeId()
	found := slices.Contains(s.knownInitiatorIQNs, nodeID)
	if !found {
		return nil, status.Errorf(codes.NotFound, "node %s not found", nodeID)
	}

	id := req.GetVolumeId()

	// Publish (make target available).
	_, err := s.volumeClient.PublishVolume(ctx, connect.NewRequest(&zfsilov1.PublishVolumeRequest{Id: id}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %s not found", id)
		}
		return nil, status.Errorf(codes.Internal, "failed to publish volume: %v", err)
	}

	// Connect (associate with node and login).
	connectResp, err := s.volumeClient.ConnectVolume(ctx, connect.NewRequest(&zfsilov1.ConnectVolumeRequest{
		Id:            id,
		InitiatorIqn:  nodeID,
		TargetAddress: s.targetPortalAddress,
	}))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to connect volume to node: %v", err)
	}

	// Verify it's connected to the right node.
	if connectResp.Msg.Volume.InitiatorIqn != nil && *connectResp.Msg.Volume.InitiatorIqn != "" && *connectResp.Msg.Volume.InitiatorIqn != nodeID {
		return nil, status.Errorf(codes.FailedPrecondition, "volume %s is already connected to another node: %s", id, *connectResp.Msg.Volume.InitiatorIqn)
	}

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (s *CSIService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if err := validateControllerUnpublishVolumeRequest(req); err != nil {
		return nil, err
	}

	id := req.GetVolumeId()
	nodeID := req.GetNodeId()

	// Get volume status.
	getResp, err := s.volumeClient.GetVolume(ctx, connect.NewRequest(&zfsilov1.GetVolumeRequest{Id: id}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	vol := getResp.Msg.Volume

	// Check node id. If it is published to a different node, it's already
	// "unpublished".
	if nodeID != "" && vol.InitiatorIqn != nil && *vol.InitiatorIqn != "" && *vol.InitiatorIqn != nodeID {
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// Disconnect if connected.
	if vol.Status >= zfsilov1.Volume_STATUS_CONNECTED {
		_, err := s.volumeClient.DisconnectVolume(ctx, connect.NewRequest(&zfsilov1.DisconnectVolumeRequest{Id: id}))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to disconnect volume: %v", err)
		}
	}

	// Unpublish if published.
	if vol.Status >= zfsilov1.Volume_STATUS_PUBLISHED {
		_, err := s.volumeClient.UnpublishVolume(ctx, connect.NewRequest(&zfsilov1.UnpublishVolumeRequest{Id: id}))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to unpublish volume: %v", err)
		}
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *CSIService) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if err := validateValidateVolumeCapabilitiesRequest(req); err != nil {
		return nil, err
	}

	id := req.GetVolumeId()

	// Fetch volume metadata.
	resp, err := s.volumeClient.GetVolume(ctx, connect.NewRequest(&zfsilov1.GetVolumeRequest{Id: id}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %s not found", id)
		}
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	vol := resp.Msg.Volume

	// Compare requested capabilities against actual volume capabilities.
	for _, cap := range req.GetVolumeCapabilities() {
		if cap.GetBlock() != nil {
			if vol.Mode != zfsilov1.Volume_MODE_BLOCK {
				return &csi.ValidateVolumeCapabilitiesResponse{
					Message: fmt.Sprintf("volume %s is not in block mode", id),
				}, nil
			}
		}
		if cap.GetMount() != nil {
			if vol.Mode != zfsilov1.Volume_MODE_FILESYSTEM {
				return &csi.ValidateVolumeCapabilitiesResponse{
					Message: fmt.Sprintf("volume %s is not in filesystem mode", id),
				}, nil
			}
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.GetVolumeCapabilities(),
		},
	}, nil
}

func (s *CSIService) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	if err := validateListVolumesRequest(req); err != nil {
		return nil, err
	}

	zreq := &zfsilov1.ListVolumesRequest{
		PageSize:  req.GetMaxEntries(),
		PageToken: req.GetStartingToken(),
	}

	resp, err := s.volumeClient.ListVolumes(ctx, connect.NewRequest(zreq))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeInvalidArgument {
			return nil, status.Errorf(codes.Aborted, "invalid starting token: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to list volumes: %v", err)
	}

	entries := make([]*csi.ListVolumesResponse_Entry, 0, len(resp.Msg.Volumes))
	for _, vol := range resp.Msg.Volumes {
		var publishedNodeIds []string
		if vol.InitiatorIqn != nil && *vol.InitiatorIqn != "" {
			publishedNodeIds = []string{*vol.InitiatorIqn}
		}

		entries = append(entries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      vol.Id,
				CapacityBytes: vol.CapacityBytes,
			},
			Status: &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: publishedNodeIds,
			},
		})
	}

	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: resp.Msg.NextPageToken,
	}, nil
}

func (s *CSIService) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	if err := validateGetCapacityRequest(req); err != nil {
		return nil, err
	}

	resp, err := s.serviceClient.GetCapacity(ctx, connect.NewRequest(&zfsilov1.GetCapacityRequest{}))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get capacity: %v", err)
	}

	return &csi.GetCapacityResponse{
		AvailableCapacity: resp.Msg.AvailableCapacityBytes,
	}, nil
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

	id := req.GetVolumeId()
	capacityRange := req.GetCapacityRange()
	requiredBytes := capacityRange.GetRequiredBytes()

	// Get current volume status.
	getResp, err := s.volumeClient.GetVolume(ctx, connect.NewRequest(&zfsilov1.GetVolumeRequest{Id: id}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %s not found", id)
		}
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	vol := getResp.Msg.Volume

	// If the current volume is already large enough, return success.
	if vol.CapacityBytes >= requiredBytes {
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         vol.CapacityBytes,
			NodeExpansionRequired: true,
		}, nil
	}

	// Perform expansion via update volume.
	updateStruct, err := structpb.NewStruct(map[string]any{
		"id":             id,
		"capacity_bytes": float64(requiredBytes),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create update struct: %v", err)
	}

	updateResp, err := s.volumeClient.UpdateVolume(ctx, connect.NewRequest(&zfsilov1.UpdateVolumeRequest{
		Volume: updateStruct,
	}))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to expand volume: %v", err)
	}

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         updateResp.Msg.Volume.CapacityBytes,
		NodeExpansionRequired: true,
	}, nil
}

func (s *CSIService) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	if err := validateControllerGetVolumeRequest(req); err != nil {
		return nil, err
	}

	id := req.GetVolumeId()

	resp, err := s.volumeClient.GetVolume(ctx, connect.NewRequest(&zfsilov1.GetVolumeRequest{Id: id}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %s not found", id)
		}
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	vol := resp.Msg.Volume
	var publishedNodeIds []string
	if vol.InitiatorIqn != nil && *vol.InitiatorIqn != "" {
		publishedNodeIds = []string{*vol.InitiatorIqn}
	}

	return &csi.ControllerGetVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      vol.Id,
			CapacityBytes: vol.CapacityBytes,
		},
		Status: &csi.ControllerGetVolumeResponse_VolumeStatus{
			PublishedNodeIds: publishedNodeIds,
		},
	}, nil
}

func (s *CSIService) ControllerModifyVolume(ctx context.Context, req *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	if err := validateControllerModifyVolumeRequest(req); err != nil {
		return nil, err
	}

	id := req.GetVolumeId()
	mutableParams := req.GetMutableParameters()
	options := Parameters(mutableParams).Options()

	// Convert options to backend format (list of objects with key/value).
	zfsOptions := make([]any, 0, len(options))
	for _, opt := range options {
		zfsOptions = append(zfsOptions, map[string]any{
			"key":   opt.Key,
			"value": opt.Value,
		})
	}

	updateMap := map[string]any{
		"id":      id,
		"options": zfsOptions,
	}

	updateStruct, err := structpb.NewStruct(updateMap)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create update struct: %v", err)
	}

	_, err = s.volumeClient.UpdateVolume(ctx, connect.NewRequest(&zfsilov1.UpdateVolumeRequest{
		Volume: updateStruct,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %s not found", id)
		}
		return nil, status.Errorf(codes.Internal, "failed to modify volume: %v", err)
	}

	return &csi.ControllerModifyVolumeResponse{}, nil
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

	id := req.GetVolumeId()
	targetPath := req.GetTargetPath()

	// Get volume.
	getResp, err := s.volumeClient.GetVolume(ctx, connect.NewRequest(&zfsilov1.GetVolumeRequest{Id: id}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, status.Errorf(codes.NotFound, "volume %s not found", id)
		}
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}
	vol := getResp.Msg.Volume

	// If already mounted, check if path matches.
	if vol.Status >= zfsilov1.Volume_STATUS_MOUNTED {
		if vol.MountPath != nil && *vol.MountPath == targetPath {
			return &csi.NodePublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.FailedPrecondition, "volume %s is already mounted at %s", id, *vol.MountPath)
	}

	// Ensure connected to this node.
	if vol.Status < zfsilov1.Volume_STATUS_CONNECTED || vol.InitiatorIqn == nil || *vol.InitiatorIqn != s.initiatorIQN {
		_, err := s.volumeClient.ConnectVolume(ctx, connect.NewRequest(&zfsilov1.ConnectVolumeRequest{
			Id:            id,
			InitiatorIqn:  s.initiatorIQN,
			TargetAddress: s.targetPortalAddress,
		}))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to connect volume: %v", err)
		}
	}

	// Mount volume.
	_, err = s.volumeClient.MountVolume(ctx, connect.NewRequest(&zfsilov1.MountVolumeRequest{
		Id:        id,
		MountPath: targetPath,
	}))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to mount volume: %v", err)
	}

	return &csi.NodePublishVolumeResponse{}, nil
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
