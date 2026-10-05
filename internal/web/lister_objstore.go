package web

import (
	"context"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const keyDelimiter = "/"

// ObjectStoreLister implements Lister for S3-compatible backends using the AWS SDK.
// It uses ListObjectsV2 with delimiter to paginate efficiently server-side.
type ObjectStoreLister struct {
	Client *s3.Client
	Bucket string
}

// listObjects walks ListObjectsV2 pages and calls fn for each object.
// fn returns false to stop iteration early.
func (o *ObjectStoreLister) listObjects(ctx context.Context, in *s3.ListObjectsV2Input, fn func(types.Object) bool) error {
	in.Bucket = aws.String(o.Bucket)
	paginator := s3.NewListObjectsV2Paginator(o.Client, in)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			if !fn(obj) {
				return nil
			}
		}
	}
	return nil
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
	in := &s3.ListObjectsV2Input{}
	if cursor != "" && query == "" {
		// Start listing from cursor/@v to skip everything before.
		in.StartAfter = aws.String(cursor + versionPrefix)
	}

	err := o.listObjects(ctx, in, func(obj types.Object) bool {
		key := aws.ToString(obj.Key)
		idx := strings.LastIndex(key, versionPrefix)
		if idx < 0 {
			return true
		}
		modPath := key[:idx]

		// Skip if already seen.
		if _, ok := seen[modPath]; ok {
			return true
		}

		// Apply query filter.
		if lowerQuery != "" && !strings.Contains(strings.ToLower(modPath), lowerQuery) {
			return true
		}

		// Apply cursor (needed when query is set, since StartAfter won't work).
		if cursor != "" && modPath <= cursor {
			return true
		}

		seen[modPath] = struct{}{}
		collected = append(collected, modPath)

		// We need limit+1 to know if there are more results.
		// But with query filtering we can't stop early since results
		// may not be contiguous. Without query, S3 order is lexicographic
		// and StartAfter handles the cursor, so we can stop.
		return !(query == "" && len(collected) > limit)
	})
	if err != nil {
		return nil, err
	}

	// Sort (should already be sorted from S3, but ensure after filtering).
	sort.Strings(collected)

	total := -1 // Unknown for object stores (would require full scan).
	if len(collected) <= limit {
		// If we got fewer than limit, we know the total from cursor onward.
		total = len(collected)
	}

	endIdx := min(limit, len(collected))

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
	err := o.listObjects(ctx, &s3.ListObjectsV2Input{
		Prefix:    aws.String(prefix),
		Delimiter: aws.String(keyDelimiter),
	}, func(obj types.Object) bool {
		name := strings.TrimPrefix(aws.ToString(obj.Key), prefix)
		if name == "list" || name == "" {
			return true
		}
		ext := path.Ext(name)
		if ext != ".info" && ext != ".mod" && ext != ".zip" {
			return true
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
			v.Time = aws.ToTime(obj.LastModified)
		case ".mod":
			v.HasMod = true
		case ".zip":
			v.HasZip = true
			v.Size = aws.ToInt64(obj.Size)
		}
		return true
	})
	if err != nil {
		return m, err
	}

	for _, v := range versions {
		m.Versions = append(m.Versions, *v)
	}
	sort.Slice(m.Versions, func(i, j int) bool {
		return m.Versions[i].Version < m.Versions[j].Version
	})

	return m, nil
}

// ListFiles lists all objects under a module's version prefix.
func (o *ObjectStoreLister) ListFiles(ctx context.Context, modulePath string) ([]FileEntry, error) {
	prefix := modulePath + versionPrefix
	var files []FileEntry

	err := o.listObjects(ctx, &s3.ListObjectsV2Input{
		Prefix:    aws.String(prefix),
		Delimiter: aws.String(keyDelimiter),
	}, func(obj types.Object) bool {
		name := strings.TrimPrefix(aws.ToString(obj.Key), prefix)
		if name == "" {
			return true
		}
		files = append(files, FileEntry{
			Name:    name,
			Size:    aws.ToInt64(obj.Size),
			ModTime: aws.ToTime(obj.LastModified),
		})
		return true
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// GetFile retrieves a single object from the store.
func (o *ObjectStoreLister) GetFile(ctx context.Context, name string) (io.ReadCloser, error) {
	out, err := o.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(o.Bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

// DeleteModule removes cached files for a module from the object store.
// S3 DeleteObject is idempotent, so missing keys are not errors.
func (o *ObjectStoreLister) DeleteModule(ctx context.Context, modulePath, version string) error {
	if version != "" {
		for _, ext := range versionExts {
			key := modulePath + versionPrefix + version + ext
			if _, err := o.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(o.Bucket),
				Key:    aws.String(key),
			}); err != nil {
				return err
			}
		}
		return nil
	}

	// Delete all objects with this module prefix. Keys are collected first
	// so deletes never race the listing on stores with fragile pagination.
	prefix := modulePath + versionPrefix
	var keys []string
	err := o.listObjects(ctx, &s3.ListObjectsV2Input{
		Prefix: aws.String(prefix),
	}, func(obj types.Object) bool {
		keys = append(keys, aws.ToString(obj.Key))
		return true
	})
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := o.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(o.Bucket),
			Key:    aws.String(key),
		}); err != nil {
			return err
		}
	}
	return nil
}
