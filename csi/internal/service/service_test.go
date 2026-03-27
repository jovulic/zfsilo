package service_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/container-storage-interface/spec/lib/go/csi"
	zfsilov1 "github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1"
	"github.com/jovulic/zfsilo/api/gen/go/zfsilo/v1/zfsilov1connect"
	"github.com/jovulic/zfsilo/csi/internal/service"
	"github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func wipeBackend(ctx context.Context, address, secret string) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	authInterceptor := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if secret != "" {
				req.Header().Set("Authorization", "Bearer "+secret)
			}
			return next(ctx, req)
		}
	})

	client := zfsilov1connect.NewVolumeServiceClient(
		httpClient,
		address,
		connect.WithInterceptors(authInterceptor),
	)

	// List all volumes and delete them.
	resp, err := client.ListVolumes(ctx, connect.NewRequest(&zfsilov1.ListVolumesRequest{
		PageSize: 100,
	}))
	if err != nil {
		fmt.Printf("failed to list volumes for wipe: %v\n", err)
		return
	}

	for _, vol := range resp.Msg.Volumes {
		// Try to tear down if needed.
		if vol.Status >= zfsilov1.Volume_STATUS_MOUNTED {
			for _, path := range vol.TargetPaths {
				_, _ = client.UnmountVolume(ctx, connect.NewRequest(&zfsilov1.UnmountVolumeRequest{
					Id:        vol.Id,
					MountPath: path,
				}))
			}
		}
		if vol.Status >= zfsilov1.Volume_STATUS_STAGED {
			_, _ = client.UnstageVolume(ctx, connect.NewRequest(&zfsilov1.UnstageVolumeRequest{
				Id: vol.Id,
			}))
		}
		if vol.Status >= zfsilov1.Volume_STATUS_CONNECTED {
			_, _ = client.DisconnectVolume(ctx, connect.NewRequest(&zfsilov1.DisconnectVolumeRequest{
				Id: vol.Id,
			}))
		}
		if vol.Status >= zfsilov1.Volume_STATUS_PUBLISHED {
			_, _ = client.UnpublishVolume(ctx, connect.NewRequest(&zfsilov1.UnpublishVolumeRequest{
				Id: vol.Id,
			}))
		}

		_, err := client.DeleteVolume(ctx, connect.NewRequest(&zfsilov1.DeleteVolumeRequest{Id: vol.Id}))
		if err != nil {
			fmt.Printf("failed to delete volume %s during wipe: %v\n", vol.Id, err)
		}
	}
}

func TestCSISanity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CSI sanity tests in short mode")
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "CSI Sanity Suite")
}

var _ = Describe("CSIService Sanity", func() {
	for _, transport := range []string{"iscsi", "nvmeof"} {
		Context(fmt.Sprintf("with %s transport", transport), Ordered, func() {
			var (
				srv             *service.CSIService
				grpcServer      *grpc.Server
				endpoint        string
				stopChan        chan struct{}
				config          sanity.TestConfig
				zfsiloAddress   string
				secret          string
				nodeID          string
				parentDatasetID string
			)

			BeforeAll(func() {
				ctx := context.Background()

				zfsiloAddress = os.Getenv("ZFSILO_ADDRESS")
				if zfsiloAddress == "" {
					zfsiloAddress = "https://127.0.0.1:8080"
				}
				secret = os.Getenv("ZFSILO_SECRET")
				if secret == "" {
					secret = "sk_token"
				}

				wipeBackend(ctx, zfsiloAddress, secret)

				nodeID = os.Getenv("ZFSILO_NODE_ID")
				if nodeID == "" {
					nodeID = "hst_take"
				}

				parentDatasetID = os.Getenv("ZFSILO_PARENT_DATASET_ID")
				if parentDatasetID == "" {
					parentDatasetID = "tank"
				}
			})

			BeforeEach(func() {
				ctx := context.Background()

				// Clean up any existing directories from previous failed runs.
				_ = os.RemoveAll("/tmp/csi-mount")
				_ = os.RemoveAll("/tmp/csi-staging")

				srv = service.NewCSIService(service.CSIServiceConfig{
					Secret:        secret,
					ZFSiloAddress: zfsiloAddress,
					PublishHost:   "hst_give",
					HostID:        nodeID,
				})

				err := srv.Start(ctx)
				Expect(err).NotTo(HaveOccurred())

				grpcServer = grpc.NewServer()
				csi.RegisterIdentityServer(grpcServer, srv)
				csi.RegisterControllerServer(grpcServer, srv)
				csi.RegisterNodeServer(grpcServer, srv)

				listener, err := net.Listen("tcp", "127.0.0.1:0")
				Expect(err).NotTo(HaveOccurred())
				endpoint = listener.Addr().String()

				stopChan = make(chan struct{})
				go func() {
					defer GinkgoRecover()
					err := grpcServer.Serve(listener)
					if err != nil && err != grpc.ErrServerStopped {
						fmt.Printf("grpc server failed: %v\n", err)
					}
					close(stopChan)
				}()

				// Initialize sanity config.
				config = sanity.NewTestConfig()
				config.Address = endpoint
				config.DialOptions = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
				config.ControllerAddress = endpoint
				config.ControllerDialOptions = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
				config.TestVolumeSize = 1024 * 1024 * 100 // 100MB
				config.TestVolumeParameters = map[string]string{
					"parent_dataset_id": parentDatasetID,
					"sparse":            "true",
					"transport":         transport,
				}

				config.CreateTargetDir = func(path string) (string, error) {
					if err := os.MkdirAll(path, 0o755); err != nil {
						return "", err
					}
					return path, nil
				}
				config.RemoveTargetPath = func(path string) error {
					return os.RemoveAll(path)
				}
			})

			AfterEach(func() {
				ctx := context.Background()
				if grpcServer != nil {
					grpcServer.Stop()
					<-stopChan
				}
				if srv != nil {
					_ = srv.Stop(ctx)
				}
			})

			Describe("Sanity Tests", func() {
				sanity.GinkgoTest(&config)
			})
		})
	}
})
