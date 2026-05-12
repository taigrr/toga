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

func TestHandlerDeleteSingleModuleForm(t *testing.T) {
	h, ml := newTestHandler()
	form := url.Values{
		"module":  {"github.com/example/foo"},
		"version": {"v1.0.0"},
	}
	req := httptest.NewRequest(http.MethodPost, "/-/ui/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := len(ml.deleted); got != 1 {
		t.Fatalf("expected 1 deletion, got %d", got)
	}
	if ml.deleted[0] != "github.com/example/foo@v1.0.0" {
		t.Fatalf("expected deleted module to include version, got %q", ml.deleted[0])
	}
}

func TestHandlerDeleteMissingModule(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/-/ui/delete", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
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

func TestHandlerFetchStatusNotFound(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/fetch-status?id=missing", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandlerFetchStatusInProgress(t *testing.T) {
	h, _ := newTestHandler()
	jobID := "job-in-progress"
	job := &fetchJob{done: make(chan struct{})}
	job.count.Store(2)
	job.latest.Store("github.com/example/bar@v1.2.3")
	activeJobs.Store(jobID, job)
	defer activeJobs.Delete(jobID)

	req := httptest.NewRequest(http.MethodGet, "/-/ui/fetch-status?id="+jobID, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<strong>2</strong> modules fetched") {
		t.Fatalf("expected progress count in response body, got %q", body)
	}
	if !strings.Contains(body, "github.com/example/bar@v1.2.3") {
		t.Fatalf("expected latest module in response body, got %q", body)
	}
}

func TestHandlerFetchStatusDone(t *testing.T) {
	h, _ := newTestHandler()
	jobID := "job-done"
	job := &fetchJob{done: make(chan struct{})}
	job.results = []FetchResult{{Module: "github.com/example/foo@v1.0.0"}}
	close(job.done)
	activeJobs.Store(jobID, job)
	defer activeJobs.Delete(jobID)

	req := httptest.NewRequest(http.MethodGet, "/-/ui/fetch-status?id="+jobID, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "github.com/example/foo@v1.0.0") {
		t.Fatalf("expected finished module in response body, got %q", w.Body.String())
	}
}

func TestHandlerDownload(t *testing.T) {
	h, _ := newTestHandler()
	name := "github.com/example/foo/@v/v1.0.0.mod"
	req := httptest.NewRequest(http.MethodGet, "/-/ui/download?name="+url.QueryEscape(name), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Disposition"); got != "attachment; filename=\"v1.0.0.mod\"" {
		t.Fatalf("unexpected content disposition %q", got)
	}
	if got := w.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("unexpected content type %q", got)
	}
}

func TestHandlerDownloadZip(t *testing.T) {
	h, ml := newTestHandler()
	ml.fileData["github.com/example/foo/@v/v1.0.0.zip"] = "zip-data"
	name := "github.com/example/foo/@v/v1.0.0.zip"
	req := httptest.NewRequest(http.MethodGet, "/-/ui/download?name="+url.QueryEscape(name), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("unexpected content type %q", got)
	}
}

func TestHandlerDownloadMissingName(t *testing.T) {
	h, _ := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/-/ui/download", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlerDownloadPathTraversal(t *testing.T) {
	h, _ := newTestHandler()
	for _, name := range []string{"../../etc/passwd", "/etc/passwd"} {
		req := httptest.NewRequest(http.MethodGet, "/-/ui/download?name="+url.QueryEscape(name), nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("path %q: expected 400, got %d", name, w.Code)
		}
	}
}

// Verify we don't pass bytes.Buffer (non-ReadCloser) confusion.
var _ io.ReadCloser = io.NopCloser(&bytes.Buffer{})
