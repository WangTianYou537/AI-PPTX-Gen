package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
)

type errorResponse struct {
	Error     string `json:"error"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("http json encode error status=%d err=%v", status, err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	detail := ""
	if debugEnabled.Load() {
		detail = message
	}
	requestID := ""
	if requestGetter, ok := w.(interface{ Header() http.Header }); ok {
		requestID = requestGetter.Header().Get("X-Request-ID")
	}
	if debugEnabled.Load() || status >= 500 {
		log.Printf("http error request_id=%s status=%d error=%s", requestID, status, message)
	}
	writeJSON(w, status, errorResponse{Error: message, Detail: detail, RequestID: requestID})
}
