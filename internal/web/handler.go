package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/goproxy/goproxy"
	"golang.org/x/mod/modfile"
)

// Handler serves the toga web UI.
type Handler struct {
	Lister  Lister
	Fetcher *goproxy.GoFetcher
	Cacher  goproxy.Cacher
	Logger  *slog.Logger
	Prefix  string // URL prefix, e.g. "/-/ui"
}

// ServeHTTP routes UI requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, h.Prefix)
	if sub == r.URL.Path {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(sub, "/static/"):
		http.StripPrefix(h.Prefix+"/static/", http.FileServer(StaticFS())).ServeHTTP(w, r)
	case r.Method == http.MethodGet && (sub == "" || sub == "/"):
		h.handleIndex(w, r)
	case r.Method == http.MethodGet && sub == "/modules":
		h.handleModuleList(w, r)
	case r.Method == http.MethodGet && sub == "/module":
		h.handleModuleDetail(w, r)
	case r.Method == http.MethodGet && sub == "/download":
		h.handleDownload(w, r)
	case r.Method == http.MethodGet && sub == "/file":
		h.handleFileView(w, r)
	case r.Method == http.MethodPost && sub == "/fetch":
		h.handleFetch(w, r)
	case r.Method == http.MethodPost && sub == "/delete":
		h.handleDelete(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexPage(h.Prefix).Render(r.Context(), w)
}

func (h *Handler) handleModuleList(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(r.URL.Query().Get("q"))
	cursor := r.URL.Query().Get("cursor")

	page, err := h.Lister.ListModules(r.Context(), cursor, query, DefaultPageSize)
	if err != nil {
		h.Logger.Error("list modules", "error", err)
		http.Error(w, "failed to list modules", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// If this is a "load more" request (has cursor), return just the rows.
	if cursor != "" {
		moduleRowsFragment(page, query, h.Prefix).Render(r.Context(), w)
	} else {
		moduleListFragment(page, query, h.Prefix).Render(r.Context(), w)
	}
}

func (h *Handler) handleModuleDetail(w http.ResponseWriter, r *http.Request) {
	modPath := r.URL.Query().Get("path")
	if modPath == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}

	files, err := h.Lister.ListFiles(r.Context(), modPath)
	if err != nil {
		h.Logger.Error("list files", "module", modPath, "error", err)
		http.Error(w, "failed to list files", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	moduleDetailFragment(modPath, files, h.Prefix).Render(r.Context(), w)
}

func (h *Handler) handleFileView(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}

	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	rc, err := h.Lister.GetFile(r.Context(), name)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer rc.Close()

	const maxSize = 1 << 20
	data, err := io.ReadAll(io.LimitReader(rc, maxSize))
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}

	ext := path.Ext(name)
	lang := extToLang(ext)
	truncated := len(data) == maxSize

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fileViewFragment(name, string(data), lang, truncated).Render(r.Context(), w)
}

// FetchResult holds the outcome of fetching a single module.
type FetchResult struct {
	Module string
	Err    error
}

func (h *Handler) handleFetch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	modPath := r.FormValue("module")
	version := r.FormValue("version")
	recursive := r.FormValue("recursive") == "on"

	if modPath == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fetchResultFragment("", fmt.Errorf("module path is required")).Render(r.Context(), w)
		return
	}

	if h.Fetcher == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fetchResultFragment(modPath, fmt.Errorf("fetcher not configured (offline mode)")).Render(r.Context(), w)
		return
	}

	ctx := r.Context()

	if !recursive {
		result := h.fetchOne(ctx, modPath, version)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fetchResultFragment(result.Module, result.Err).Render(ctx, w)
		return
	}

	// Recursive fetch: get the module and all its dependencies.
	h.Logger.Info("recursive fetch", "module", modPath, "version", version)
	results := h.fetchRecursive(ctx, modPath, version)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fetchRecursiveResultFragment(results).Render(ctx, w)
}

