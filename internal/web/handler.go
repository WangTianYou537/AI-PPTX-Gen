package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

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
		_, _ = w.Write(indexHTML)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		requestPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requestPath == "." {
			requestPath = ""
		}

		servePath := requestPath
		if servePath == "" {
			serveIndex(w)
			return
		}

		info, err := fs.Stat(staticFS, servePath)
		if err == nil {
			if info.IsDir() {
				indexPath := path.Join(servePath, "index.html")
				if _, err := fs.Stat(staticFS, indexPath); err == nil {
					r.URL.Path = "/" + indexPath
					fileServer.ServeHTTP(w, r)
					return
				}
			} else {
				r.URL.Path = "/" + servePath
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		serveIndex(w)
	})
}
