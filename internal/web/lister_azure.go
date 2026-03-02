package web

import (
	"context"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// AzureLister implements Lister for Azure Blob Storage backends.
type AzureLister struct {
	Client    *azblob.Client
	Container string
}

// ListModules returns a paginated list of cached modules from the backend.
func (a *AzureLister) ListModules(ctx context.Context, cursor, query string, limit int) (*ModulePage, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}

	lowerQuery := strings.ToLower(query)
	seen := make(map[string]struct{})
	var collected []string

	opts := &azblob.ListBlobsFlatOptions{}
	// Azure Marker is an opaque continuation token, not a seek position.
	// We rely on client-side cursor filtering instead.

	pager := a.Client.NewListBlobsFlatPager(a.Container, opts)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, blob := range resp.Segment.BlobItems {
			if blob.Name == nil {
				continue
			}

			idx := strings.LastIndex(*blob.Name, versionPrefix)
			if idx < 0 {
				continue
			}
			modPath := (*blob.Name)[:idx]

			if _, ok := seen[modPath]; ok {
				continue
			}
			if lowerQuery != "" && !strings.Contains(strings.ToLower(modPath), lowerQuery) {
				continue
			}
			if cursor != "" && modPath <= cursor {
				continue
			}

			seen[modPath] = struct{}{}
			collected = append(collected, modPath)
		}

		if query == "" && len(collected) > limit {
			break
		}
	}

	sort.Strings(collected)

	total := -1
	if len(collected) <= limit {
		total = len(collected)
	}

	endIdx := limit
	if endIdx > len(collected) {
		endIdx = len(collected)
	}

	page := &ModulePage{Total: total}
	for _, modPath := range collected[:endIdx] {
		m, err := a.loadModuleDetail(ctx, modPath)
		if err != nil {
			continue
		}
		page.Modules = append(page.Modules, m)
	}

	if endIdx < len(collected) {
		page.NextCursor = collected[endIdx-1]
	}

	return page, nil
}

func (a *AzureLister) loadModuleDetail(ctx context.Context, modPath string) (Module, error) {
	m := Module{Path: modPath}
	prefix := modPath + versionPrefix

	versions := make(map[string]*Version)
	opts := &azblob.ListBlobsFlatOptions{Prefix: &prefix}
	pager := a.Client.NewListBlobsFlatPager(a.Container, opts)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return m, err
		}

		for _, blob := range resp.Segment.BlobItems {
			if blob.Name == nil {
				continue
			}
			name := strings.TrimPrefix(*blob.Name, prefix)
			if name == "list" || name == "" {
				continue
			}
			ext := path.Ext(name)
			if ext != ".info" && ext != ".mod" && ext != ".zip" {
				continue
			}

			ver := strings.TrimSuffix(name, ext)
			v, ok := versions[ver]
			if !ok {
				v = &Version{Version: ver}
				versions[ver] = v
			}

			switch ext {
			case ".info":
				v.HasInfo = true
				if blob.Properties != nil && blob.Properties.LastModified != nil {
					v.Time = *blob.Properties.LastModified
				}
			case ".mod":
				v.HasMod = true
			case ".zip":
				v.HasZip = true
				if blob.Properties != nil && blob.Properties.ContentLength != nil {
					v.Size = *blob.Properties.ContentLength
				}
			}
		}
	}

	for _, v := range versions {
		m.Versions = append(m.Versions, *v)
	}
	sort.Slice(m.Versions, func(i, j int) bool {
		return m.Versions[i].Version < m.Versions[j].Version
	})

	return m, nil
}

// ListFiles lists all cached files for a module path.
func (a *AzureLister) ListFiles(ctx context.Context, modulePath string) ([]FileEntry, error) {
	prefix := modulePath + versionPrefix
	var files []FileEntry

	opts := &azblob.ListBlobsFlatOptions{Prefix: &prefix}
	pager := a.Client.NewListBlobsFlatPager(a.Container, opts)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, blob := range resp.Segment.BlobItems {
			if blob.Name == nil {
				continue
			}
			name := strings.TrimPrefix(*blob.Name, prefix)
			if name == "" {
				continue
			}
			fe := FileEntry{Name: name}
			if blob.Properties != nil {
				if blob.Properties.ContentLength != nil {
					fe.Size = *blob.Properties.ContentLength
				}
				if blob.Properties.LastModified != nil {
					fe.ModTime = *blob.Properties.LastModified
				}
			}
			files = append(files, fe)
		}
	}
	return files, nil
}

// GetFile retrieves a single cached file by name.
func (a *AzureLister) GetFile(ctx context.Context, name string) (io.ReadCloser, error) {
	resp, err := a.Client.DownloadStream(ctx, a.Container, name, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// DeleteModule removes cached files for a module and optional version.
func (a *AzureLister) DeleteModule(ctx context.Context, modulePath, version string) error {
	if version != "" {
		for _, ext := range []string{".info", ".mod", ".zip"} {
			key := modulePath + versionPrefix + version + ext
			_, _ = a.Client.DeleteBlob(ctx, a.Container, key, nil)
		}
		return nil
	}

	prefix := modulePath + versionPrefix
	opts := &azblob.ListBlobsFlatOptions{Prefix: &prefix}
	pager := a.Client.NewListBlobsFlatPager(a.Container, opts)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, blob := range resp.Segment.BlobItems {
			if blob.Name == nil {
				continue
			}
			if _, err := a.Client.DeleteBlob(ctx, a.Container, *blob.Name, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
