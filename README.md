<p align="center">
  <img src="logo.svg" width="200" alt="Toga">
</p>

<h1 align="center">Toga</h1>

<p align="center">
  <em>A modern Go module proxy — drop-in replacement for Athens</em>
</p>

<p align="center">
  <img src="docs/demo.gif" alt="Toga Demo">
</p>

<p align="center">
  <a href="https://github.com/taigrr/toga/actions"><img src="https://github.com/taigrr/toga/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://goreportcard.com/report/github.com/taigrr/toga"><img src="https://goreportcard.com/badge/github.com/taigrr/toga" alt="Go Report Card"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-0BSD-blue" alt="License"></a>
</p>

---

Toga is a Go module proxy powered by [goproxy](https://github.com/goproxy/goproxy). It correctly handles vanity imports, `gopkg.in` redirects, and the full `GOPROXY` protocol by delegating module fetching to goproxy's `GoFetcher`, which uses the Go toolchain directly.

Toga maintains Athens-compatible storage layouts — point it at your existing S3/GCS/MinIO/Azure bucket and it just works.

## Why Toga?

Athens has a [long-standing bug](https://github.com/gomods/athens/issues/2029) where vanity import URLs don't resolve correctly, and the project is no longer actively maintained. Toga fixes this while keeping full compatibility with your existing Athens storage.

## Features

- **Athens-compatible storage** — works with your existing module cache, no data migration
- **Vanity import support** — correctly resolves vanity URLs and `gopkg.in` redirects
- **5 storage backends** — Disk, S3, MinIO, GCS, Azure Blob
- **Web UI** — browse cached modules with syntax-highlighted `go.mod` previews
- **Health endpoints** — `/healthz` and `/readyz` for orchestration
- **TLS & basic auth** — production-ready out of the box
- **Network modes** — `fallback` (default), `strict` (cache-only misses error), `offline` (no upstream)
- **Single binary** — no dependencies, no database

## Quick Start

### Binary

```bash
# Install
go install github.com/taigrr/toga/cmd/toga@latest

# Run with disk storage (default)
toga

# Point Go at it
go env -w GOPROXY=http://localhost:6060,direct
```

### Docker

```bash
docker run -p 6060:6060 ghcr.io/taigrr/toga
```

### With S3

```bash
TOGA_STORAGE_TYPE=s3 \
TOGA_S3_REGION=us-east-1 \
TOGA_S3_BUCKET=my-go-modules \
toga
```

## Storage Backends

| Backend    | `TOGA_STORAGE_TYPE` | Athens Compatible |
| ---------- | ------------------- | :---------------: |
| Disk       | `disk`              |        Yes        |
| S3         | `s3`                |        Yes        |
| MinIO      | `minio`             |        Yes        |
| GCS        | `gcs`               |        Yes        |
| Azure Blob | `azureblob`         |        Yes        |

## Configuration

Toga is configured with environment variables (prefix `TOGA_`) or a config file (`toga.toml`, `toga.yaml`, `toga.json`).

### Server

| Variable                | Default    | Description                                  |
| ----------------------- | ---------- | -------------------------------------------- |
| `TOGA_PORT`             | `:6060`    | Listen address                               |
| `TOGA_UNIX_SOCKET`      |            | Unix socket path (overrides port)            |
| `TOGA_TIMEOUT`          | `300s`     | Request timeout                              |
| `TOGA_SHUTDOWN_TIMEOUT` | `30s`      | Graceful shutdown timeout                    |
| `TOGA_PATH_PREFIX`      |            | URL path prefix                              |
| `TOGA_LOG_LEVEL`        | `info`     | Log level (`debug`, `info`, `warn`, `error`) |
| `TOGA_NETWORK_MODE`     | `fallback` | `fallback`, `strict`, or `offline`           |
| `TOGA_GO_BINARY`        | `go`       | Path to Go binary                            |
| `TOGA_SUM_DBS`          |            | Comma-separated sum DBs to proxy             |

### TLS & Auth

| Variable               | Description          |
| ---------------------- | -------------------- |
| `TOGA_TLS_CERT`        | TLS certificate file |
| `TOGA_TLS_KEY`         | TLS key file         |
| `TOGA_BASIC_AUTH_USER` | Basic auth username  |
| `TOGA_BASIC_AUTH_PASS` | Basic auth password  |

### Storage: Disk

| Variable              | Default             | Description       |
| --------------------- | ------------------- | ----------------- |
| `TOGA_DISK_ROOT_PATH` | `/tmp/toga-storage` | Storage directory |

### Storage: S3

| Variable                   | Description                 |
| -------------------------- | --------------------------- |
| `TOGA_S3_REGION`           | AWS region                  |
| `TOGA_S3_BUCKET`           | Bucket name                 |
| `TOGA_S3_KEY`              | Access key ID               |
| `TOGA_S3_SECRET`           | Secret access key           |
| `TOGA_S3_TOKEN`            | Session token               |
| `TOGA_S3_ENDPOINT`         | S3-compatible endpoint      |
| `TOGA_S3_FORCE_PATH_STYLE` | Path-style access (`false`) |

### Storage: MinIO

| Variable                | Description          |
| ----------------------- | -------------------- |
| `TOGA_MINIO_ENDPOINT`   | MinIO endpoint       |
| `TOGA_MINIO_KEY`        | Access key           |
| `TOGA_MINIO_SECRET`     | Secret key           |
| `TOGA_MINIO_BUCKET`     | Bucket name          |
| `TOGA_MINIO_REGION`     | Region               |
| `TOGA_MINIO_ENABLE_SSL` | Enable SSL (`false`) |

### Storage: GCS

| Variable                    | Description               |
| --------------------------- | ------------------------- |
| `TOGA_GCS_BUCKET`           | Bucket name               |
| `TOGA_GCS_PROJECT_ID`       | GCP project ID            |
| `TOGA_GCS_CREDENTIALS_FILE` | Service account JSON path |

### Storage: Azure Blob

| Variable                        | Description          |
| ------------------------------- | -------------------- |
| `TOGA_AZUREBLOB_ACCOUNT_NAME`   | Storage account name |
| `TOGA_AZUREBLOB_ACCOUNT_KEY`    | Storage account key  |
| `TOGA_AZUREBLOB_CONTAINER_NAME` | Container name       |

## API Endpoints

| Endpoint                          | Description           |
| --------------------------------- | --------------------- |
| `GET /healthz`                    | Liveness probe        |
| `GET /readyz`                     | Readiness probe       |
| `GET /<module>/@v/list`           | List versions         |
| `GET /<module>/@v/<version>.info` | Version metadata      |
| `GET /<module>/@v/<version>.mod`  | `go.mod` file         |
| `GET /<module>/@v/<version>.zip`  | Module source archive |

## Running as a Service

### Linux (systemd user service)

Create `~/.config/systemd/user/toga.service`:

```ini
[Unit]
Description=Toga Go Module Proxy
After=network.target

[Service]
Type=simple
ExecStart=%h/go/bin/toga
Environment="TOGA_PORT=:6060"
Environment="TOGA_STORAGE_TYPE=disk"
Environment="TOGA_DISK_ROOT_PATH=%h/.cache/toga"
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
```

```bash
# Reload, enable, start
systemctl --user daemon-reload
systemctl --user enable toga
systemctl --user start toga

# Enable lingering (keeps service running after logout)
loginctl enable-linger $USER

# Check status
systemctl --user status toga
journalctl --user -u toga -f
```

### macOS (launchd)

Create `~/Library/LaunchAgents/com.taigrr.toga.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.taigrr.toga</string>
    <key>ProgramArguments</key>
    <array>
        <string>/path/to/toga</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>TOGA_PORT</key>
        <string>:6060</string>
        <key>TOGA_STORAGE_TYPE</key>
        <string>disk</string>
        <key>TOGA_DISK_ROOT_PATH</key>
        <string>/path/to/storage</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/toga.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/toga.err</string>
</dict>
</plist>
```

```bash
# Load and start
launchctl load ~/Library/LaunchAgents/com.taigrr.toga.plist

# Unload
launchctl unload ~/Library/LaunchAgents/com.taigrr.toga.plist

# Check status
launchctl list | grep toga
```

## Migrating from Athens

Toga reads Athens storage layouts directly — no data migration required. See the full **[Migration Guide](docs/MIGRATION_GUIDE.md)** for step-by-step instructions.

The short version:

1. Keep your existing storage bucket
2. Rename `ATHENS_*` env vars to `TOGA_*` equivalents
3. Swap the container image

## License

[0BSD](LICENSE)
