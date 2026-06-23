package httpapi

import (
	"net/http"

	"wty5.cn/ppt-gen/internal/store"
	"wty5.cn/ppt-gen/internal/web"
)

func NewServer(appStore store.Store) http.Handler {
	server := New(appStore)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", server.handleHealth)
	mux.HandleFunc("/api/setup/status", server.handleSetupStatus)
	mux.HandleFunc("/api/setup/admin", server.handleSetupAdmin)
	mux.HandleFunc("/api/auth/login", server.handleLogin)
	mux.HandleFunc("/api/auth/logout", server.handleLogout)
	mux.HandleFunc("/api/auth/me", server.handleMe)
	mux.HandleFunc("/api/architect", server.withUser(server.handleArchitect))
	mux.HandleFunc("/api/generate-svg", server.withUser(server.handleGenerateSVG))
	mux.HandleFunc("/api/export-pptx", server.withUser(server.handleExportPPTX))
	mux.HandleFunc("/api/admin/users", server.withAdmin(server.handleAdminUsers))
	mux.HandleFunc("/api/admin/users/", server.withAdmin(server.handleAdminUser))
	mux.HandleFunc("/api/admin/prompts", server.withAdmin(server.handleAdminPrompts))
	mux.HandleFunc("/api/admin/prompts/reset", server.withAdmin(server.handleAdminPromptsReset))
	mux.Handle("/", web.Handler())
	return withCORS(mux)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
