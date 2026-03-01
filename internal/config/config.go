// Package config loads toga configuration using JETY (JSON, ENV, TOML, YAML).
// Environment variables use the TOGA_ prefix. Athens-compatible env vars are
// also supported as aliases (loaded via SetDefault so JETY env takes precedence).
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/taigrr/jety"
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

const envPrefix = "TOGA_"

// Init sets up JETY defaults and env prefix. Call before Load.
// If configFile is non-empty, that file is used; otherwise JETY searches
// for toga.toml / toga.yaml / toga.json in standard paths.
func Init(configFile string) error {
	jety.SetEnvPrefix(envPrefix)

	// Set defaults (including Athens-compatible env var fallbacks).
	setDefaults()

	if configFile != "" {
		jety.SetConfigFile(configFile)
	} else {
		jety.SetConfigName("toga")
		jety.SetConfigType("toml")
	}

	if err := jety.ReadInConfig(); err != nil {
		// Config file is optional — only fail if one was explicitly requested.
		if configFile != "" {
			return fmt.Errorf("read config %s: %w", configFile, err)
		}
	}

	return nil
}

func setDefaults() {
	jety.SetDefault("port", athensEnvOr("ATHENS_PORT", ":3000"))
	jety.SetDefault("unix_socket", os.Getenv("ATHENS_UNIX_SOCKET"))
	jety.SetDefault("tls_cert", os.Getenv("ATHENS_TLSCERT_FILE"))
	jety.SetDefault("tls_key", os.Getenv("ATHENS_TLSKEY_FILE"))
	jety.SetDefault("storage_type", athensEnvOr("ATHENS_STORAGE_TYPE", "disk"))
	jety.SetDefault("go_binary", athensEnvOr("GO_BINARY_PATH", "go"))
	jety.SetDefault("go_mod_cache", os.Getenv("GOMODCACHE"))
	jety.SetDefault("go_proxy", os.Getenv("GOPROXY"))
	jety.SetDefault("go_private", os.Getenv("GOPRIVATE"))
	jety.SetDefault("go_noproxy", os.Getenv("GONOPROXY"))
	jety.SetDefault("go_sumdb", os.Getenv("GOSUMDB"))
	jety.SetDefault("go_nosumdb", os.Getenv("GONOSUMDB"))
	jety.SetDefault("timeout", athensEnvOr("ATHENS_TIMEOUT", "300s"))
	jety.SetDefault("shutdown_timeout", athensEnvOr("ATHENS_SHUTDOWN_TIMEOUT", "30s"))
	jety.SetDefault("log_level", athensEnvOr("ATHENS_LOG_LEVEL", "info"))
	jety.SetDefault("path_prefix", os.Getenv("ATHENS_PATH_PREFIX"))
	jety.SetDefault("basic_auth_user", os.Getenv("BASIC_AUTH_USER"))
	jety.SetDefault("basic_auth_pass", os.Getenv("BASIC_AUTH_PASS"))
	jety.SetDefault("network_mode", athensEnvOr("ATHENS_NETWORK_MODE", "fallback"))
	jety.SetDefault("sum_dbs", os.Getenv("ATHENS_SUM_DBS"))

	// Disk
	jety.SetDefault("disk.root_path", athensEnvOr("ATHENS_DISK_STORAGE_ROOT", fmt.Sprintf("%s/toga-storage", os.TempDir())))

	// S3
	jety.SetDefault("s3.region", os.Getenv("AWS_REGION"))
	jety.SetDefault("s3.key", os.Getenv("AWS_ACCESS_KEY_ID"))
	jety.SetDefault("s3.secret", os.Getenv("AWS_SECRET_ACCESS_KEY"))
	jety.SetDefault("s3.token", os.Getenv("AWS_SESSION_TOKEN"))
	jety.SetDefault("s3.bucket", os.Getenv("ATHENS_S3_BUCKET_NAME"))
	jety.SetDefault("s3.endpoint", os.Getenv("AWS_ENDPOINT"))
	jety.SetDefault("s3.force_path_style", os.Getenv("AWS_FORCE_PATH_STYLE"))

	// MinIO
	jety.SetDefault("minio.endpoint", os.Getenv("ATHENS_MINIO_ENDPOINT"))
	jety.SetDefault("minio.key", os.Getenv("ATHENS_MINIO_ACCESS_KEY_ID"))
	jety.SetDefault("minio.secret", os.Getenv("ATHENS_MINIO_SECRET_ACCESS_KEY"))
	jety.SetDefault("minio.bucket", os.Getenv("ATHENS_MINIO_BUCKET_NAME"))
	jety.SetDefault("minio.region", os.Getenv("ATHENS_MINIO_REGION"))
	jety.SetDefault("minio.enable_ssl", os.Getenv("ATHENS_MINIO_USE_SSL"))

	// GCS
	jety.SetDefault("gcs.bucket", os.Getenv("ATHENS_GCP_BUCKET"))
	jety.SetDefault("gcs.project_id", os.Getenv("ATHENS_GCP_PROJECT_ID"))
	jety.SetDefault("gcs.credentials_file", os.Getenv("ATHENS_GCP_CREDENTIALS_FILE"))

	// Azure Blob
	jety.SetDefault("azureblob.account_name", os.Getenv("ATHENS_AZURE_ACCOUNT_NAME"))
	jety.SetDefault("azureblob.account_key", os.Getenv("ATHENS_AZURE_ACCOUNT_KEY"))
	jety.SetDefault("azureblob.container_name", os.Getenv("ATHENS_AZURE_CONTAINER_NAME"))
}

