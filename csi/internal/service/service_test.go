// Package service defines the application services.
package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/jovulic/zfsilo/lib/command"
	"github.com/kubernetes-csi/csi-test/v5/pkg/sanity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestCSISanity(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CSI Sanity Suite")
}

var _ = Describe("CSIService Sanity", func() {
	var (
		srv        *CSIService
		grpcServer *grpc.Server
		endpoint   string
		stopChan   chan struct{}
		config     sanity.TestConfig
	)

	BeforeEach(func() {
		ctx := context.Background()

		// Clean up any existing directories from previous failed runs.
		_ = os.RemoveAll("/tmp/csi-mount")
		_ = os.RemoveAll("/tmp/csi-staging")

		// Use environment variables for configuration, with sensible defaults for
		// dev environment.
		zfsiloAddress := os.Getenv("ZFSILO_ADDRESS")
		if zfsiloAddress == "" {
			zfsiloAddress = "https://127.0.0.1:8080"
		}

		targetPortalAddress := os.Getenv("ZFSILO_TARGET_PORTAL_ADDRESS")
		if targetPortalAddress == "" {
			// Dynamically resolve 'give' address from 'take' host perspective.
			takeExecutor := command.NewRemoteExecutor(command.RemoteExecutorConfig{
				Address:  "localhost",
				Port:     9100,
				Username: "root",
				Password: "",
			})
			err := takeExecutor.Startup(ctx)
			Expect(err).NotTo(HaveOccurred(), "failed to startup take executor")
			defer takeExecutor.Shutdown(ctx)

			result, err := takeExecutor.Exec(ctx, "dig +short give")
			Expect(err).NotTo(HaveOccurred(), "failed to resolve give address")
			address := strings.TrimSpace(result.Stdout)
			Expect(address).NotTo(BeEmpty(), "resolved give address is empty")

			targetPortalAddress = address + ":3260"
		}

		initiatorIQN := os.Getenv("ZFSILO_INITIATOR_IQN")
		if initiatorIQN == "" {
			initiatorIQN = "iqn.2006-01.org.linux-iscsi.take"
		}

		secret := os.Getenv("ZFSILO_SECRET")
		if secret == "" {
			secret = "sk_token"
		}

		parentDatasetID := os.Getenv("ZFSILO_PARENT_DATASET_ID")
		if parentDatasetID == "" {
			parentDatasetID = "tank"
		}

		srv = NewCSIService(CSIServiceConfig{
			Secret:              secret,
			ZFSiloAddress:       zfsiloAddress,
			TargetPortalAddress: targetPortalAddress,
			InitiatorIQN:        initiatorIQN,
			KnownInitiatorIQNs:  []string{initiatorIQN},
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
			grpcServer.GracefulStop()
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
