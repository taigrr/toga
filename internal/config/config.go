package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the toga proxy.
type Config struct {
	Port        string
	UnixSocket  string
	TLSCertFile string
	TLSKeyFile  string

	// Storage backend: disk, s3, minio, gcs, azureblob
	StorageType string

	// Go environment
	GoBinary   string
	GoModCache string
	GoProxy    string
	GoPrivate  string
	GoNoProxy  string
	GoSumDB    string
	GoNoSumDB  string

	// Timeouts
	Timeout         time.Duration
	ShutdownTimeout time.Duration

	// Logging
	LogLevel string

	// Storage configs
	Disk      DiskConfig
	S3        S3Config
	Minio     MinioConfig
	GCS       GCSConfig
	AzureBlob AzureBlobConfig

	// Proxy behavior
	ProxiedSumDBs []string
	PathPrefix    string
	BasicAuthUser string
	BasicAuthPass string

	// Network mode: strict (upstream only), offline (cache only), fallback (cache then upstream)
	NetworkMode string
}

// DiskConfig holds configuration for the filesystem storage backend.
type DiskConfig struct {
	RootPath string
}

// S3Config holds configuration for the S3 storage backend.
type S3Config struct {
	Region         string
	Key            string
	Secret         string
	Token          string
	Bucket         string
	Endpoint       string
	ForcePathStyle bool
}

// MinioConfig holds configuration for the MinIO storage backend.
type MinioConfig struct {
	Endpoint  string
	Key       string
	Secret    string
	Bucket    string
	Region    string
	EnableSSL bool
}

// GCSConfig holds configuration for the Google Cloud Storage backend.
type GCSConfig struct {
	Bucket          string
	ProjectID       string
	CredentialsFile string
}

// AzureBlobConfig holds configuration for the Azure Blob Storage backend.
type AzureBlobConfig struct {
	AccountName   string
	AccountKey    string
	ContainerName string
}

