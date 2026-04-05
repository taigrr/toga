package disk

import (
	"testing"
)

func TestNew(t *testing.T) {
	cfg := Config{RootPath: "/tmp/toga-test"}
	cacher := New(cfg)

	// DirCacher is a string type, so we can compare directly.
	if string(cacher) != "/tmp/toga-test" {
		t.Errorf("expected root path %q, got %q", "/tmp/toga-test", string(cacher))
	}
}

func TestNewEmptyPath(t *testing.T) {
	cfg := Config{RootPath: ""}
	cacher := New(cfg)

	if string(cacher) != "" {
		t.Errorf("expected empty root path, got %q", string(cacher))
	}
}
