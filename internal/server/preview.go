package server

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/eargollo/ditto/internal/db"
)

// previewURL returns the URL to preview a file by path (for use in templates).
func previewURL(path string) string {
	return "/preview?path=" + url.QueryEscape(path)
}

// isPreviewable returns true if the file path has an image or video extension (for templates).
func isPreviewable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := previewableExts[ext]
	return ok
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".webm": true, ".mkv": true, ".mov": true, ".avi": true, ".m4v": true,
}

// hasImageExt returns true if the path has an image extension (for templates).
func hasImageExt(path string) bool { return imageExts[strings.ToLower(filepath.Ext(path))] }

// hasVideoExt returns true if the path has a video extension (for templates).
func hasVideoExt(path string) bool { return videoExts[strings.ToLower(filepath.Ext(path))] }

// previewableExts is a set of lowercased extensions for which we serve previews (images and videos).
var previewableExts = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
	".mp4": "video/mp4", ".webm": "video/webm", ".mkv": "video/x-matroska",
	".mov": "video/quicktime", ".avi": "video/x-msvideo", ".m4v": "video/x-m4v",
}

// pathUnderRoot returns true if cleanPath is exactly root or is under root.
// root and cleanPath must be cleaned and absolute (same format).
func pathUnderRoot(cleanPath, root string) bool {
	if root == "" || root == "." {
		return true
	}
	if cleanPath == root {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(root, sep) {
		root = root + sep
	}
	return strings.HasPrefix(cleanPath, root)
}

func (s *Server) handlePreview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rawPath := r.URL.Query().Get("path")
		if rawPath == "" {
			http.Error(w, "path required", http.StatusBadRequest)
			return
		}
		path, err := decodePath(rawPath)
		if err != nil {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		absPath = filepath.Clean(absPath)

		roots, err := db.ListScanRoots(r.Context(), s.dbForRead())
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		var allowed bool
		for _, root := range roots {
			absRoot, err := filepath.Abs(filepath.Clean(root.Path))
			if err != nil {
				continue
			}
			if pathUnderRoot(absPath, absRoot) {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		ext := strings.ToLower(filepath.Ext(absPath))
		contentType, ok := previewableExts[ext]
		if !ok {
			http.Error(w, "preview not supported for this file type", http.StatusBadRequest)
			return
		}

		f, err := os.Open(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.ServeContent(w, r, filepath.Base(absPath), info.ModTime(), f)
	}
}

// decodePath decodes a path that was URL-encoded. Returns error if path contains ".." after decode.
func decodePath(encoded string) (string, error) {
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		return "", err
	}
	if strings.Contains(decoded, "..") {
		return "", errors.New("path traversal not allowed")
	}
	return decoded, nil
}
