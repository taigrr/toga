package main

import (
	"context"
	"fmt"

	"github.com/goproxy/goproxy"
	"github.com/taigrr/toga/internal/config"
	"github.com/taigrr/toga/internal/storage/azureblob"
	"github.com/taigrr/toga/internal/storage/disk"
	"github.com/taigrr/toga/internal/storage/gcs"
	"github.com/taigrr/toga/internal/storage/memory"
	miniocacher "github.com/taigrr/toga/internal/storage/minio"
	s3cacher "github.com/taigrr/toga/internal/storage/s3"
)

func newCacher(ctx context.Context, cfg *config.Config) (goproxy.Cacher, error) {
	if err := validateStorage(cfg); err != nil {
		return nil, err
	}

	switch cfg.StorageType {
	case "memory":
		return memory.New(), nil
	case "disk":
		return disk.New(disk.Config{RootPath: cfg.Disk.RootPath}), nil
	case "s3":
		return s3cacher.New(ctx, s3cacher.Config{
			Region:         cfg.S3.Region,
			Key:            cfg.S3.Key,
			Secret:         cfg.S3.Secret,
			Token:          cfg.S3.Token,
			Bucket:         cfg.S3.Bucket,
			Endpoint:       cfg.S3.Endpoint,
			ForcePathStyle: cfg.S3.ForcePathStyle,
		})
	case "minio":
		return miniocacher.New(ctx, miniocacher.Config{
			Endpoint:  cfg.Minio.Endpoint,
			Key:       cfg.Minio.Key,
			Secret:    cfg.Minio.Secret,
			Bucket:    cfg.Minio.Bucket,
			Region:    cfg.Minio.Region,
			EnableSSL: cfg.Minio.EnableSSL,
		})
	case "gcs":
		return gcs.New(ctx, gcs.Config{
			Bucket:          cfg.GCS.Bucket,
			ProjectID:       cfg.GCS.ProjectID,
			CredentialsFile: cfg.GCS.CredentialsFile,
		})
	case "azureblob":
		return azureblob.New(ctx, azureblob.Config{
			AccountName:   cfg.AzureBlob.AccountName,
			AccountKey:    cfg.AzureBlob.AccountKey,
			ContainerName: cfg.AzureBlob.ContainerName,
		})
	default:
		return nil, fmt.Errorf("unknown storage type: %s", cfg.StorageType)
	}
}

func validateStorage(cfg *config.Config) error {
	switch cfg.StorageType {
	case "memory":
		// No validation needed.
	case "disk":
		if cfg.Disk.RootPath == "" {
			return fmt.Errorf("disk storage requires root_path")
		}
	case "s3":
		if cfg.S3.Bucket == "" {
			return fmt.Errorf("s3 storage requires bucket")
		}
	case "minio":
		if cfg.Minio.Endpoint == "" || cfg.Minio.Bucket == "" {
			return fmt.Errorf("minio storage requires endpoint and bucket")
		}
	case "gcs":
		if cfg.GCS.Bucket == "" {
			return fmt.Errorf("gcs storage requires bucket")
		}
	case "azureblob":
		if cfg.AzureBlob.AccountName == "" || cfg.AzureBlob.ContainerName == "" {
			return fmt.Errorf("azureblob storage requires account_name and container_name")
		}
	default:
		return fmt.Errorf("unknown storage type: %s", cfg.StorageType)
	}
	return nil
}
