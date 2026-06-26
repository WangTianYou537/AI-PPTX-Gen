package httpapi

import (
	"net/http"

	"wty5.cn/ppt-gen/internal/store"
	"wty5.cn/ppt-gen/internal/web"
)

func NewServer(appStore store.Store) http.Handler {
	return NewServerWithStoreManager(NewStoreManager(appStore, store.Config{Kind: store.StorageJSON, Path: "data/app.json"}, store.DefaultConfigPath()))
}

func NewServerWithStoreManager(manager *StoreManager) http.Handler {
	server := NewWithStoreManager(manager)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", server.handleHealth)
	mux.HandleFunc("/api/setup/status", server.handleSetupStatus)
	mux.HandleFunc("/api/setup/admin", server.handleSetupAdmin)
	mux.HandleFunc("/api/setup/storage/test", server.handleSetupStorageTest)
	mux.HandleFunc("/api/auth/login", server.handleLogin)
	mux.HandleFunc("/api/auth/register", server.handleRegister)
	mux.HandleFunc("/api/auth/logout", server.handleLogout)
	mux.HandleFunc("/api/auth/me", server.handleMe)
	mux.HandleFunc("/api/me/quota", server.withUser(server.handleMyQuota))
	mux.HandleFunc("/api/architect", server.withUser(server.handleArchitect))
	mux.HandleFunc("/api/generate-svg", server.withUser(server.handleGenerateSVG))
	mux.HandleFunc("/api/export-pptx", server.withUser(server.handleExportPPTX))
	mux.HandleFunc("/api/admin/users", server.withAdmin(server.handleAdminUsers))
	mux.HandleFunc("/api/admin/users/", server.withAdmin(server.handleAdminUser))
	mux.HandleFunc("/api/admin/groups", server.withAdmin(server.handleAdminGroups))
	mux.HandleFunc("/api/admin/groups/", server.withAdmin(server.handleAdminGroup))
	mux.HandleFunc("/api/admin/settings", server.withAdmin(server.handleAdminSettings))
	mux.HandleFunc("/api/admin/prompts", server.withAdmin(server.handleAdminPrompts))
	mux.HandleFunc("/api/admin/prompts/reset", server.withAdmin(server.handleAdminPromptsReset))
	mux.HandleFunc("/api/admin/storage", server.withAdmin(server.handleAdminStorage))
	mux.HandleFunc("/api/admin/storage/test", server.withAdmin(server.handleAdminStorageTest))
	mux.HandleFunc("/api/admin/storage/switch", server.withAdmin(server.handleAdminStorageSwitch))
	mux.Handle("/", web.Handler())
	return withRequestLogging(withCORS(mux))
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
