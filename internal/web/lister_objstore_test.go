package web

import (
	"context"
	"io"
	"strings"
	"testing"

	s3cacher "github.com/taigrr/toga/internal/storage/s3"
	"github.com/taigrr/toga/internal/storage/s3test"
)

const objstoreTestBucket = "toga-lister-test"

func setupObjectStoreLister(t *testing.T) *ObjectStoreLister {
	t.Helper()
	ctx := context.Background()
	srv := s3test.Start(t)

	cacher, err := s3cacher.New(ctx, s3cacher.Config{
		Endpoint:       srv.Endpoint,
		Key:            s3test.AccessKey,
		Secret:         s3test.SecretKey,
		Bucket:         objstoreTestBucket,
		Region:         s3test.Region,
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("new s3 cacher: %v", err)
	}
	if err := cacher.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}

	modules := map[string][]string{
		"github.com/example/foo": {"v1.0.0", "v1.1.0"},
		"github.com/example/bar": {"v2.0.0"},
	}
	for mod, versions := range modules {
		for _, ver := range versions {
			for _, ext := range []string{".info", ".mod", ".zip"} {
				key := mod + versionPrefix + ver + ext
				if err := cacher.Put(ctx, key, strings.NewReader("test")); err != nil {
					t.Fatalf("put %s: %v", key, err)
				}
			}
		}
	}

	return &ObjectStoreLister{Client: cacher.S3Client(), Bucket: cacher.BucketName()}
}

func TestObjectStoreListerListModules(t *testing.T) {
	lister := setupObjectStoreLister(t)
	ctx := context.Background()

	page, err := lister.ListModules(ctx, "", "", 50)
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(page.Modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(page.Modules))
	}
	if page.Modules[0].Path != "github.com/example/bar" {
		t.Errorf("expected bar first, got %s", page.Modules[0].Path)
	}
	foo := page.Modules[1]
	if foo.Path != "github.com/example/foo" {
		t.Fatalf("expected foo second, got %s", foo.Path)
	}
	if len(foo.Versions) != 2 {
		t.Fatalf("expected 2 versions for foo, got %d", len(foo.Versions))
	}
	v := foo.Versions[0]
	if !v.HasInfo || !v.HasMod || !v.HasZip || v.Size != 4 {
		t.Errorf("unexpected version detail: %+v", v)
	}
}

func TestObjectStoreListerPagination(t *testing.T) {
	lister := setupObjectStoreLister(t)
	ctx := context.Background()

	page1, err := lister.ListModules(ctx, "", "", 1)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Modules) != 1 || page1.NextCursor == "" {
		t.Fatalf("expected 1 module with cursor, got %d (%q)", len(page1.Modules), page1.NextCursor)
	}

	page2, err := lister.ListModules(ctx, page1.NextCursor, "", 1)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Modules) != 1 || page2.NextCursor != "" {
		t.Fatalf("expected 1 module without cursor, got %d (%q)", len(page2.Modules), page2.NextCursor)
	}
	if page2.Modules[0].Path == page1.Modules[0].Path {
		t.Error("pages returned the same module")
	}
}

func TestObjectStoreListerQuery(t *testing.T) {
	lister := setupObjectStoreLister(t)

	page, err := lister.ListModules(context.Background(), "", "FOO", 50)
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(page.Modules) != 1 || page.Modules[0].Path != "github.com/example/foo" {
		t.Fatalf("expected only foo, got %+v", page.Modules)
	}
}

func TestObjectStoreListerFilesAndDelete(t *testing.T) {
	lister := setupObjectStoreLister(t)
	ctx := context.Background()
	mod := "github.com/example/foo"

	files, err := lister.ListFiles(ctx, mod)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 6 {
		t.Fatalf("expected 6 files, got %d", len(files))
	}

	rc, err := lister.GetFile(ctx, mod+versionPrefix+"v1.0.0.mod")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "test" {
		t.Errorf("expected %q, got %q", "test", data)
	}

	if err := lister.DeleteModule(ctx, mod, "v1.0.0"); err != nil {
		t.Fatalf("DeleteModule version: %v", err)
	}
	files, _ = lister.ListFiles(ctx, mod)
	if len(files) != 3 {
		t.Fatalf("expected 3 files after version delete, got %d", len(files))
	}

	if err := lister.DeleteModule(ctx, mod, ""); err != nil {
		t.Fatalf("DeleteModule all: %v", err)
	}
	files, _ = lister.ListFiles(ctx, mod)
	if len(files) != 0 {
		t.Fatalf("expected 0 files after module delete, got %d", len(files))
	}
}
