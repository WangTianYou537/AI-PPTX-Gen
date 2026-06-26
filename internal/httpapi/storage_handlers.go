package httpapi

import (
	"encoding/json"
	"net/http"

	"wty5.cn/ppt-gen/internal/store"
)

type storageRequest struct {
	Storage store.Config `json:"storage"`
}

type storageResponse struct {
	Storage           *store.Config         `json:"storage,omitempty"`
	StorageConfigured bool                  `json:"storageConfigured"`
	SupportedStorage  []store.StorageOption `json:"supportedStorage"`
}

func (s *Server) handleAdminStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cfg, ok := s.stores.Config()
	var cfgPtr *store.Config
	if ok {
		cfgPtr = &cfg
	}
	writeJSON(w, http.StatusOK, storageResponse{Storage: cfgPtr, StorageConfigured: ok, SupportedStorage: store.SupportedStorageOptions()})
}

func (s *Server) handleAdminStorageTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input storageRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if err := s.stores.TestConfig(r.Context(), input.Storage); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminStorageSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input storageRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "请求 JSON 格式不正确")
		return
	}
	if err := s.stores.SwitchStore(r.Context(), input.Storage); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, ok := s.stores.Config()
	writeJSON(w, http.StatusOK, storageResponse{Storage: &cfg, StorageConfigured: ok, SupportedStorage: store.SupportedStorageOptions()})
}
