<p align="center">
  <img src="logo.svg" width="128" alt="Toga">
</p>

<h1 align="center">Toga</h1>

<p align="center">
  <em>A drop-in replacement for Athens — a Go module proxy powered by <a href="https://github.com/goproxy/goproxy">goproxy</a></em>
</p>

<p align="center">
  <a href="https://github.com/taigrr/toga/actions"><img src="https://github.com/taigrr/toga/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://goreportcard.com/report/github.com/taigrr/toga"><img src="https://goreportcard.com/badge/github.com/taigrr/toga" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-0BSD-blue" alt="License"></a>
</p>

A Go module proxy that is a drop-in replacement for [Athens](https://github.com/gomods/athens), powered by [goproxy](https://github.com/goproxy/goproxy).

Toga uses goproxy's battle-tested module fetcher (which correctly handles vanity imports, gopkg.in redirects, and the full GOPROXY protocol) while maintaining Athens-compatible storage layouts — meaning you can point Toga at your existing Athens S3/GCS/MinIO bucket and it just works.

## Why?

Athens has a [long-standing bug](https://github.com/gomods/athens/issues/2029) where vanity import URLs don't resolve correctly, and the project is no longer actively maintained. Toga solves this by delegating all module fetching to goproxy's `GoFetcher`, which uses the Go toolchain directly.

## Storage Backends

| Backend | Config | Athens Compatible |
|---------|--------|-------------------|
| Disk | `TOGA_STORAGE_TYPE=disk` | ✅ |
| S3 | `TOGA_STORAGE_TYPE=s3` | ✅ |
| MinIO | `TOGA_STORAGE_TYPE=minio` | ✅ |
| GCS | `TOGA_STORAGE_TYPE=gcs` | ✅ |
| Azure Blob | `TOGA_STORAGE_TYPE=azureblob` | ✅ |

## Quick Start

```bash
# Disk storage (default)
toga

# S3 storage
TOGA_STORAGE_TYPE=s3 \
TOGA_S3_REGION=us-east-1 \
TOGA_S3_BUCKET=my-go-modules \
toga

# Then point your Go toolchain at it
go env -w GOPROXY=http://localhost:3000,direct
```

## Configuration

All configuration uses the `TOGA_` environment variable prefix. You can also use a config file (`toga.toml`, `toga.yaml`, or `toga.json`).

| Env Var | Default | Description |
|---------|---------|-------------|
| `TOGA_PORT` | `:3000` | Listen port |
| `TOGA_UNIX_SOCKET` | | Unix socket path |
| `TOGA_STORAGE_TYPE` | `disk` | Storage backend |
| `TOGA_DISK_ROOT_PATH` | `/tmp/toga-storage` | Disk storage path |
| `TOGA_S3_REGION` | | S3 region |
| `TOGA_S3_KEY` | | AWS access key ID |
| `TOGA_S3_SECRET` | | AWS secret access key |
| `TOGA_S3_TOKEN` | | AWS session token |
| `TOGA_S3_BUCKET` | | S3 bucket name |
| `TOGA_S3_ENDPOINT` | | S3-compatible endpoint |
| `TOGA_S3_FORCE_PATH_STYLE` | `false` | S3 path-style access |
| `TOGA_MINIO_ENDPOINT` | | MinIO endpoint |
| `TOGA_MINIO_KEY` | | MinIO access key |
| `TOGA_MINIO_SECRET` | | MinIO secret key |
| `TOGA_MINIO_BUCKET` | | MinIO bucket name |
| `TOGA_MINIO_REGION` | | MinIO region |
| `TOGA_MINIO_ENABLE_SSL` | `false` | MinIO SSL |
| `TOGA_GCS_BUCKET` | | GCS bucket name |
| `TOGA_GCS_PROJECT_ID` | | GCP project ID |
| `TOGA_GCS_CREDENTIALS_FILE` | | GCP credentials file path |
| `TOGA_AZUREBLOB_ACCOUNT_NAME` | | Azure storage account |
| `TOGA_AZUREBLOB_ACCOUNT_KEY` | | Azure storage key |
| `TOGA_AZUREBLOB_CONTAINER_NAME` | | Azure container |
| `TOGA_LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |
| `TOGA_BASIC_AUTH_USER` | | Basic auth username |
| `TOGA_BASIC_AUTH_PASS` | | Basic auth password |
| `TOGA_NETWORK_MODE` | `fallback` | Network mode (strict/offline/fallback) |
| `TOGA_TLS_CERT` | | TLS certificate file |
| `TOGA_TLS_KEY` | | TLS key file |
| `TOGA_GO_BINARY` | `go` | Path to Go binary |
| `TOGA_TIMEOUT` | `300s` | Request timeout |
| `TOGA_SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |
| `TOGA_PATH_PREFIX` | | URL path prefix |
| `TOGA_SUM_DBS` | | Comma-separated sum DBs to proxy |

## Endpoints

- `GET /healthz` — Liveness probe
- `GET /readyz` — Readiness probe
- `GET /<module>/@v/list` — List versions
- `GET /<module>/@v/<version>.info` — Version info
- `GET /<module>/@v/<version>.mod` — Go module file
- `GET /<module>/@v/<version>.zip` — Module source zip

## Docker

```bash
docker run -p 3000:3000 ghcr.io/taigrr/toga
```

## Migration from Athens

Toga reads from the same storage layout as Athens, so your existing module cache works as-is. The only change is environment variables.

### 1. Keep your storage bucket

Point Toga at the same S3/GCS/MinIO/Azure bucket. No data migration needed.

### 2. Rename environment variables

Athens env vars are **not** supported. Replace them with `TOGA_` equivalents:

| Athens | Toga |
|--------|------|
| `ATHENS_PORT` | `TOGA_PORT` |
| `ATHENS_STORAGE_TYPE` | `TOGA_STORAGE_TYPE` |
| `ATHENS_DISK_STORAGE_ROOT` | `TOGA_DISK_ROOT_PATH` |
| `ATHENS_S3_BUCKET_NAME` | `TOGA_S3_BUCKET` |
| `AWS_REGION` | `TOGA_S3_REGION` |
| `AWS_ACCESS_KEY_ID` | `TOGA_S3_KEY` |
| `AWS_SECRET_ACCESS_KEY` | `TOGA_S3_SECRET` |
| `AWS_SESSION_TOKEN` | `TOGA_S3_TOKEN` |
| `AWS_ENDPOINT` | `TOGA_S3_ENDPOINT` |
| `AWS_FORCE_PATH_STYLE` | `TOGA_S3_FORCE_PATH_STYLE` |
| `ATHENS_MINIO_ENDPOINT` | `TOGA_MINIO_ENDPOINT` |
| `ATHENS_MINIO_ACCESS_KEY_ID` | `TOGA_MINIO_KEY` |
| `ATHENS_MINIO_SECRET_ACCESS_KEY` | `TOGA_MINIO_SECRET` |
| `ATHENS_MINIO_BUCKET_NAME` | `TOGA_MINIO_BUCKET` |
| `ATHENS_MINIO_REGION` | `TOGA_MINIO_REGION` |
| `ATHENS_MINIO_USE_SSL` | `TOGA_MINIO_ENABLE_SSL` |
| `ATHENS_GCP_BUCKET` | `TOGA_GCS_BUCKET` |
| `ATHENS_GCP_PROJECT_ID` | `TOGA_GCS_PROJECT_ID` |
| `ATHENS_GCP_CREDENTIALS_FILE` | `TOGA_GCS_CREDENTIALS_FILE` |
| `ATHENS_AZURE_ACCOUNT_NAME` | `TOGA_AZUREBLOB_ACCOUNT_NAME` |
| `ATHENS_AZURE_ACCOUNT_KEY` | `TOGA_AZUREBLOB_ACCOUNT_KEY` |
| `ATHENS_AZURE_CONTAINER_NAME` | `TOGA_AZUREBLOB_CONTAINER_NAME` |
| `ATHENS_LOG_LEVEL` | `TOGA_LOG_LEVEL` |
| `ATHENS_NETWORK_MODE` | `TOGA_NETWORK_MODE` |
| `ATHENS_TIMEOUT` | `TOGA_TIMEOUT` |
| `ATHENS_SHUTDOWN_TIMEOUT` | `TOGA_SHUTDOWN_TIMEOUT` |
| `ATHENS_PATH_PREFIX` | `TOGA_PATH_PREFIX` |
| `ATHENS_TLSCERT_FILE` | `TOGA_TLS_CERT` |
| `ATHENS_TLSKEY_FILE` | `TOGA_TLS_KEY` |
| `BASIC_AUTH_USER` | `TOGA_BASIC_AUTH_USER` |
| `BASIC_AUTH_PASS` | `TOGA_BASIC_AUTH_PASS` |
| `GO_BINARY_PATH` | `TOGA_GO_BINARY` |
| `ATHENS_SUM_DBS` | `TOGA_SUM_DBS` |

### 3. Swap the container image

```bash
# Before
docker run gomods/athens:latest

# After
docker run ghcr.io/taigrr/toga:latest
```

## License

0BSD
