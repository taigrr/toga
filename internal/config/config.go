// Package config loads toga configuration using JETY (JSON, ENV, TOML, YAML).
// Environment variables use the TOGA_ prefix.
package config

import (
	"fmt"
	"os"
	"path/filepath"
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

	// Storage backend: memory, disk, s3, minio, gcs, azureblob
	StorageType string

	// Go environment
	GoBinary        string
	GoBinaryEnvVars []string
	GoModCache      string
	GoProxy         string
	GoPrivate       string
	GoNoProxy       string
	GoSumDB         string
	GoNoSumDB       string

	// Timeouts
	Timeout         time.Duration
	ShutdownTimeout time.Duration

	// Logging
	LogLevel  string
	LogFormat string

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

	// Concurrency limits
	GoGetWorkers    int
	ProtocolWorkers int

	// Auth for private repos
	NETRCPath   string
	GithubToken string

	// Homepage and robots
	HomeTemplatePath string
	RobotsFile       string

	// Observability
	EnablePprof bool
	PprofPort   string

	// Tracing (OpenTelemetry)
	TraceExporter   string // "jaeger", "otlp", or "" (disabled)
	TraceEndpoint   string
	TraceSampleRate float64
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

// cm is the package-level config manager, initialized in Init.
var cm *jety.ConfigManager

// Init sets up JETY defaults and env prefix. Call before Load.
// If configFile is non-empty, that file is used; otherwise JETY searches
// for toga.toml / toga.yaml / toga.json in standard paths.
func Init(configFile string) error {
	cm = jety.NewConfigManager().WithEnvPrefix(envPrefix)

	setDefaults()

	if configFile != "" {
		cm.SetConfigFile(configFile)
		if ct := configTypeFromExt(configFile); ct != "" {
			if err := cm.SetConfigType(ct); err != nil {
				return fmt.Errorf("set config type: %w", err)
			}
		}
	} else {
		cm.SetConfigName("toga")
		cm.SetConfigDir(".")
		if err := cm.SetConfigType("toml"); err != nil {
			return fmt.Errorf("set config type: %w", err)
		}
	}

	if err := cm.ReadInConfig(); err != nil {
		// Config file is optional — only fail if one was explicitly requested.
		if configFile != "" {
			return fmt.Errorf("read config %s: %w", configFile, err)
		}
	}

	return nil
}

func setDefaults() {
	cm.SetDefault("port", ":3000")
	cm.SetDefault("unix_socket", "")
	cm.SetDefault("tls_cert", "")
	cm.SetDefault("tls_key", "")
	cm.SetDefault("storage_type", "disk")
	cm.SetDefault("go_binary", "go")
	cm.SetDefault("go_binary_env_vars", "")
	cm.SetDefault("go_mod_cache", "")
	cm.SetDefault("go_proxy", "")
	cm.SetDefault("go_private", "")
	cm.SetDefault("go_noproxy", "")
	cm.SetDefault("go_sumdb", "")
	cm.SetDefault("go_nosumdb", "")
	cm.SetDefault("timeout", "300s")
	cm.SetDefault("shutdown_timeout", "30s")
	cm.SetDefault("log_level", "info")
	cm.SetDefault("log_format", "json")
	cm.SetDefault("path_prefix", "")
	cm.SetDefault("basic_auth_user", "")
	cm.SetDefault("basic_auth_pass", "")
	cm.SetDefault("network_mode", "fallback")
	cm.SetDefault("sum_dbs", "")

	// Concurrency
	cm.SetDefault("goget_workers", "10")
	cm.SetDefault("protocol_workers", "30")

	// Auth
	cm.SetDefault("netrc_path", "")
	cm.SetDefault("github_token", "")

	// Homepage and robots
	cm.SetDefault("home_template_path", "")
	cm.SetDefault("robots_file", "")

	// Observability
	cm.SetDefault("enable_pprof", "false")
	cm.SetDefault("pprof_port", ":3001")

	// Tracing
	cm.SetDefault("trace_exporter", "")
	cm.SetDefault("trace_endpoint", "")
	cm.SetDefault("trace_sample_rate", "1.0")

	// Flat keys for env var compatibility (TOGA_DISK_ROOT_PATH, not TOGA_DISK.ROOT_PATH).
	cm.SetDefault("disk_root_path", fmt.Sprintf("%s/toga-storage", os.TempDir()))
	cm.SetDefault("s3_region", "")
	cm.SetDefault("s3_key", "")
	cm.SetDefault("s3_secret", "")
	cm.SetDefault("s3_token", "")
	cm.SetDefault("s3_bucket", "")
	cm.SetDefault("s3_endpoint", "")
	cm.SetDefault("s3_force_path_style", "false")
	cm.SetDefault("minio_endpoint", "")
	cm.SetDefault("minio_key", "")
	cm.SetDefault("minio_secret", "")
	cm.SetDefault("minio_bucket", "")
	cm.SetDefault("minio_region", "")
	cm.SetDefault("minio_enable_ssl", "false")
	cm.SetDefault("gcs_bucket", "")
	cm.SetDefault("gcs_project_id", "")
	cm.SetDefault("gcs_credentials_file", "")
	cm.SetDefault("azureblob_account_name", "")
	cm.SetDefault("azureblob_account_key", "")
	cm.SetDefault("azureblob_container_name", "")
}

// configTypeFromExt returns "toml", "yaml", or "json" based on the file extension.
func configTypeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	default:
		return ""
	}
}

