package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	if err := Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"port", cfg.Port, ":6060"},
		{"storage_type", cfg.StorageType, "disk"},
		{"network_mode", cfg.NetworkMode, "fallback"},
		{"log_level", cfg.LogLevel, "info"},
		{"go_binary", cfg.GoBinary, "go"},
		{"unix_socket", cfg.UnixSocket, ""},
		{"tls_cert", cfg.TLSCertFile, ""},
		{"tls_key", cfg.TLSKeyFile, ""},
		{"go_proxy", cfg.GoProxy, ""},
		{"go_private", cfg.GoPrivate, ""},
		{"path_prefix", cfg.PathPrefix, ""},
		{"basic_auth_user", cfg.BasicAuthUser, ""},
		{"basic_auth_pass", cfg.BasicAuthPass, ""},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}

	if cfg.Disk.RootPath == "" {
		t.Error("expected non-empty default disk root path")
	}
	if cfg.Timeout != 300*time.Second {
		t.Errorf("timeout: got %v, want 5m", cfg.Timeout)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("shutdown_timeout: got %v, want 30s", cfg.ShutdownTimeout)
	}
	if cfg.S3.ForcePathStyle {
		t.Error("s3_force_path_style should default to false")
	}
	if cfg.Minio.EnableSSL {
		t.Error("minio_enable_ssl should default to false")
	}
	if cfg.ProxiedSumDBs != nil {
		t.Errorf("expected nil sum_dbs, got %v", cfg.ProxiedSumDBs)
	}
}

