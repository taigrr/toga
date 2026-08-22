package web

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func setupTestCache(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create two modules with versions.
	modules := map[string][]string{
		"github.com/example/foo": {"v1.0.0", "v1.1.0"},
		"github.com/example/bar": {"v2.0.0"},
	}
	for mod, versions := range modules {
		for _, ver := range versions {
			dir := filepath.Join(root, filepath.FromSlash(mod), "@v")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, ext := range []string{".info", ".mod", ".zip"} {
				if err := os.WriteFile(filepath.Join(dir, ver+ext), []byte("test"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return root
}

func TestDiskListerListModules(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	page, err := lister.ListModules(ctx, "", "", 50)
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(page.Modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(page.Modules))
	}
	if page.Total != 2 {
		t.Errorf("expected total 2, got %d", page.Total)
	}
}

func TestDiskListerPagination(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	page1, err := lister.ListModules(ctx, "", "", 1)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(page1.Modules))
	}
	if page1.NextCursor == "" {
		t.Fatal("expected non-empty cursor")
	}

	page2, err := lister.ListModules(ctx, page1.NextCursor, "", 1)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Modules) != 1 {
		t.Fatalf("expected 1 module on page2, got %d", len(page2.Modules))
	}
	if page2.NextCursor != "" {
		t.Errorf("expected empty cursor on last page, got %q", page2.NextCursor)
	}
}

func TestDiskListerQueryFilter(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	page, err := lister.ListModules(ctx, "", "foo", 50)
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(page.Modules) != 1 {
		t.Fatalf("expected 1 module matching 'foo', got %d", len(page.Modules))
	}
	if page.Modules[0].Path != "github.com/example/foo" {
		t.Errorf("expected foo module, got %q", page.Modules[0].Path)
	}
}

func TestDiskListerVersionDetails(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	page, err := lister.ListModules(ctx, "", "foo", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Modules) == 0 {
		t.Fatal("no modules")
	}
	mod := page.Modules[0]
	if len(mod.Versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(mod.Versions))
	}
	for _, v := range mod.Versions {
		if !v.HasInfo || !v.HasMod || !v.HasZip {
			t.Errorf("version %s missing files: info=%v mod=%v zip=%v", v.Version, v.HasInfo, v.HasMod, v.HasZip)
		}
	}
}

func TestDiskListerListFiles(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	files, err := lister.ListFiles(ctx, "github.com/example/bar")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 3 { // .info, .mod, .zip
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestDiskListerGetFile(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	rc, err := lister.GetFile(ctx, "github.com/example/bar/@v/v2.0.0.info")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	rc.Close()
}

func TestDiskListerGetFileRejectsUnsafePaths(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	for _, name := range []string{
		"../outside",
		"github.com/example/foo/../../outside",
		"/github.com/example/foo/@v/v1.0.0.info",
		"github.com//example/foo/@v/v1.0.0.info",
	} {
		if _, err := lister.GetFile(ctx, name); err == nil {
			t.Fatalf("expected error for unsafe path %q", name)
		}
	}
}

func TestDiskListerListFilesRejectsUnsafeModulePath(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}

	for _, modulePath := range []string{"../outside", "github.com/example/../outside", "/github.com/example/foo"} {
		if _, err := lister.ListFiles(context.Background(), modulePath); err == nil {
			t.Fatalf("expected error for unsafe module path %q", modulePath)
		}
	}
}

func TestDiskListerDeleteVersion(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	if err := lister.DeleteModule(ctx, "github.com/example/bar", "v2.0.0"); err != nil {
		t.Fatalf("DeleteModule: %v", err)
	}

	files, _ := lister.ListFiles(ctx, "github.com/example/bar")
	if len(files) != 0 {
		t.Errorf("expected 0 files after delete, got %d", len(files))
	}
}

func TestDiskListerDeleteModuleRejectsUnsafePaths(t *testing.T) {
	root := setupTestCache(t)
	outside := filepath.Join(filepath.Dir(root), "outside.info")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	lister := &DiskLister{Root: root}
	for _, tt := range []struct {
		modulePath string
		version    string
	}{
		{modulePath: "../outside"},
		{modulePath: "github.com/example/foo/../../outside"},
		{modulePath: "github.com/example/foo", version: "../outside"},
		{modulePath: "github.com/example/foo", version: "v1.0.0/../../outside"},
	} {
		if err := lister.DeleteModule(context.Background(), tt.modulePath, tt.version); err == nil {
			t.Fatalf("expected error for module path %q version %q", tt.modulePath, tt.version)
		}
	}

	if _, err := os.Stat(outside); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("unsafe delete removed %s", outside)
		}
		t.Fatal(err)
	}
}

func TestDiskListerDeleteModule(t *testing.T) {
	root := setupTestCache(t)
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	if err := lister.DeleteModule(ctx, "github.com/example/foo", ""); err != nil {
		t.Fatalf("DeleteModule: %v", err)
	}

	page, _ := lister.ListModules(ctx, "", "foo", 50)
	if len(page.Modules) != 0 {
		t.Errorf("expected 0 modules after delete, got %d", len(page.Modules))
	}
}

func TestDiskListerEmptyRoot(t *testing.T) {
	root := t.TempDir()
	lister := &DiskLister{Root: root}
	ctx := context.Background()

	page, err := lister.ListModules(ctx, "", "", 50)
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(page.Modules) != 0 {
		t.Errorf("expected 0 modules, got %d", len(page.Modules))
	}
}

func TestDiskListerListFilesMissing(t *testing.T) {
	root := t.TempDir()
	lister := &DiskLister{Root: root}

	files, err := lister.ListFiles(context.Background(), "nonexistent/mod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil, got %v", files)
	}
}