// fetchOne fetches a single module version, caches it, and returns the mod file bytes.
func (h *Handler) fetchOne(ctx context.Context, modPath, version string) FetchResult {
	h.Logger.Info("fetching module", "module", modPath, "version", version)

	if version == "" || version == "latest" {
		resolved, _, err := h.Fetcher.Query(ctx, modPath, "latest")
		if err != nil {
			return FetchResult{Module: modPath, Err: err}
		}
		version = resolved
	}

	info, mod, zip, err := h.Fetcher.Download(ctx, modPath, version)
	if err != nil {
		return FetchResult{Module: modPath + "@" + version, Err: err}
	}
	defer info.Close()
	defer mod.Close()
	defer zip.Close()

	if h.Cacher != nil {
		base := modPath + "/@v/" + version
		for _, pair := range []struct {
			name string
			r    io.ReadSeeker
		}{
			{base + ".info", info},
			{base + ".mod", mod},
			{base + ".zip", zip},
		} {
			if _, err := pair.r.Seek(0, io.SeekStart); err != nil {
				h.Logger.Error("cache seek", "name", pair.name, "error", err)
				continue
			}
			if err := h.Cacher.Put(ctx, pair.name, pair.r); err != nil {
				h.Logger.Error("cache put", "name", pair.name, "error", err)
			}
		}
	}

	return FetchResult{Module: modPath + "@" + version}
}

// fetchModFileBytes fetches a module and returns the go.mod content along with the result.
func (h *Handler) fetchModFileBytes(ctx context.Context, modPath, version string) ([]byte, FetchResult) {
	h.Logger.Info("fetching module", "module", modPath, "version", version)

	if version == "" || version == "latest" {
		resolved, _, err := h.Fetcher.Query(ctx, modPath, "latest")
		if err != nil {
			return nil, FetchResult{Module: modPath, Err: err}
		}
		version = resolved
	}

	info, mod, zip, err := h.Fetcher.Download(ctx, modPath, version)
	if err != nil {
		return nil, FetchResult{Module: modPath + "@" + version, Err: err}
	}
	defer info.Close()
	defer zip.Close()

	modData, err := io.ReadAll(mod)
	mod.Close()
	if err != nil {
		return nil, FetchResult{Module: modPath + "@" + version, Err: err}
	}

	if h.Cacher != nil {
		base := modPath + "/@v/" + version
		for _, pair := range []struct {
			name string
			r    io.ReadSeeker
		}{
			{base + ".info", info},
			{base + ".mod", bytes.NewReader(modData)},
			{base + ".zip", zip},
		} {
			if _, err := pair.r.Seek(0, io.SeekStart); err != nil {
				h.Logger.Error("cache seek", "name", pair.name, "error", err)
				continue
			}
			if err := h.Cacher.Put(ctx, pair.name, pair.r); err != nil {
				h.Logger.Error("cache put", "name", pair.name, "error", err)
			}
		}
	}

	return modData, FetchResult{Module: modPath + "@" + version}
}

// fetchRecursive fetches a module and all its transitive dependencies.
func (h *Handler) fetchRecursive(ctx context.Context, modPath, version string) []FetchResult {
	var results []FetchResult
	seen := make(map[string]bool)
	h.fetchRecursiveWalk(ctx, modPath, version, seen, &results)
	return results
}

func (h *Handler) fetchRecursiveWalk(ctx context.Context, modPath, version string, seen map[string]bool, results *[]FetchResult) {
	if seen[modPath] {
		return
	}
	seen[modPath] = true

	modData, result := h.fetchModFileBytes(ctx, modPath, version)
	*results = append(*results, result)
	if result.Err != nil || modData == nil {
		return
	}

	f, err := modfile.Parse("go.mod", modData, nil)
	if err != nil {
		h.Logger.Warn("parse go.mod", "module", modPath, "error", err)
		return
	}

	for _, req := range f.Require {
		h.fetchRecursiveWalk(ctx, req.Mod.Path, req.Mod.Version, seen, results)
	}
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	selected := r.Form["selected"]
	if len(selected) == 0 {
		modPath := r.FormValue("module")
		version := r.FormValue("version")
		if modPath == "" {
			http.Error(w, "missing module", http.StatusBadRequest)
			return
		}
		selected = []string{modPath + "@" + version}
	}

	deleted := 0
	for _, s := range selected {
		modPath, version, _ := strings.Cut(s, "@")
		if modPath == "" {
			continue
		}
		if err := h.Lister.DeleteModule(r.Context(), modPath, version); err != nil {
			h.Logger.Error("delete module", "module", s, "error", err)
		} else {
			deleted++
		}
	}

	h.Logger.Info("deleted modules", "count", deleted)
	h.handleModuleList(w, r)
}

func (h *Handler) handleDownload(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}

	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	rc, err := h.Lister.GetFile(r.Context(), name)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer rc.Close()

	filename := path.Base(name)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if strings.HasSuffix(name, ".zip") {
		w.Header().Set("Content-Type", "application/zip")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	io.Copy(w, rc)
}
func extToLang(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".mod":
		return "go"
	case ".sum":
		return "plaintext"
	case ".json", ".info":
		return "json"
	default:
		return "plaintext"
	}
}
