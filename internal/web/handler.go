package web

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/goproxy/goproxy"
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

	modules, err := h.Lister.ListModules(r.Context())
	if err != nil {
		h.Logger.Error("list modules", "error", err)
		http.Error(w, "failed to list modules", http.StatusInternalServerError)
		return
	}

	if query != "" {
		filtered := modules[:0]
		for _, m := range modules {
			if strings.Contains(strings.ToLower(m.Path), query) {
				filtered = append(filtered, m)
			}
		}
		modules = filtered
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	moduleListFragment(modules, h.Prefix).Render(r.Context(), w)
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

	if strings.Contains(name, "..") {
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

func (h *Handler) handleFetch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	modPath := r.FormValue("module")
	version := r.FormValue("version")

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

	h.Logger.Info("fetching module", "module", modPath, "version", version)

	ctx := r.Context()
	if version == "" || version == "latest" {
		// Query for the latest version.
		resolved, _, err := h.Fetcher.Query(ctx, modPath, "latest")
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fetchResultFragment(modPath, err).Render(ctx, w)
			return
		}
		version = resolved
	}

	// Download info, mod, zip and cache them.
	info, mod, zip, err := h.Fetcher.Download(ctx, modPath, version)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fetchResultFragment(modPath, err).Render(ctx, w)
		return
	}
	defer info.Close()
	defer mod.Close()
	defer zip.Close()

	// Cache all three files if we have a cacher.
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
			if err := h.Cacher.Put(ctx, pair.name, pair.r); err != nil {
				h.Logger.Error("cache put", "name", pair.name, "error", err)
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fetchResultFragment(modPath+"@"+version, nil).Render(ctx, w)
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
