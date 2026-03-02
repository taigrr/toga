package web

import (
	"context"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
)

// ObjectStoreLister implements Lister for S3/MinIO backends using the minio-go client.
// It uses ListObjects with delimiter to paginate efficiently server-side.
type ObjectStoreLister struct {
	Client *minio.Client
	Bucket string
}

// ListModules uses S3 ListObjects to enumerate module paths.
// Since S3 keys are flat (module/@v/version.ext), we list with prefix=""
// and look for keys containing versionPrefix to extract module paths.
// For large buckets, this streams results instead of loading all keys into memory.
// ListModules returns a paginated list of cached modules from the object store.
func (o *ObjectStoreLister) ListModules(ctx context.Context, cursor, query string, limit int) (*ModulePage, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}

	lowerQuery := strings.ToLower(query)

	// Use ListObjects to stream all keys. We look for the /@v/ marker
	// to identify module paths. S3 returns keys in lexicographic order,
	// so we can build a sorted set of module paths.
	//
	// For very large caches, we stream and collect unique module paths,
	// stopping once we have enough past the cursor.
	seen := make(map[string]struct{})
	var collected []string

	// If we have a cursor and no query, use StartAfter to skip server-side.
	opts := minio.ListObjectsOptions{
		Recursive: true,
	}
	if cursor != "" && query == "" {
		// Start listing from cursor/@v to skip everything before.
		opts.StartAfter = cursor + versionPrefix
	}

	for obj := range o.Client.ListObjects(ctx, o.Bucket, opts) {
		if obj.Err != nil {
			return nil, obj.Err
		}

		atV := versionPrefix
		idx := strings.LastIndex(obj.Key, atV)
		if idx < 0 {
			continue
		}
		modPath := obj.Key[:idx]

		// Skip if already seen.
		if _, ok := seen[modPath]; ok {
			continue
		}

		// Apply query filter.
		if lowerQuery != "" && !strings.Contains(strings.ToLower(modPath), lowerQuery) {
			continue
		}

		// Apply cursor (needed when query is set, since StartAfter won't work).
		if cursor != "" && modPath <= cursor {
			continue
		}

		seen[modPath] = struct{}{}
		collected = append(collected, modPath)

		// We need limit+1 to know if there are more results.
		// But with query filtering we can't stop early since results
		// may not be contiguous. Without query, S3 order is lexicographic
		// and StartAfter handles the cursor, so we can stop.
		if query == "" && len(collected) > limit {
			break
		}
	}

	// Sort (should already be sorted from S3, but ensure after filtering).
	sort.Strings(collected)

	total := -1 // Unknown for object stores (would require full scan).
	if len(collected) <= limit {
		// If we got fewer than limit, we know the total from cursor onward.
		total = len(collected)
	}

	endIdx := limit
	if endIdx > len(collected) {
		endIdx = len(collected)
	}

	page := &ModulePage{Total: total}

	// Load version details for each module in the page.
	for _, modPath := range collected[:endIdx] {
		m, err := o.loadModuleDetail(ctx, modPath)
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

// loadModuleDetail lists objects under module/@v/ to build version info.
func (o *ObjectStoreLister) loadModuleDetail(ctx context.Context, modPath string) (Module, error) {
	m := Module{Path: modPath}
	prefix := modPath + versionPrefix

	versions := make(map[string]*Version)
	for obj := range o.Client.ListObjects(ctx, o.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	}) {
		if obj.Err != nil {
			return m, obj.Err
		}

		name := strings.TrimPrefix(obj.Key, prefix)
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
			v.Time = obj.LastModified
		case ".mod":
			v.HasMod = true
		case ".zip":
			v.HasZip = true
			v.Size = obj.Size
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

// ListFiles lists all objects under module/@v/.
// ListFiles lists all objects under a module's version prefix.
func (o *ObjectStoreLister) ListFiles(ctx context.Context, modulePath string) ([]FileEntry, error) {
	prefix := modulePath + versionPrefix
	var files []FileEntry

	for obj := range o.Client.ListObjects(ctx, o.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		name := strings.TrimPrefix(obj.Key, prefix)
		if name == "" {
			continue
		}
		files = append(files, FileEntry{
			Name:    name,
			Size:    obj.Size,
			ModTime: obj.LastModified,
		})
	}
	return files, nil
}

// GetFile retrieves a single object from the store.
func (o *ObjectStoreLister) GetFile(ctx context.Context, name string) (io.ReadCloser, error) {
	obj, err := o.Client.GetObject(ctx, o.Bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// Verify the object exists.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, err
	}
	return obj, nil
}

// DeleteModule removes cached files for a module from the object store.
func (o *ObjectStoreLister) DeleteModule(ctx context.Context, modulePath, version string) error {
	if version != "" {
		for _, ext := range versionExts {
			key := modulePath + versionPrefix + version + ext
			_ = o.Client.RemoveObject(ctx, o.Bucket, key, minio.RemoveObjectOptions{})
		}
		return nil
	}

	// Delete all objects with this module prefix.
	prefix := modulePath + versionPrefix
	for obj := range o.Client.ListObjects(ctx, o.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return obj.Err
		}
		if err := o.Client.RemoveObject(ctx, o.Bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}
