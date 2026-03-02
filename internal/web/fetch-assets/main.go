// Command fetch-assets downloads htmx, Alpine.js, Pico CSS, and highlight.js
// into internal/web/static/ for embedding into the toga binary.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

var assets = map[string]string{
	"htmx.min.js":         "https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js",
	"alpine.min.js":       "https://unpkg.com/alpinejs@3.14.8/dist/cdn.min.js",
	"pico.min.css":        "https://unpkg.com/@picocss/pico@2.1.1/css/pico.min.css",
	"highlight.min.js":    "https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.11.1/highlight.min.js",
	"highlight-go.min.js": "https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.11.1/languages/go.min.js",
	"github-dark.min.css": "https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.11.1/styles/github-dark.min.css",
}

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	staticDir := filepath.Join(filepath.Dir(thisFile), "..", "static")

	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}

	for name, url := range assets {
		dest := filepath.Join(staticDir, name)
		if _, err := os.Stat(dest); err == nil {
			fmt.Printf("skip %s (exists)\n", name)
			continue
		}
		fmt.Printf("fetch %s\n", name)
		if err := download(url, dest); err != nil {
			fmt.Fprintf(os.Stderr, "download %s: %v\n", name, err)
			os.Exit(1)
		}
	}
}

func download(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