// athensEnvOr returns the Athens env var value if set, otherwise the fallback.
func athensEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Load builds a Config from JETY state. Call Init first.
func Load() *Config {
	cfg := &Config{
		Port:            jety.GetString("port"),
		UnixSocket:      jety.GetString("unix_socket"),
		TLSCertFile:     jety.GetString("tls_cert"),
		TLSKeyFile:      jety.GetString("tls_key"),
		StorageType:     strings.ToLower(jety.GetString("storage_type")),
		GoBinary:        jety.GetString("go_binary"),
		GoModCache:      jety.GetString("go_mod_cache"),
		GoProxy:         jety.GetString("go_proxy"),
		GoPrivate:       jety.GetString("go_private"),
		GoNoProxy:       jety.GetString("go_noproxy"),
		GoSumDB:         jety.GetString("go_sumdb"),
		GoNoSumDB:       jety.GetString("go_nosumdb"),
		Timeout:         jety.GetDuration("timeout"),
		ShutdownTimeout: jety.GetDuration("shutdown_timeout"),
		LogLevel:        jety.GetString("log_level"),
		PathPrefix:      jety.GetString("path_prefix"),
		BasicAuthUser:   jety.GetString("basic_auth_user"),
		BasicAuthPass:   jety.GetString("basic_auth_pass"),
		NetworkMode:     jety.GetString("network_mode"),
	}

	// Sum DBs
	if s := jety.GetString("sum_dbs"); s != "" {
		cfg.ProxiedSumDBs = strings.Split(s, ",")
	}

	// Disk
	cfg.Disk = DiskConfig{
		RootPath: jety.GetString("disk.root_path"),
	}

	// S3
	cfg.S3 = S3Config{
		Region:         jety.GetString("s3.region"),
		Key:            jety.GetString("s3.key"),
		Secret:         jety.GetString("s3.secret"),
		Token:          jety.GetString("s3.token"),
		Bucket:         jety.GetString("s3.bucket"),
		Endpoint:       jety.GetString("s3.endpoint"),
		ForcePathStyle: jety.GetBool("s3.force_path_style"),
	}

	// MinIO
	cfg.Minio = MinioConfig{
		Endpoint:  jety.GetString("minio.endpoint"),
		Key:       jety.GetString("minio.key"),
		Secret:    jety.GetString("minio.secret"),
		Bucket:    jety.GetString("minio.bucket"),
		Region:    jety.GetString("minio.region"),
		EnableSSL: jety.GetBool("minio.enable_ssl"),
	}

	// GCS
	cfg.GCS = GCSConfig{
		Bucket:          jety.GetString("gcs.bucket"),
		ProjectID:       jety.GetString("gcs.project_id"),
		CredentialsFile: jety.GetString("gcs.credentials_file"),
	}

	// Azure Blob
	cfg.AzureBlob = AzureBlobConfig{
		AccountName:   jety.GetString("azureblob.account_name"),
		AccountKey:    jety.GetString("azureblob.account_key"),
		ContainerName: jety.GetString("azureblob.container_name"),
	}

	return cfg
}