func TestEnvVarsOverrideDefaults(t *testing.T) {
	envs := map[string]string{
		"TOGA_PORT":             ":9999",
		"TOGA_STORAGE_TYPE":     "s3",
		"TOGA_LOG_LEVEL":        "debug",
		"TOGA_NETWORK_MODE":     "strict",
		"TOGA_S3_BUCKET":        "my-bucket",
		"TOGA_S3_REGION":        "us-west-2",
		"TOGA_TIMEOUT":          "60s",
		"TOGA_SHUTDOWN_TIMEOUT": "10s",
		"TOGA_BASIC_AUTH_USER":  "admin",
		"TOGA_BASIC_AUTH_PASS":  "secret",
		"TOGA_DISK_ROOT_PATH":   "/custom/path",
		"TOGA_PATH_PREFIX":      "/proxy",
		"TOGA_MINIO_ENABLE_SSL": "true",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	if err := Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	checks := []struct {
		name, got, want string
	}{
		{"port", cfg.Port, ":9999"},
		{"storage_type", cfg.StorageType, "s3"},
		{"log_level", cfg.LogLevel, "debug"},
		{"network_mode", cfg.NetworkMode, "strict"},
		{"s3_bucket", cfg.S3.Bucket, "my-bucket"},
		{"s3_region", cfg.S3.Region, "us-west-2"},
		{"basic_auth_user", cfg.BasicAuthUser, "admin"},
		{"basic_auth_pass", cfg.BasicAuthPass, "secret"},
		{"disk_root_path", cfg.Disk.RootPath, "/custom/path"},
		{"path_prefix", cfg.PathPrefix, "/proxy"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}

	if cfg.Timeout != 60*time.Second {
		t.Errorf("timeout: got %v, want 1m", cfg.Timeout)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("shutdown_timeout: got %v, want 10s", cfg.ShutdownTimeout)
	}
	if !cfg.Minio.EnableSSL {
		t.Error("minio_enable_ssl should be true from env")
	}
}

func TestConfigFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "toga-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`
port = ":4000"
storage_type = "minio"
log_level = "warn"
network_mode = "offline"
minio_endpoint = "minio.local:9000"
minio_bucket = "go-modules"
minio_key = "access-key"
minio_secret = "secret-key"
sum_dbs = "sum.golang.org,sum.golang.google.cn"
`)
	f.Close()

	if err := Init(f.Name()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	if cfg.Port != ":4000" {
		t.Errorf("port: got %q, want %q", cfg.Port, ":4000")
	}
	if cfg.StorageType != "minio" {
		t.Errorf("storage_type: got %q, want %q", cfg.StorageType, "minio")
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("log_level: got %q, want %q", cfg.LogLevel, "warn")
	}
	if cfg.NetworkMode != "offline" {
		t.Errorf("network_mode: got %q, want %q", cfg.NetworkMode, "offline")
	}
	if cfg.Minio.Endpoint != "minio.local:9000" {
		t.Errorf("minio_endpoint: got %q", cfg.Minio.Endpoint)
	}
	if cfg.Minio.Bucket != "go-modules" {
		t.Errorf("minio_bucket: got %q", cfg.Minio.Bucket)
	}
	if len(cfg.ProxiedSumDBs) != 2 {
		t.Errorf("sum_dbs: got %v, want 2 entries", cfg.ProxiedSumDBs)
	}
}

func TestEnvOverridesConfigFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "toga-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`port = ":4000"`)
	f.Close()

	t.Setenv("TOGA_PORT", ":7777")

	if err := Init(f.Name()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	if cfg.Port != ":7777" {
		t.Errorf("env should override file: got %q, want %q", cfg.Port, ":7777")
	}
}

func TestInitWithMissingExplicitConfigFile(t *testing.T) {
	err := Init("/nonexistent/toga.toml")
	if err == nil {
		t.Error("expected error for missing explicit config file")
	}
}

func TestInitWithMissingImplicitConfigFile(t *testing.T) {
	if err := Init(""); err != nil {
		t.Fatalf("Init should not fail without config file: %v", err)
	}
	cfg := Load()
	if cfg.Port != ":6060" {
		t.Errorf("expected default port, got %q", cfg.Port)
	}
}

func TestLoadPanicsWithoutInit(t *testing.T) {
	old := cm
	cm = nil
	defer func() { cm = old }()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from Load without Init")
		}
	}()
	Load()
}

func TestStorageTypeLowercased(t *testing.T) {
	t.Setenv("TOGA_STORAGE_TYPE", "S3")

	if err := Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	if cfg.StorageType != "s3" {
		t.Errorf("storage_type should be lowercased: got %q", cfg.StorageType)
	}
}

func TestAllStorageFieldsPopulated(t *testing.T) {
	envs := map[string]string{
		"TOGA_S3_KEY":                   "ak",
		"TOGA_S3_SECRET":                "sk",
		"TOGA_S3_TOKEN":                 "tok",
		"TOGA_S3_ENDPOINT":              "s3.local",
		"TOGA_S3_FORCE_PATH_STYLE":      "true",
		"TOGA_GCS_BUCKET":               "gcs-bucket",
		"TOGA_GCS_PROJECT_ID":           "my-project",
		"TOGA_GCS_CREDENTIALS_FILE":     "/creds.json",
		"TOGA_AZUREBLOB_ACCOUNT_NAME":   "acct",
		"TOGA_AZUREBLOB_ACCOUNT_KEY":    "key",
		"TOGA_AZUREBLOB_CONTAINER_NAME": "container",
		"TOGA_MINIO_ENDPOINT":           "minio:9000",
		"TOGA_MINIO_KEY":                "mkey",
		"TOGA_MINIO_SECRET":             "msecret",
		"TOGA_MINIO_BUCKET":             "mbucket",
		"TOGA_MINIO_REGION":             "us-east-1",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	if err := Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	// S3
	if cfg.S3.Key != "ak" {
		t.Errorf("s3.key: got %q", cfg.S3.Key)
	}
	if cfg.S3.Secret != "sk" {
		t.Errorf("s3.secret: got %q", cfg.S3.Secret)
	}
	if cfg.S3.Token != "tok" {
		t.Errorf("s3.token: got %q", cfg.S3.Token)
	}
	if cfg.S3.Endpoint != "s3.local" {
		t.Errorf("s3.endpoint: got %q", cfg.S3.Endpoint)
	}
	if !cfg.S3.ForcePathStyle {
		t.Error("s3.force_path_style should be true")
	}

	// GCS
	if cfg.GCS.Bucket != "gcs-bucket" {
		t.Errorf("gcs.bucket: got %q", cfg.GCS.Bucket)
	}
	if cfg.GCS.ProjectID != "my-project" {
		t.Errorf("gcs.project_id: got %q", cfg.GCS.ProjectID)
	}
	if cfg.GCS.CredentialsFile != "/creds.json" {
		t.Errorf("gcs.credentials_file: got %q", cfg.GCS.CredentialsFile)
	}

	// Azure
	if cfg.AzureBlob.AccountName != "acct" {
		t.Errorf("azure.account_name: got %q", cfg.AzureBlob.AccountName)
	}
	if cfg.AzureBlob.AccountKey != "key" {
		t.Errorf("azure.account_key: got %q", cfg.AzureBlob.AccountKey)
	}
	if cfg.AzureBlob.ContainerName != "container" {
		t.Errorf("azure.container_name: got %q", cfg.AzureBlob.ContainerName)
	}

	// Minio
	if cfg.Minio.Endpoint != "minio:9000" {
		t.Errorf("minio.endpoint: got %q", cfg.Minio.Endpoint)
	}
	if cfg.Minio.Key != "mkey" {
		t.Errorf("minio.key: got %q", cfg.Minio.Key)
	}
	if cfg.Minio.Secret != "msecret" {
		t.Errorf("minio.secret: got %q", cfg.Minio.Secret)
	}
	if cfg.Minio.Bucket != "mbucket" {
		t.Errorf("minio.bucket: got %q", cfg.Minio.Bucket)
	}
	if cfg.Minio.Region != "us-east-1" {
		t.Errorf("minio.region: got %q", cfg.Minio.Region)
	}
}

func TestGoEnvironmentFields(t *testing.T) {
	envs := map[string]string{
		"TOGA_GO_BINARY":    "/usr/local/go/bin/go",
		"TOGA_GO_MOD_CACHE": "/tmp/modcache",
		"TOGA_GO_PROXY":     "https://proxy.golang.org",
		"TOGA_GO_PRIVATE":   "github.com/private/*",
		"TOGA_GO_NOPROXY":   "github.com/private/*",
		"TOGA_GO_SUMDB":     "sum.golang.org",
		"TOGA_GO_NOSUMDB":   "github.com/private/*",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	if err := Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	if cfg.GoBinary != "/usr/local/go/bin/go" {
		t.Errorf("go_binary: got %q", cfg.GoBinary)
	}
	if cfg.GoModCache != "/tmp/modcache" {
		t.Errorf("go_mod_cache: got %q", cfg.GoModCache)
	}
	if cfg.GoProxy != "https://proxy.golang.org" {
		t.Errorf("go_proxy: got %q", cfg.GoProxy)
	}
	if cfg.GoPrivate != "github.com/private/*" {
		t.Errorf("go_private: got %q", cfg.GoPrivate)
	}
	if cfg.GoNoProxy != "github.com/private/*" {
		t.Errorf("go_noproxy: got %q", cfg.GoNoProxy)
	}
	if cfg.GoSumDB != "sum.golang.org" {
		t.Errorf("go_sumdb: got %q", cfg.GoSumDB)
	}
	if cfg.GoNoSumDB != "github.com/private/*" {
		t.Errorf("go_nosumdb: got %q", cfg.GoNoSumDB)
	}
}

func TestNestedTOMLTables(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "toga-nested-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`
storage_type = "minio"

[minio]
endpoint = "minio.local:9000"
bucket = "go-modules"
key = "access-key"
secret = "secret-key"
region = "us-west-2"
enable_ssl = true

[s3]
region = "eu-west-1"
bucket = "s3-bucket"
key = "s3-key"
secret = "s3-secret"
endpoint = "s3.local"
force_path_style = true

[disk]
root_path = "/data/modules"

[gcs]
bucket = "gcs-bucket"
project_id = "my-project"
credentials_file = "/creds.json"

[azureblob]
account_name = "acct"
account_key = "key"
container_name = "container"
`)
	f.Close()

	if err := Init(f.Name()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	if cfg.Minio.Endpoint != "minio.local:9000" {
		t.Errorf("minio.endpoint: got %q, want %q", cfg.Minio.Endpoint, "minio.local:9000")
	}
	if cfg.Minio.Bucket != "go-modules" {
		t.Errorf("minio.bucket: got %q, want %q", cfg.Minio.Bucket, "go-modules")
	}
	if cfg.Minio.Key != "access-key" {
		t.Errorf("minio.key: got %q", cfg.Minio.Key)
	}
	if cfg.Minio.Secret != "secret-key" {
		t.Errorf("minio.secret: got %q", cfg.Minio.Secret)
	}
	if cfg.Minio.Region != "us-west-2" {
		t.Errorf("minio.region: got %q", cfg.Minio.Region)
	}
	if !cfg.Minio.EnableSSL {
		t.Error("minio.enable_ssl should be true")
	}

	if cfg.S3.Region != "eu-west-1" {
		t.Errorf("s3.region: got %q", cfg.S3.Region)
	}
	if cfg.S3.Bucket != "s3-bucket" {
		t.Errorf("s3.bucket: got %q", cfg.S3.Bucket)
	}
	if cfg.S3.Key != "s3-key" {
		t.Errorf("s3.key: got %q", cfg.S3.Key)
	}
	if !cfg.S3.ForcePathStyle {
		t.Error("s3.force_path_style should be true")
	}

	if cfg.Disk.RootPath != "/data/modules" {
		t.Errorf("disk.root_path: got %q", cfg.Disk.RootPath)
	}

	if cfg.GCS.Bucket != "gcs-bucket" {
		t.Errorf("gcs.bucket: got %q", cfg.GCS.Bucket)
	}
	if cfg.GCS.ProjectID != "my-project" {
		t.Errorf("gcs.project_id: got %q", cfg.GCS.ProjectID)
	}

	if cfg.AzureBlob.AccountName != "acct" {
		t.Errorf("azure.account_name: got %q", cfg.AzureBlob.AccountName)
	}
	if cfg.AzureBlob.ContainerName != "container" {
		t.Errorf("azure.container_name: got %q", cfg.AzureBlob.ContainerName)
	}
}

func TestNestedTOMLEnvOverride(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "toga-override-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`
[minio]
endpoint = "minio.local:9000"
bucket = "file-bucket"
`)
	f.Close()

	t.Setenv("TOGA_MINIO_BUCKET", "env-bucket")

	if err := Init(f.Name()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	if cfg.Minio.Endpoint != "minio.local:9000" {
		t.Errorf("minio.endpoint: got %q, want %q", cfg.Minio.Endpoint, "minio.local:9000")
	}
	if cfg.Minio.Bucket != "env-bucket" {
		t.Errorf("env should override nested table: got %q, want %q", cfg.Minio.Bucket, "env-bucket")
	}
}
