// Package web provides the toga web UI for browsing cached modules and viewing logs.
package web

import (
	"context"
	"io"
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

// versionPrefix is the path separator between module path and version files.
const versionPrefix = "/@v/"

// versionExts are the file extensions for a cached module version.
var versionExts = []string{".info", ".mod", ".zip"}

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
