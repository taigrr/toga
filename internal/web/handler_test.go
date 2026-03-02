package web

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// mockLister implements Lister for testing.
type mockLister struct {
	modules  []Module
	files    []FileEntry
	fileData map[string]string
	deleted  []string
}

func (m *mockLister) ListModules(_ context.Context, cursor, query string, limit int) (*ModulePage, error) {
	var filtered []Module
	for _, mod := range m.modules {
		if query != "" && !strings.Contains(strings.ToLower(mod.Path), strings.ToLower(query)) {
			continue
		}
		if cursor != "" && mod.Path <= cursor {
			continue
		}
		filtered = append(filtered, mod)
		if len(filtered) >= limit {
			break
		}
	}
	return &ModulePage{Modules: filtered, Total: len(filtered)}, nil
}

func (m *mockLister) ListFiles(_ context.Context, _ string) ([]FileEntry, error) {
	return m.files, nil
}

func (m *mockLister) GetFile(_ context.Context, name string) (io.ReadCloser, error) {
	if data, ok := m.fileData[name]; ok {
		return io.NopCloser(strings.NewReader(data)), nil
	}
	return nil, io.ErrUnexpectedEOF
}

func (m *mockLister) DeleteModule(_ context.Context, modPath, version string) error {
	m.deleted = append(m.deleted, modPath+"@"+version)
	return nil
}

func newTestHandler() (*Handler, *mockLister) {
	ml := &mockLister{
		modules: []Module{
			{
				Path: "github.com/example/foo",
				Versions: []Version{
					{Version: "v1.0.0", Time: time.Now(), HasInfo: true, HasMod: true, HasZip: true, Size: 1024},
				},
			},
		},
		files: []FileEntry{
			{Name: "v1.0.0.info", Size: 64, ModTime: time.Now()},
			{Name: "v1.0.0.mod", Size: 128, ModTime: time.Now()},
		},
		fileData: map[string]string{
			"github.com/example/foo/@v/v1.0.0.mod": "module github.com/example/foo\n\ngo 1.21\n",
		},
	}
	h := &Handler{
		Lister: ml,
		Prefix: "/-/ui",
		Logger: slog.Default(),
	}
	return h, ml
}

func TestHandlerIndex(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
}

func TestHandlerModuleList(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/modules", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandlerModuleDetail(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/module?path=github.com/example/foo", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandlerModuleDetailMissingPath(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/module", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandlerFileView(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/file?name=github.com/example/foo/@v/v1.0.0.mod", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandlerFileViewMissingName(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/file", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandlerFileViewPathTraversal(t *testing.T) {
	h, _ := newTestHandler()
	tests := []string{
		"../../etc/passwd",
		"/etc/passwd",
	}
	for _, name := range tests {
		req := httptest.NewRequest(http.MethodGet, "/-/ui/file?name="+url.QueryEscape(name), nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("path %q: expected 400, got %d", name, w.Code)
		}
	}
}

func TestHandlerDelete(t *testing.T) {
	h, ml := newTestHandler()
	form := url.Values{"selected": {"github.com/example/foo@v1.0.0"}}
	req := httptest.NewRequest(http.MethodPost, "/-/ui/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if len(ml.deleted) != 1 {
		t.Errorf("expected 1 deletion, got %d", len(ml.deleted))
	}
}

func TestHandlerFetchNoFetcher(t *testing.T) {
	h, _ := newTestHandler()
	h.Fetcher = nil
	form := url.Values{"module": {"github.com/example/foo"}}
	req := httptest.NewRequest(http.MethodPost, "/-/ui/fetch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (with error fragment), got %d", w.Code)
	}
}

func TestHandlerFetchEmptyModule(t *testing.T) {
	h, _ := newTestHandler()
	form := url.Values{"module": {""}}
	req := httptest.NewRequest(http.MethodPost, "/-/ui/fetch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandlerNotFound(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/nonexistent", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandlerWrongPrefix(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/wrong/path", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandlerStaticFiles(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/static/htmx.min.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// Verify we don't pass bytes.Buffer (non-ReadCloser) confusion.
var _ io.ReadCloser = io.NopCloser(&bytes.Buffer{})
