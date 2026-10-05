// Package s3test starts a throwaway S3-compatible object store (RustFS) in a
// container for integration tests of S3-backed storage.
package s3test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// Image is the S3-compatible server used for tests.
	Image = "rustfs/rustfs:1.0.1"
	// AccessKey is the root access key configured on the test server.
	AccessKey = "rustfsadmin"
	// SecretKey is the root secret key configured on the test server.
	SecretKey = "rustfsadmin"
	// Region is the region passed to clients; RustFS accepts any value.
	Region = "us-east-1"

	s3Port = "9000/tcp"
)

// Server describes a running S3-compatible test server.
type Server struct {
	// Endpoint is the full http:// URL of the S3 API.
	Endpoint string
	// HostPort is the host:port of the S3 API without a scheme.
	HostPort string
}

// Start launches an S3-compatible container and returns its endpoint.
// The container is terminated when the test finishes.
func Start(t *testing.T) Server {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        Image,
		ExposedPorts: []string{s3Port},
		Env: map[string]string{
			"RUSTFS_ACCESS_KEY":     AccessKey,
			"RUSTFS_SECRET_KEY":     SecretKey,
			"RUSTFS_CONSOLE_ENABLE": "false",
		},
		WaitingFor: wait.ForHTTP("/health/ready").
			WithPort(s3Port).
			WithStartupTimeout(2 * time.Minute),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start s3 test container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	hostPort, err := container.PortEndpoint(ctx, s3Port, "")
	if err != nil {
		t.Fatalf("get s3 endpoint: %v", err)
	}
	return Server{Endpoint: "http://" + hostPort, HostPort: hostPort}
}
