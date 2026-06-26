package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"wty5.cn/ppt-gen/internal/auth"
)

var debugEnabled atomic.Bool

func SetDebug(enabled bool) {
	debugEnabled.Store(enabled)
}

type requestIDContextKey struct{}

func requestIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return value
	}
	return ""
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		recorder := &statusRecorder{ResponseWriter: w}
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)

		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("http panic request_id=%s method=%s path=%s panic=%v stack=%s", requestID, r.Method, r.URL.RequestURI(), recovered, string(debug.Stack()))
				if recorder.status == 0 {
					writeJSON(recorder, http.StatusInternalServerError, errorResponse{Error: "服务器内部错误", Detail: fmt.Sprint(recovered), RequestID: requestID})
				}
			}
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			duration := time.Since(started)
			if debugEnabled.Load() || status >= 500 {
				log.Printf("http request request_id=%s method=%s path=%s status=%d duration=%s remote=%s", requestID, r.Method, r.URL.RequestURI(), status, duration, r.RemoteAddr)
			}
		}()

		next.ServeHTTP(recorder, r.WithContext(ctx))
	})
}

func newRequestID() string {
	token, _, err := auth.NewToken()
	if err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if len(token) > 16 {
		return token[:16]
	}
	return token
}
