# Toga

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

# S3 storage (Athens-compatible env vars)
ATHENS_STORAGE_TYPE=s3 \
AWS_REGION=us-east-1 \
ATHENS_S3_BUCKET_NAME=my-go-modules \
toga

# Then point your Go toolchain at it
go env -w GOPROXY=http://localhost:3000,direct
```

## Configuration

Toga accepts both Athens (`ATHENS_*`) and native (`TOGA_*`) environment variables. Athens env vars take precedence for drop-in compatibility.

| Athens Env Var | Toga Env Var | Default | Description |
|----------------|-------------|---------|-------------|
| `ATHENS_PORT` | `TOGA_PORT` | `:3000` | Listen port |
| `ATHENS_UNIX_SOCKET` | `TOGA_UNIX_SOCKET` | | Unix socket path |
| `ATHENS_STORAGE_TYPE` | `TOGA_STORAGE_TYPE` | `disk` | Storage backend |
| `ATHENS_DISK_STORAGE_ROOT` | `TOGA_DISK_ROOT` | `/tmp/toga-storage` | Disk storage path |
| `ATHENS_S3_BUCKET_NAME` | `TOGA_S3_BUCKET` | | S3 bucket name |
| `AWS_REGION` | | | S3 region |
| `AWS_ENDPOINT` | `TOGA_S3_ENDPOINT` | | S3-compatible endpoint |
| `AWS_FORCE_PATH_STYLE` | `TOGA_S3_FORCE_PATH_STYLE` | `false` | S3 path-style access |
| `ATHENS_LOG_LEVEL` | `TOGA_LOG_LEVEL` | `info` | Log level (debug/info/warn/error) |
| `BASIC_AUTH_USER` | `TOGA_BASIC_AUTH_USER` | | Basic auth username |
| `BASIC_AUTH_PASS` | `TOGA_BASIC_AUTH_PASS` | | Basic auth password |
| `ATHENS_NETWORK_MODE` | `TOGA_NETWORK_MODE` | `fallback` | Network mode |
| `ATHENS_TLSCERT_FILE` | `TOGA_TLS_CERT` | | TLS certificate file |
| `ATHENS_TLSKEY_FILE` | `TOGA_TLS_KEY` | | TLS key file |

See [Athens docs](https://docs.gomods.io/) for S3, MinIO, GCS, and Azure Blob configuration.

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

1. Keep your existing S3/GCS/MinIO bucket — the storage format is compatible
2. Replace Athens env vars with the same values (they're accepted as-is)
3. Swap the container image

## License

0BSD
