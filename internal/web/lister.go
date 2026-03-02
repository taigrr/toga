// Package web provides the toga web UI for browsing cached modules and viewing logs.
package web

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Module represents a cached Go module with its available versions.
type Module struct {
	Path     string
	Versions []Version
}

// Version represents a cached module version.
type Version struct {
	Version string
	Time    time.Time
	HasInfo bool
	HasMod  bool
	HasZip  bool
	Size    int64
}

// FileEntry represents a file in the cache for display.
type FileEntry struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// ModulePage is a paginated result set of modules.
type ModulePage struct {
	Modules    []Module
	NextCursor string // empty if no more results
	Total      int    // total matching modules (-1 if unknown)
}

// DefaultPageSize is the number of modules per page.
const DefaultPageSize = 50

// Lister can enumerate cached modules. Implemented per storage backend.
type Lister interface {
	// ListModules returns a paginated list of cached modules.
	// cursor is the module path to start after (empty for first page).
	// query filters by substring match (empty for all).
	// limit is max results to return.
	ListModules(ctx context.Context, cursor, query string, limit int) (*ModulePage, error)
	ListFiles(ctx context.Context, modulePath string) ([]FileEntry, error)
	GetFile(ctx context.Context, name string) (io.ReadCloser, error)
	DeleteModule(ctx context.Context, modulePath, version string) error
}

// DiskLister implements Lister for filesystem-based caches.
type DiskLister struct {
	Root string
}

// ListModules walks the cache directory with pagination support.
func (d *DiskLister) ListModules(ctx context.Context, cursor, query string, limit int) (*ModulePage, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}

	// Walk and collect all module paths first (just paths, not full info).
	// For disk, we have to walk regardless, but we can stop early if no filter.
	modules := make(map[string]*Module)

	err := filepath.WalkDir(d.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		rel, err := filepath.Rel(d.Root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		atV := "/@v/"
		idx := strings.LastIndex(rel, atV)
		if idx < 0 {
			return nil
		}

		modPath := rel[:idx]
		fileName := rel[idx+len(atV):]
		ext := filepath.Ext(fileName)
		if ext != ".info" && ext != ".mod" && ext != ".zip" && fileName != "list" {
			return nil
		}

		// Apply query filter early.
		if query != "" && !strings.Contains(strings.ToLower(modPath), query) {
			return nil
		}

		mod, ok := modules[modPath]
		if !ok {
			mod = &Module{Path: modPath}
			modules[modPath] = mod
		}
		if fileName == "list" {
			return nil
		}

		ver := strings.TrimSuffix(fileName, ext)
		var v *Version
		for i := range mod.Versions {
			if mod.Versions[i].Version == ver {
				v = &mod.Versions[i]
				break
			}
		}
		if v == nil {
			mod.Versions = append(mod.Versions, Version{Version: ver})
			v = &mod.Versions[len(mod.Versions)-1]
		}

		info, _ := entry.Info()
		switch ext {
		case ".info":
			v.HasInfo = true
			if info != nil {
				v.Time = info.ModTime()
			}
		case ".mod":
			v.HasMod = true
		case ".zip":
			v.HasZip = true
			if info != nil {
				v.Size = info.Size()
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort all paths.
	sorted := make([]string, 0, len(modules))
	for p := range modules {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	total := len(sorted)

	// Apply cursor: find the start index.
	startIdx := 0
	if cursor != "" {
		for i, p := range sorted {
			if p == cursor {
				startIdx = i + 1
				break
			}
		}
	}

	// Slice the page.
	endIdx := startIdx + limit
	if endIdx > len(sorted) {
		endIdx = len(sorted)
	}

	page := &ModulePage{Total: total}
	for _, p := range sorted[startIdx:endIdx] {
		m := modules[p]
		sort.Slice(m.Versions, func(i, j int) bool {
			return m.Versions[i].Version < m.Versions[j].Version
		})
		page.Modules = append(page.Modules, *m)
	}

	if endIdx < len(sorted) {
		page.NextCursor = sorted[endIdx-1]
	}

	return page, nil
}

// ListFiles returns all cached files for a module path.
func (d *DiskLister) ListFiles(_ context.Context, modulePath string) ([]FileEntry, error) {
	dir := filepath.Join(d.Root, filepath.FromSlash(modulePath), "@v")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []FileEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileEntry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return files, nil
}

// GetFile returns a cached file by its cache-relative name.
func (d *DiskLister) GetFile(_ context.Context, name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(d.Root, filepath.FromSlash(name)))
}

// DeleteModule removes cached files for a module path and optional version.
func (d *DiskLister) DeleteModule(_ context.Context, modulePath, version string) error {
	if version != "" {
		dir := filepath.Join(d.Root, filepath.FromSlash(modulePath), "@v")
		for _, ext := range []string{".info", ".mod", ".zip"} {
			os.Remove(filepath.Join(dir, version+ext))
		}
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) == 0 {
			os.Remove(dir)
			cleanEmptyParents(d.Root, filepath.Join(d.Root, filepath.FromSlash(modulePath)))
		}
		return nil
	}

	modDir := filepath.Join(d.Root, filepath.FromSlash(modulePath))
	if err := os.RemoveAll(modDir); err != nil {
		return err
	}
	cleanEmptyParents(d.Root, modDir)
	return nil
}

func cleanEmptyParents(root, dir string) {
	for {
		dir = filepath.Dir(dir)
		if dir == root || !strings.HasPrefix(dir, root) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		os.Remove(dir)
	}
}
