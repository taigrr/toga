package web

import (
	"context"
	"io"
	"path"
	"sort"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// GCSLister implements Lister for Google Cloud Storage backends.
type GCSLister struct {
	Client *storage.Client
	Bucket string
}

func (g *GCSLister) ListModules(ctx context.Context, cursor, query string, limit int) (*ModulePage, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}

	lowerQuery := strings.ToLower(query)
	seen := make(map[string]struct{})
	var collected []string

	q := &storage.Query{Versions: false}
	if cursor != "" && query == "" {
		q.StartOffset = cursor + "/@v/"
	}

	it := g.Client.Bucket(g.Bucket).Objects(ctx, q)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		idx := strings.LastIndex(attrs.Name, "/@v/")
		if idx < 0 {
			continue
		}
		modPath := attrs.Name[:idx]

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
		m, err := g.loadModuleDetail(ctx, modPath)
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

func (g *GCSLister) loadModuleDetail(ctx context.Context, modPath string) (Module, error) {
	m := Module{Path: modPath}
	prefix := modPath + "/@v/"

	versions := make(map[string]*Version)
	q := &storage.Query{Prefix: prefix}
	it := g.Client.Bucket(g.Bucket).Objects(ctx, q)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return m, err
		}

		name := strings.TrimPrefix(attrs.Name, prefix)
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
			v.Time = attrs.Updated
		case ".mod":
			v.HasMod = true
		case ".zip":
			v.HasZip = true
			v.Size = attrs.Size
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

func (g *GCSLister) ListFiles(ctx context.Context, modulePath string) ([]FileEntry, error) {
	prefix := modulePath + "/@v/"
	var files []FileEntry

	q := &storage.Query{Prefix: prefix}
	it := g.Client.Bucket(g.Bucket).Objects(ctx, q)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(attrs.Name, prefix)
		if name == "" {
			continue
		}
		files = append(files, FileEntry{
			Name:    name,
			Size:    attrs.Size,
			ModTime: attrs.Updated,
		})
	}
	return files, nil
}

func (g *GCSLister) GetFile(ctx context.Context, name string) (io.ReadCloser, error) {
	return g.Client.Bucket(g.Bucket).Object(name).NewReader(ctx)
}

func (g *GCSLister) DeleteModule(ctx context.Context, modulePath, version string) error {
	if version != "" {
		for _, ext := range []string{".info", ".mod", ".zip"} {
			key := modulePath + "/@v/" + version + ext
			_ = g.Client.Bucket(g.Bucket).Object(key).Delete(ctx)
		}
		return nil
	}

	prefix := modulePath + "/@v/"
	q := &storage.Query{Prefix: prefix}
	it := g.Client.Bucket(g.Bucket).Objects(ctx, q)
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		if err := g.Client.Bucket(g.Bucket).Object(attrs.Name).Delete(ctx); err != nil {
			return err
		}
	}
	return nil
}
