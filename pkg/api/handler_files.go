package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangkun19921001/PP-Claw/config"
)

func (s *APIServer) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	maxSize := int64(50 << 20) // 50MB
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	if err := r.ParseMultipartForm(maxSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}

	ws := config.ExpandHome(s.cfg.Agents.Defaults.Workspace)
	uploadDir := filepath.Join(ws, "uploads")
	os.MkdirAll(uploadDir, 0755)

	var uploaded []string

	for _, fileHeaders := range r.MultipartForm.File {
		for _, fh := range fileHeaders {
			src, err := fh.Open()
			if err != nil {
				continue
			}

			ext := filepath.Ext(fh.Filename)
			name := fmt.Sprintf("%d_%s%s", time.Now().UnixMilli(), strings.TrimSuffix(fh.Filename, ext), ext)
			destPath := filepath.Join(uploadDir, name)

			dst, err := os.Create(destPath)
			if err != nil {
				src.Close()
				continue
			}
			io.Copy(dst, src)
			dst.Close()
			src.Close()

			uploaded = append(uploaded, name)
		}
	}

	writeOK(w, map[string]any{
		"files": uploaded,
		"count": len(uploaded),
	})
}

func (s *APIServer) handleFileServe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	filePath := pathSuffix(r, "/api/v1/files/")
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "file path required")
		return
	}

	// Extract just filename — handles both absolute paths (old data) and plain names
	name := filepath.Base(filePath)
	if name == "." || name == "/" {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}

	ws := config.ExpandHome(s.cfg.Agents.Defaults.Workspace)
	fullPath := filepath.Join(ws, "uploads", name)

	http.ServeFile(w, r, fullPath)
}
