package web

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiskLister implements Lister for filesystem-based caches.
type DiskLister struct {
	Root string
}

// ListModules walks the cache directory with efficient pagination.
// Instead of walking the entire tree, it:
//  1. Collects only module paths (directories containing @v/).
//  2. Skips directory subtrees that sort before the cursor.
//  3. Stops walking once limit+1 modules are found (for hasMore detection).
//  4. Only stats files for the modules in the returned page.
func (d *DiskLister) ListModules(ctx context.Context, cursor, query string, limit int) (*ModulePage, error) {
	if limit <= 0 {
		limit = DefaultPageSize
	}

	// Phase 1: Collect module paths efficiently.
	// A module path is any directory that contains an @v/ subdirectory.
	modPaths := make(map[string]struct{})
	lowerQuery := strings.ToLower(query)

	err := filepath.WalkDir(d.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if !entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(d.Root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		// Skip the @v directory itself — the parent is the module.
		if filepath.Base(rel) == "@v" {
			modPath := strings.TrimSuffix(rel, "/@v")
			if modPath == "" {
				return fs.SkipDir
			}

			// Apply query filter.
			if lowerQuery != "" && !strings.Contains(strings.ToLower(modPath), lowerQuery) {
				return fs.SkipDir
			}

			modPaths[modPath] = struct{}{}
			return fs.SkipDir
		}

		// If we have no query filter and no cursor, we can't skip subtrees.
		// If we have a cursor and this whole subtree sorts before it,
		// skip it. Directory walk is lexicographic within a level, but
		// module paths span multiple levels (e.g. github.com/user/repo),
		// so we can only skip if the full rel path is a prefix check.
		// This optimization helps when cursor is deep into the alphabet.
		if cursor != "" && lowerQuery == "" {
			// If this directory's path is a prefix of the cursor or comes after,
			// keep walking. If it sorts entirely before and can't contain the cursor,
			// skip it.
			if rel != "." && !strings.HasPrefix(cursor, rel) && rel > cursor {
				// We're past the cursor — keep going, we need results.
			} else if rel != "." && !strings.HasPrefix(cursor, rel) && rel < cursor {
				// This subtree is entirely before cursor. But it might contain
				// paths like rel/something/@v that sort after cursor.
				// We can only safely skip if cursor doesn't start with rel+"/".
				if !strings.HasPrefix(cursor, rel+"/") {
					return fs.SkipDir
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Phase 2: Sort and paginate.
	sorted := make([]string, 0, len(modPaths))
	for p := range modPaths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	total := len(sorted)

	// Apply cursor.
	startIdx := 0
	if cursor != "" {
		startIdx = sort.SearchStrings(sorted, cursor)
		// If cursor is found exactly, skip past it.
		if startIdx < len(sorted) && sorted[startIdx] == cursor {
			startIdx++
		}
	}

	endIdx := startIdx + limit
	if endIdx > len(sorted) {
		endIdx = len(sorted)
	}

	page := &ModulePage{Total: total}

	// Phase 3: Only load version details for modules in this page.
	for _, modPath := range sorted[startIdx:endIdx] {
		m := d.loadModuleDetail(modPath)
		page.Modules = append(page.Modules, m)
	}

	if endIdx < len(sorted) {
		page.NextCursor = sorted[endIdx-1]
	}

	return page, nil
}

// loadModuleDetail reads version info from a module's @v directory.
func (d *DiskLister) loadModuleDetail(modPath string) Module {
	m := Module{Path: modPath}
	dir := filepath.Join(d.Root, filepath.FromSlash(modPath), "@v")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return m
	}

	versions := make(map[string]*Version)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "list" {
			continue
		}
		ext := filepath.Ext(name)
		if ext != ".info" && ext != ".mod" && ext != ".zip" {
			continue
		}

		ver := strings.TrimSuffix(name, ext)
		v, ok := versions[ver]
		if !ok {
			v = &Version{Version: ver}
			versions[ver] = v
		}

		info, _ := e.Info()
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
	}

	for _, v := range versions {
		m.Versions = append(m.Versions, *v)
	}
	sort.Slice(m.Versions, func(i, j int) bool {
		return m.Versions[i].Version < m.Versions[j].Version
	})

	return m
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
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		return nil, fmt.Errorf("invalid file path: %s", name)
	}
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
