// Package azureblob implements goproxy.Cacher backed by Azure Blob Storage.
package azureblob

import (
	"context"
	"io"
	"io/fs"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

// Config holds Azure Blob Storage connection parameters.
type Config struct {
	AccountName   string
	AccountKey    string
	ContainerName string
}

// Cacher implements goproxy.Cacher using Azure Blob Storage.
type Cacher struct {
	client    *azblob.Client
	container string
}

// New creates an Azure Blob Storage-backed Cacher.
func New(_ context.Context, cfg Config) (*Cacher, error) {
	cred, err := azblob.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	if err != nil {
		return nil, err
	}

	serviceURL := "https://" + cfg.AccountName + ".blob.core.windows.net"
	client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		return nil, err
	}

	return &Cacher{client: client, container: cfg.ContainerName}, nil
}

// Get retrieves a cached module file.
func (c *Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	resp, err := c.client.DownloadStream(ctx, c.container, name, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return resp.Body, nil
}

// Put stores a module file in Azure Blob Storage.
func (c *Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	_, err = c.client.UploadBuffer(ctx, c.container, name, data, nil)
	return err
}
