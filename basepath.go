package main

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// basePathAlphabet avoids characters that are easy to confuse.
const basePathAlphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// The access path can be changed at runtime: StripBasePath reads the current
// value for every request, so changes made in Settings take effect immediately.
var (
	basePathMu  sync.RWMutex
	basePathCur string
	basePathDir string
)

// initBasePath loads the access path and remembers the working directory so
// later changes can be persisted.
func initBasePath(dir string) (bool, error) {
	bp, created, err := LoadBasePath(dir)
	if err != nil {
		return false, err
	}
	basePathMu.Lock()
	basePathCur = bp
	basePathDir = dir
	basePathMu.Unlock()
	return created, nil
}

// currentBasePath returns the current access path, such as /xxx, or an empty string.
func currentBasePath() string {
	basePathMu.RLock()
	defer basePathMu.RUnlock()
	return basePathCur
}

// setBasePath validates and saves a new access path. An empty string removes
// the path prefix. Changes take effect immediately.
func setBasePath(raw string) (string, error) {
	bp := normalizeBasePath(raw)
	if bp != "" {
		// User-entered paths may contain any alphanumeric character plus - and _.
		// The reduced alphabet above is only for automatically generated paths.
		for _, c := range strings.TrimPrefix(bp, "/") {
			ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '-' || c == '_'
			if !ok {
				return "", fmt.Errorf("access path may contain only letters, numbers, - and _")
			}
		}
	}
	basePathMu.RLock()
	dir := basePathDir
	basePathMu.RUnlock()
	if err := os.WriteFile(filepath.Join(dir, "basepath"), []byte(strings.TrimPrefix(bp, "/")+"\n"), 0600); err != nil {
		return "", fmt.Errorf("failed to write access path: %w", err)
	}
	basePathMu.Lock()
	basePathCur = bp
	basePathMu.Unlock()
	return bp, nil
}

// LoadBasePath reads or generates a random access path such as /aB3xY9pQ.
// Like 3x-ui, the path itself adds a barrier so simple port scans do not reveal the UI.
func LoadBasePath(dir string) (string, bool, error) {
	path := filepath.Join(dir, "basepath")

	blob, err := os.ReadFile(path)
	if err == nil {
		if bp := strings.TrimSpace(string(blob)); bp != "" {
			return normalizeBasePath(bp), false, nil
		}
	} else if !os.IsNotExist(err) {
		return "", false, err
	}

	bp, err := randomBasePath(10)
	if err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(bp+"\n"), 0600); err != nil {
		return "", false, fmt.Errorf("failed to write access path: %w", err)
	}
	return normalizeBasePath(bp), true, nil
}

func randomBasePath(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = basePathAlphabet[int(v)%len(basePathAlphabet)]
	}
	return string(out), nil
}

// normalizeBasePath converts a path to /xxx form without a trailing slash.
func normalizeBasePath(bp string) string {
	bp = strings.Trim(bp, "/")
	if bp == "" {
		return ""
	}
	return "/" + bp
}

// StripBasePath removes the configured prefix before passing a request to the
// inner handler. Requests with the wrong prefix always receive 404, and the
// current base path is read on every request so changes require no restart.
func StripBasePath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := currentBasePath()
		if base == "" {
			next.ServeHTTP(w, r)
			return
		}
		switch {
		case r.URL.Path == base:
			// Add the trailing slash so relative page URLs resolve correctly.
			http.Redirect(w, r, base+"/", http.StatusTemporaryRedirect)
		case strings.HasPrefix(r.URL.Path, base+"/"):
			r.URL.Path = strings.TrimPrefix(r.URL.Path, base)
			next.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}
