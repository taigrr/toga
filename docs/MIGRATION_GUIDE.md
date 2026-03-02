# Migrating from Athens to Toga

Toga is a drop-in replacement for [Athens](https://github.com/gomods/athens). It reads the same storage layout, so your existing module cache works without any data migration.

## Overview

1. Keep your existing storage bucket — no data changes needed
2. Rename environment variables from `ATHENS_` to `TOGA_` equivalents
3. Swap the container image

## Step 1: Keep Your Storage Bucket

Toga is compatible with Athens storage layouts for all supported backends (S3, GCS, MinIO, Azure Blob, disk). Point Toga at the same bucket or directory and it reads everything as-is.

## Step 2: Rename Environment Variables

Athens environment variables are **not** supported. Replace them with the `TOGA_` equivalents below.

### General

| Athens | Toga |
|--------|------|
| `ATHENS_PORT` | `TOGA_PORT` |
| `ATHENS_STORAGE_TYPE` | `TOGA_STORAGE_TYPE` |
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

### Disk

| Athens | Toga |
|--------|------|
| `ATHENS_DISK_STORAGE_ROOT` | `TOGA_DISK_ROOT_PATH` |

### S3

| Athens | Toga |
|--------|------|
| `ATHENS_S3_BUCKET_NAME` | `TOGA_S3_BUCKET` |
| `AWS_REGION` | `TOGA_S3_REGION` |
| `AWS_ACCESS_KEY_ID` | `TOGA_S3_KEY` |
| `AWS_SECRET_ACCESS_KEY` | `TOGA_S3_SECRET` |
| `AWS_SESSION_TOKEN` | `TOGA_S3_TOKEN` |
| `AWS_ENDPOINT` | `TOGA_S3_ENDPOINT` |
| `AWS_FORCE_PATH_STYLE` | `TOGA_S3_FORCE_PATH_STYLE` |

### MinIO

| Athens | Toga |
|--------|------|
| `ATHENS_MINIO_ENDPOINT` | `TOGA_MINIO_ENDPOINT` |
| `ATHENS_MINIO_ACCESS_KEY_ID` | `TOGA_MINIO_KEY` |
| `ATHENS_MINIO_SECRET_ACCESS_KEY` | `TOGA_MINIO_SECRET` |
| `ATHENS_MINIO_BUCKET_NAME` | `TOGA_MINIO_BUCKET` |
| `ATHENS_MINIO_REGION` | `TOGA_MINIO_REGION` |
| `ATHENS_MINIO_USE_SSL` | `TOGA_MINIO_ENABLE_SSL` |

### GCS

| Athens | Toga |
|--------|------|
| `ATHENS_GCP_BUCKET` | `TOGA_GCS_BUCKET` |
| `ATHENS_GCP_PROJECT_ID` | `TOGA_GCS_PROJECT_ID` |
| `ATHENS_GCP_CREDENTIALS_FILE` | `TOGA_GCS_CREDENTIALS_FILE` |

### Azure Blob

| Athens | Toga |
|--------|------|
| `ATHENS_AZURE_ACCOUNT_NAME` | `TOGA_AZUREBLOB_ACCOUNT_NAME` |
| `ATHENS_AZURE_ACCOUNT_KEY` | `TOGA_AZUREBLOB_ACCOUNT_KEY` |
| `ATHENS_AZURE_CONTAINER_NAME` | `TOGA_AZUREBLOB_CONTAINER_NAME` |

## Step 3: Swap the Container Image

```bash
# Before
docker run gomods/athens:latest

# After
docker run ghcr.io/taigrr/toga:latest
```

## Verifying the Migration

After switching, confirm everything works:

```bash
# Health check
curl http://localhost:3000/healthz

# Readiness check
curl http://localhost:3000/readyz

# Fetch a module
GOPROXY=http://localhost:3000 go get github.com/example/module@latest
```

## Rollback

Since Toga doesn't modify the storage layout, you can switch back to Athens at any time by reverting the container image and environment variables.
