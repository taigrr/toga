package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goproxy/goproxy"
	"github.com/taigrr/toga/internal/config"
)

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "ok\n" {
		t.Errorf("expected ok, got %q", body)
	}
}

func TestBasicAuthRejectsInvalid(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "wrong")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestBasicAuthAcceptsValid(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestValidateStorageDiskRequiresRootPath(t *testing.T) {
	cfg := &config.Config{StorageType: "disk"}
	err := validateStorage(cfg)
	if err == nil {
		t.Error("expected error for empty disk root path")
	}
}

func TestValidateStorageS3RequiresBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "s3"}
	err := validateStorage(cfg)
	if err == nil {
		t.Error("expected error for empty s3 bucket")
	}
}

func TestValidateStorageMinioRequiresEndpointAndBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "minio"}
	err := validateStorage(cfg)
	if err == nil {
		t.Error("expected error for empty minio endpoint/bucket")
	}
}

func TestValidateStorageGCSRequiresBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "gcs"}
	err := validateStorage(cfg)
	if err == nil {
		t.Error("expected error for empty gcs bucket")
	}
}

func TestValidateStorageAzureRequiresAccountAndContainer(t *testing.T) {
	cfg := &config.Config{StorageType: "azureblob"}
	err := validateStorage(cfg)
	if err == nil {
		t.Error("expected error for empty azure account/container")
	}
}

func TestBuildHandlerHealthz(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{}
	handler := buildHandler(proxy, cfg)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for healthz, got %d", w.Code)
	}
}

func TestBuildHandlerReadyz(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{}
	handler := buildHandler(proxy, cfg)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for readyz, got %d", w.Code)
	}
}

func TestBasicAuthNoCredentials(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
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

func TestValidateStorageS3Valid(t *testing.T) {
	cfg := &config.Config{
		StorageType: "s3",
		S3:          config.S3Config{Bucket: "my-bucket"},
	}
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageUnknownType(t *testing.T) {
	cfg := &config.Config{StorageType: "unknown"}
	// validateStorage allows unknown types (newCacher catches them)
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