// Load builds a Config from JETY state. Call Init first.
// Panics if Init has not been called.
func Load() *Config {
	if cm == nil {
		panic("config: Load called before Init")
	}
	cfg := &Config{
		Port:             cm.GetString("port"),
		UnixSocket:       cm.GetString("unix_socket"),
		TLSCertFile:      cm.GetString("tls_cert"),
		TLSKeyFile:       cm.GetString("tls_key"),
		StorageType:      strings.ToLower(cm.GetString("storage_type")),
		GoBinary:         cm.GetString("go_binary"),
		GoModCache:       cm.GetString("go_mod_cache"),
		GoProxy:          cm.GetString("go_proxy"),
		GoPrivate:        cm.GetString("go_private"),
		GoNoProxy:        cm.GetString("go_noproxy"),
		GoSumDB:          cm.GetString("go_sumdb"),
		GoNoSumDB:        cm.GetString("go_nosumdb"),
		Timeout:          cm.GetDuration("timeout"),
		ShutdownTimeout:  cm.GetDuration("shutdown_timeout"),
		LogLevel:         cm.GetString("log_level"),
		LogFormat:        strings.ToLower(cm.GetString("log_format")),
		PathPrefix:       cm.GetString("path_prefix"),
		BasicAuthUser:    cm.GetString("basic_auth_user"),
		BasicAuthPass:    cm.GetString("basic_auth_pass"),
		NetworkMode:      strings.ToLower(cm.GetString("network_mode")),
		GoGetWorkers:     cm.GetInt("goget_workers"),
		ProtocolWorkers:  cm.GetInt("protocol_workers"),
		NETRCPath:        cm.GetString("netrc_path"),
		GithubToken:      cm.GetString("github_token"),
		HomeTemplatePath: cm.GetString("home_template_path"),
		RobotsFile:       cm.GetString("robots_file"),
		EnablePprof:      cm.GetBool("enable_pprof"),
		PprofPort:        cm.GetString("pprof_port"),
		TraceExporter:    strings.ToLower(cm.GetString("trace_exporter")),
		TraceEndpoint:    cm.GetString("trace_endpoint"),
		TraceSampleRate: func() float64 {
			// jety doesn't have GetFloat64, parse manually
			s := cm.GetString("trace_sample_rate")
			var f float64
			if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
				return 1.0
			}
			return f
		}(),
	}

	// Go binary env vars (semicolon-separated in env, array in TOML)
	if s := cm.GetString("go_binary_env_vars"); s != "" {
		for _, part := range strings.Split(s, ";") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				cfg.GoBinaryEnvVars = append(cfg.GoBinaryEnvVars, trimmed)
			}
		}
	}

	// Sum DBs
	if s := cm.GetString("sum_dbs"); s != "" {
		cfg.ProxiedSumDBs = strings.Split(s, ",")
	}

	cfg.Disk = DiskConfig{
		RootPath: cm.GetString("disk_root_path"),
	}

	cfg.S3 = S3Config{
		Region:         cm.GetString("s3_region"),
		Key:            cm.GetString("s3_key"),
		Secret:         cm.GetString("s3_secret"),
		Token:          cm.GetString("s3_token"),
		Bucket:         cm.GetString("s3_bucket"),
		Endpoint:       cm.GetString("s3_endpoint"),
		ForcePathStyle: cm.GetBool("s3_force_path_style"),
	}

	cfg.Minio = MinioConfig{
		Endpoint:  cm.GetString("minio_endpoint"),
		Key:       cm.GetString("minio_key"),
		Secret:    cm.GetString("minio_secret"),
		Bucket:    cm.GetString("minio_bucket"),
		Region:    cm.GetString("minio_region"),
		EnableSSL: cm.GetBool("minio_enable_ssl"),
	}

	cfg.GCS = GCSConfig{
		Bucket:          cm.GetString("gcs_bucket"),
		ProjectID:       cm.GetString("gcs_project_id"),
		CredentialsFile: cm.GetString("gcs_credentials_file"),
	}

	cfg.AzureBlob = AzureBlobConfig{
		AccountName:   cm.GetString("azureblob_account_name"),
		AccountKey:    cm.GetString("azureblob_account_key"),
		ContainerName: cm.GetString("azureblob_container_name"),
	}

	return cfg
}
