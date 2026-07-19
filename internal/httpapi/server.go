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
	mux.HandleFunc("/api/bootstrap", server.handleBootstrap)
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
	mux.HandleFunc("/api/generate-svg-slide", server.withUser(server.handleGenerateOneSVG))
	mux.HandleFunc("/api/jobs/outline", server.withUser(server.handleCreateOutlineJob))
	mux.HandleFunc("/api/jobs/svg", server.withUser(server.handleCreateSVGJob))
	mux.HandleFunc("/api/jobs/", server.withUser(server.handleJob))
	mux.HandleFunc("/api/jobs", server.withUser(server.handleJobs))
	mux.HandleFunc("/api/export-pptx", server.withUser(server.handleExportPPTX))
	mux.HandleFunc("/api/admin/users", server.withAdmin(server.handleAdminUsers))
	mux.HandleFunc("/api/admin/users/", server.withAdmin(server.handleAdminUser))
	mux.HandleFunc("/api/admin/groups", server.withAdmin(server.handleAdminGroups))
	mux.HandleFunc("/api/admin/groups/", server.withAdmin(server.handleAdminGroup))
	mux.HandleFunc("/api/admin/settings", server.withAdmin(server.handleAdminSettings))
	mux.HandleFunc("/api/admin/prompts", server.withAdmin(server.handleAdminPrompts))
	mux.HandleFunc("/api/admin/providers", server.withAdmin(server.handleAdminProviders))
	mux.HandleFunc("/api/admin/agent/workflow", server.withAdmin(server.handleAdminAgentWorkflow))
	mux.HandleFunc("/api/admin/agent/workflow/reset", server.withAdmin(server.handleAdminAgentWorkflowReset))
	mux.HandleFunc("/api/uploads", server.withUser(server.handleUploads))
	mux.HandleFunc("/api/admin/providers/", server.withAdmin(server.handleAdminProvider))
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
