package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	case r.Method == http.MethodGet && sub == "/fetch-status":
		h.handleFetchStatus(w, r)
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

// fetchJob tracks an in-flight recursive fetch.
type fetchJob struct {
	count   atomic.Int32
	latest  atomic.Value // string
	mu      sync.Mutex
	results []FetchResult
	done    chan struct{}
}

var activeJobs sync.Map

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

	// Recursive fetch: start background job, return polling UI.
	h.Logger.Info("recursive fetch", "module", modPath, "version", version)

	b := make([]byte, 8)
	rand.Read(b)
	jobID := hex.EncodeToString(b)

	job := &fetchJob{done: make(chan struct{})}
	activeJobs.Store(jobID, job)

	go func() {
		defer func() {
			close(job.done)
			time.AfterFunc(60*time.Second, func() {
				activeJobs.Delete(jobID)
			})
		}()
		h.fetchRecursive(context.Background(), modPath, version, func(r FetchResult) {
			job.mu.Lock()
			job.results = append(job.results, r)
			job.mu.Unlock()
			job.count.Add(1)
			job.latest.Store(r.Module)
		})
	}()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fetchPollingFragment(jobID, h.Prefix).Render(ctx, w)
}

// fetchOne fetches a single module version and caches it.
func (h *Handler) fetchOne(ctx context.Context, modPath, version string) FetchResult {
	_, result := h.fetchModFileBytes(ctx, modPath, version)
	return result
}

// fetchModFileBytes fetches a module, caches it, and returns the go.mod content along with the result.
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

// ProgressFunc is called after each module is fetched during recursive fetch.
type ProgressFunc func(FetchResult)

// fetchRecursive fetches a module and all its transitive dependencies.
func (h *Handler) fetchRecursive(ctx context.Context, modPath, version string, onProgress ProgressFunc) {
	seen := make(map[string]bool)
	h.fetchRecursiveWalk(ctx, modPath, version, seen, onProgress)
}

func (h *Handler) fetchRecursiveWalk(ctx context.Context, modPath, version string, seen map[string]bool, onProgress ProgressFunc) {
	if seen[modPath] {
		return
	}
	seen[modPath] = true

	// Try reading go.mod from cache first to avoid re-fetching.
	var modData []byte
	if h.Cacher != nil && version != "" && version != "latest" {
		base := modPath + "/@v/" + version
		if rc, err := h.Cacher.Get(ctx, base+".mod"); err == nil {
			data, readErr := io.ReadAll(rc)
			rc.Close()
			if readErr == nil {
				// Also verify .zip exists so we know it is fully cached.
				if zrc, zerr := h.Cacher.Get(ctx, base+".zip"); zerr == nil {
					zrc.Close()
					modData = data
					if onProgress != nil {
						onProgress(FetchResult{Module: modPath + "@" + version})
					}
					h.Logger.Info("using cached module", "module", modPath, "version", version)
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			h.Logger.Warn("cache check failed", "module", modPath, "error", err)
		}
	}

	// If not cached, fetch from upstream.
	if modData == nil {
		var result FetchResult
		modData, result = h.fetchModFileBytes(ctx, modPath, version)
		if onProgress != nil {
			onProgress(result)
		}
		if result.Err != nil || modData == nil {
			return
		}
	}

	f, err := modfile.Parse("go.mod", modData, nil)
	if err != nil {
		h.Logger.Warn("parse go.mod", "module", modPath, "error", err)
		return
	}

	for _, req := range f.Require {
		h.fetchRecursiveWalk(ctx, req.Mod.Path, req.Mod.Version, seen, onProgress)
	}
}

func (h *Handler) handleFetchStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("id")
	val, ok := activeJobs.Load(jobID)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	job := val.(*fetchJob)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	select {
	case <-job.done:
		// Job complete — return final results.
		job.mu.Lock()
		results := make([]FetchResult, len(job.results))
		copy(results, job.results)
		job.mu.Unlock()
		fetchRecursiveResultFragment(results).Render(r.Context(), w)
	default:
		// Still running — return progress with continued polling.
		count := int(job.count.Load())
		latest, _ := job.latest.Load().(string)
		fetchPollingProgressFragment(jobID, count, latest, h.Prefix).Render(r.Context(), w)
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
