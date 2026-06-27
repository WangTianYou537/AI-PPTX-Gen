package web

import (
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// gzipResponseWriter 包装标准 http.ResponseWriter 以支持动态压缩
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	// 由于压缩后文件大小改变，需要移除原始的 Content-Length 触发分块传输
	w.ResponseWriter.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	w.ResponseWriter.Header().Del("Content-Length")
	return w.Writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if f, ok := w.Writer.(*gzip.Writer); ok {
		_ = f.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withGzipMiddleware 动态压缩中间件
func withGzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 客户端不支持 gzip 则直接透传
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// 过滤无需压缩的媒体或字体格式
		ext := strings.ToLower(r.URL.Path)
		if strings.HasSuffix(ext, ".png") ||
			strings.HasSuffix(ext, ".jpg") ||
			strings.HasSuffix(ext, ".jpeg") ||
			strings.HasSuffix(ext, ".gif") ||
			strings.HasSuffix(ext, ".woff2") ||
			strings.HasSuffix(ext, ".mp4") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()

		gzw := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next.ServeHTTP(gzw, r)
	})
}

// setCacheControl 根据文件类型设置合理的缓存头
func setCacheControl(w http.ResponseWriter, requestPath string) {
	// 匹配前端带 Hash 的静态资源文件夹 (Vite 的 assets/, Next.js 的 _next/ 或 static/)
	if strings.Contains(requestPath, "assets/") ||
		strings.Contains(requestPath, "_next/") ||
		strings.Contains(requestPath, "static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}

	// HTML 入口文件绝不能缓存
	if strings.HasSuffix(requestPath, ".html") || requestPath == "" {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	}
}

func Handler() http.Handler {
	staticFS, err := fs.Sub(Assets, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend assets are unavailable", http.StatusInternalServerError)
		})
	}

	fileServer := http.FileServer(http.FS(staticFS))
	indexHTML, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend index.html is unavailable", http.StatusInternalServerError)
		})
	}

	serveIndex := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// 入口 HTML 设置不缓存
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		_, _ = w.Write(indexHTML)
	}

	// 主处理器逻辑
	coreHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		requestPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requestPath == "." {
			requestPath = ""
		}

		if requestPath == "" {
			serveIndex(w)
			return
		}

		candidates := []string{requestPath}
		if !strings.HasSuffix(requestPath, ".html") {
			candidates = append(candidates, requestPath+".html", path.Join(requestPath, "index.html"))
		}

		for _, servePath := range candidates {
			info, err := fs.Stat(staticFS, servePath)
			if err != nil {
				continue
			}
			if info.IsDir() {
				indexPath := path.Join(servePath, "index.html")
				if _, err := fs.Stat(staticFS, indexPath); err == nil {
					setCacheControl(w, indexPath) // 设置缓存
					r.URL.Path = "/" + indexPath
					fileServer.ServeHTTP(w, r)
					return
				}
				continue
			}

			setCacheControl(w, servePath) // 设置缓存
			r.URL.Path = "/" + servePath
			fileServer.ServeHTTP(w, r)
			return
		}

		serveIndex(w)
	})

	// 将主处理器包装在 Gzip 中间件中返回
	return withGzipMiddleware(coreHandler)
}
