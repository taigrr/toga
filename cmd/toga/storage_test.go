package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/taigrr/toga/internal/config"
)

func TestValidateStorageDiskRequiresRootPath(t *testing.T) {
	cfg := &config.Config{StorageType: "disk"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty disk root path")
	}
}

func TestValidateStorageDiskValid(t *testing.T) {
	cfg := &config.Config{
		StorageType: "disk",
		Disk:        config.DiskConfig{RootPath: "/tmp/test"},
	}
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageS3RequiresBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "s3"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty s3 bucket")
	}
}

func TestValidateStorageS3Valid(t *testing.T) {
	cfg := &config.Config{
		StorageType: "s3",
		S3:          config.S3Config{Bucket: "my-bucket"},
	}
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageMinioRequiresEndpointAndBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "minio"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty minio endpoint/bucket")
	}
}

func TestValidateStorageMinioPartial(t *testing.T) {
	cfg := &config.Config{
		StorageType: "minio",
		Minio:       config.MinioConfig{Endpoint: "localhost:9000"},
	}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for missing minio bucket")
	}
}

func TestValidateStorageGCSRequiresBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "gcs"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty gcs bucket")
	}
}

func TestValidateStorageAzureRequiresAccountAndContainer(t *testing.T) {
	cfg := &config.Config{StorageType: "azureblob"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty azure account/container")
	}
}

func TestValidateStorageAzurePartial(t *testing.T) {
	cfg := &config.Config{
		StorageType: "azureblob",
		AzureBlob:   config.AzureBlobConfig{AccountName: "myacct"},
	}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for missing azure container_name")
	}
}

func TestValidateStorageMemory(t *testing.T) {
	cfg := &config.Config{StorageType: "memory"}
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error for memory storage: %v", err)
	}
}

func TestValidateStorageUnknownType(t *testing.T) {
	cfg := &config.Config{StorageType: "unknown"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for unknown storage type")
	}
}

func TestNewCacherMemory(t *testing.T) {
	cfg := &config.Config{StorageType: "memory"}
	c, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil cacher")
	}
}

func TestNewCacherDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		StorageType: "disk",
		Disk:        config.DiskConfig{RootPath: dir},
	}
	c, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil cacher")
	}
}

func TestNewCacherUnknown(t *testing.T) {
	cfg := &config.Config{StorageType: "bogus"}
	_, err := newCacher(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for unknown storage type")
	}
}

func TestNewCacherDiskMissingRootPath(t *testing.T) {
	cfg := &config.Config{StorageType: "disk"}
	_, err := newCacher(context.Background(), cfg)
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestMemoryCacherRoundTrip(t *testing.T) {
	cfg := &config.Config{StorageType: "memory"}
	cacher, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	name := "github.com/example/mod/@v/v1.0.0.info"
	content := `{"Version":"v1.0.0","Time":"2025-01-01T00:00:00Z"}`

	if err := cacher.Put(ctx, name, strings.NewReader(content)); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	rc, err := cacher.Get(ctx, name)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}

func TestMemoryCacherMiss(t *testing.T) {
	cfg := &config.Config{StorageType: "memory"}
	cacher, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = cacher.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for cache miss")
	}
}

func TestDiskCacherRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		StorageType: "disk",
		Disk:        config.DiskConfig{RootPath: dir},
	}
	cacher, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	name := "github.com/example/mod/@v/v1.0.0.info"
	content := `{"Version":"v1.0.0"}`

	if err := cacher.Put(ctx, name, strings.NewReader(content)); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	rc, err := cacher.Get(ctx, name)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}
