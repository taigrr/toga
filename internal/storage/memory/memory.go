// Package memory implements goproxy.Cacher using an in-memory map.
// Useful for development and ephemeral deployments.
// Not suitable for production — all data is lost on restart.
package memory

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"sync"
	"time"
)

// Cacher is a thread-safe in-memory module cache.
type Cacher struct {
	mu      sync.RWMutex
	entries map[string]entry
}

type entry struct {
	data    []byte
	created time.Time
}

// New creates an in-memory Cacher.
func New() *Cacher {
	return &Cacher{entries: make(map[string]entry)}
}

type readCloser struct {
	*bytes.Reader
	created time.Time
}

func (rc *readCloser) Close() error            { return nil }
func (rc *readCloser) LastModified() time.Time { return rc.created }
func (rc *readCloser) Size() int64             { return int64(rc.Len()) }

// Get implements goproxy.Cacher.
func (c *Cacher) Get(_ context.Context, name string) (io.ReadCloser, error) {
	c.mu.RLock()
	e, ok := c.entries[name]
	c.mu.RUnlock()
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &readCloser{Reader: bytes.NewReader(e.data), created: e.created}, nil
}

// Put implements goproxy.Cacher.
func (c *Cacher) Put(_ context.Context, name string, content io.ReadSeeker) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.entries[name] = entry{data: data, created: time.Now()}
	c.mu.Unlock()
	return nil
}
