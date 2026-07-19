package httpapi

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wty5.cn/ppt-gen/internal/store"
)

func (s *Server) handleUploads(w http.ResponseWriter, r *http.Request) {
	user, _ := s.currentUser(r)
	switch r.Method {
	case http.MethodGet:
		list, err := s.dataStore().ListUploadsByUser(r.Context(), user.ID, 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// do not expose local filesystem path to clients
		out := make([]map[string]any, 0, len(list))
		for _, up := range list {
			out = append(out, map[string]any{
				"id":          up.ID,
				"userId":      up.UserID,
				"filename":    up.Filename,
				"contentType": up.ContentType,
				"sizeBytes":   up.SizeBytes,
				"createdAt":   up.CreatedAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"uploads": out})
	case http.MethodPost:
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "上传文件过大或格式不正确")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "请选择文件")
			return
		}
		defer file.Close()
		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		name := sanitizeFilename(header.Filename)
		lower := strings.ToLower(name + " " + contentType)
		allowed := strings.Contains(lower, "pdf") ||
			strings.Contains(lower, "image/") ||
			strings.Contains(lower, "text/") ||
			strings.HasSuffix(lower, ".png") || strings.HasSuffix(lower, ".jpg") ||
			strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".webp") ||
			strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".md") ||
			strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".pdf")
		if !allowed {
			writeError(w, http.StatusBadRequest, "仅支持图片/PDF/文本文件")
			return
		}
		// Pre-create record to get ID.
		up, err := s.dataStore().CreateUpload(r.Context(), store.Upload{
			UserID:      user.ID,
			Filename:    name,
			ContentType: contentType,
			SizeBytes:   0,
			Path:        "",
			CreatedAt:   time.Now().UTC(),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		dir := filepath.Join("data", "uploads", user.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			_ = s.dataStore().DeleteUpload(r.Context(), up.ID)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		finalPath := filepath.Join(dir, up.ID+"_"+name)
		dst, err := os.Create(finalPath)
		if err != nil {
			_ = s.dataStore().DeleteUpload(r.Context(), up.ID)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		written, err := io.Copy(dst, io.LimitReader(file, 20<<20+1))
		_ = dst.Close()
		if err != nil || written > 20<<20 {
			_ = os.Remove(finalPath)
			_ = s.dataStore().DeleteUpload(r.Context(), up.ID)
			writeError(w, http.StatusBadRequest, "文件保存失败或超过 20MB")
			return
		}
		// Update record fields via delete+create to keep interface small.
		_ = s.dataStore().DeleteUpload(r.Context(), up.ID)
		up.SizeBytes = written
		up.Path = finalPath
		up, err = s.dataStore().CreateUpload(r.Context(), up)
		if err != nil {
			_ = os.Remove(finalPath)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          up.ID,
			"userId":      up.UserID,
			"filename":    up.Filename,
			"contentType": up.ContentType,
			"sizeBytes":   up.SizeBytes,
			"createdAt":   up.CreatedAt,
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "..", ".")
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		default:
			return r
		}
	}, name)
	if strings.TrimSpace(name) == "" {
		return "file"
	}
	return name
}