// Load reads configuration from environment variables, maintaining
// compatibility with Athens environment variable names where possible.
func Load() *Config {
	cfg := &Config{
		Port:            envOr("ATHENS_PORT", "TOGA_PORT", ":3000"),
		UnixSocket:      envOr("ATHENS_UNIX_SOCKET", "TOGA_UNIX_SOCKET", ""),
		TLSCertFile:     envOr("ATHENS_TLSCERT_FILE", "TOGA_TLS_CERT", ""),
		TLSKeyFile:      envOr("ATHENS_TLSKEY_FILE", "TOGA_TLS_KEY", ""),
		StorageType:     strings.ToLower(envOr("ATHENS_STORAGE_TYPE", "TOGA_STORAGE_TYPE", "disk")),
		GoBinary:        envOr("GO_BINARY_PATH", "TOGA_GO_BINARY", "go"),
		GoModCache:      envOr("GOMODCACHE", "TOGA_GOMODCACHE", ""),
		GoProxy:         os.Getenv("GOPROXY"),
		GoPrivate:       os.Getenv("GOPRIVATE"),
		GoNoProxy:       os.Getenv("GONOPROXY"),
		GoSumDB:         os.Getenv("GOSUMDB"),
		GoNoSumDB:       os.Getenv("GONOSUMDB"),
		Timeout:         envDuration("ATHENS_TIMEOUT", "TOGA_TIMEOUT", 300*time.Second),
		ShutdownTimeout: envDuration("ATHENS_SHUTDOWN_TIMEOUT", "TOGA_SHUTDOWN_TIMEOUT", 30*time.Second),
		LogLevel:        envOr("ATHENS_LOG_LEVEL", "TOGA_LOG_LEVEL", "info"),
		PathPrefix:      envOr("ATHENS_PATH_PREFIX", "TOGA_PATH_PREFIX", ""),
		BasicAuthUser:   envOr("BASIC_AUTH_USER", "TOGA_BASIC_AUTH_USER", ""),
		BasicAuthPass:   envOr("BASIC_AUTH_PASS", "TOGA_BASIC_AUTH_PASS", ""),
		NetworkMode:     envOr("ATHENS_NETWORK_MODE", "TOGA_NETWORK_MODE", "fallback"),
	}

	// Disk
	cfg.Disk = DiskConfig{
		RootPath: envOr("ATHENS_DISK_STORAGE_ROOT", "TOGA_DISK_ROOT", fmt.Sprintf("%s/toga-storage", os.TempDir())),
	}

	// S3
	cfg.S3 = S3Config{
		Region:         envOr("AWS_REGION", "", ""),
		Key:            envOr("AWS_ACCESS_KEY_ID", "", ""),
		Secret:         envOr("AWS_SECRET_ACCESS_KEY", "", ""),
		Token:          envOr("AWS_SESSION_TOKEN", "", ""),
		Bucket:         envOr("ATHENS_S3_BUCKET_NAME", "TOGA_S3_BUCKET", ""),
		Endpoint:       envOr("AWS_ENDPOINT", "TOGA_S3_ENDPOINT", ""),
		ForcePathStyle: envBool("AWS_FORCE_PATH_STYLE", "TOGA_S3_FORCE_PATH_STYLE"),
	}

	// Minio
	cfg.Minio = MinioConfig{
		Endpoint:  envOr("ATHENS_MINIO_ENDPOINT", "TOGA_MINIO_ENDPOINT", ""),
		Key:       envOr("ATHENS_MINIO_ACCESS_KEY_ID", "TOGA_MINIO_KEY", ""),
		Secret:    envOr("ATHENS_MINIO_SECRET_ACCESS_KEY", "TOGA_MINIO_SECRET", ""),
		Bucket:    envOr("ATHENS_MINIO_BUCKET_NAME", "TOGA_MINIO_BUCKET", ""),
		Region:    envOr("ATHENS_MINIO_REGION", "TOGA_MINIO_REGION", ""),
		EnableSSL: envBool("ATHENS_MINIO_USE_SSL", "TOGA_MINIO_SSL"),
	}

	// GCS
	cfg.GCS = GCSConfig{
		Bucket:          envOr("ATHENS_GCP_BUCKET", "TOGA_GCS_BUCKET", ""),
		ProjectID:       envOr("ATHENS_GCP_PROJECT_ID", "TOGA_GCS_PROJECT", ""),
		CredentialsFile: envOr("ATHENS_GCP_CREDENTIALS_FILE", "TOGA_GCS_CREDENTIALS_FILE", ""),
	}

	// Azure Blob
	cfg.AzureBlob = AzureBlobConfig{
		AccountName:   envOr("ATHENS_AZURE_ACCOUNT_NAME", "TOGA_AZURE_ACCOUNT", ""),
		AccountKey:    envOr("ATHENS_AZURE_ACCOUNT_KEY", "TOGA_AZURE_KEY", ""),
		ContainerName: envOr("ATHENS_AZURE_CONTAINER_NAME", "TOGA_AZURE_CONTAINER", ""),
	}

	// Sum DBs
	if s := envOr("ATHENS_SUM_DBS", "TOGA_SUM_DBS", ""); s != "" {
		cfg.ProxiedSumDBs = strings.Split(s, ",")
	}

	return cfg
}

func envOr(athensKey, togaKey, fallback string) string {
	if v := os.Getenv(athensKey); v != "" {
		return v
	}
	if togaKey != "" {
		if v := os.Getenv(togaKey); v != "" {
			return v
		}
	}
	return fallback
}

func envBool(athensKey, togaKey string) bool {
	v := envOr(athensKey, togaKey, "false")
	b, _ := strconv.ParseBool(v)
	return b
}

func envDuration(athensKey, togaKey string, fallback time.Duration) time.Duration {
	v := envOr(athensKey, togaKey, "")
	if v == "" {
		return fallback
	}
	// Try as seconds first (Athens compat)
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	return fallback
}
