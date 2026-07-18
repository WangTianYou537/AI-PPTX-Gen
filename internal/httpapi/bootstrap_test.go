package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"wty5.cn/ppt-gen/internal/auth"
	"wty5.cn/ppt-gen/internal/store"
)

func TestBootstrapAnonymousAndAuthenticated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	appStore, err := store.OpenConfiguredStore(context.Background(), store.Config{Kind: store.StorageSQLite, Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewStoreManager(appStore, store.Config{Kind: store.StorageSQLite, Path: dbPath}, filepath.Join(dir, "config.yml"))
	server := NewWithStoreManager(manager)

	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	rec := httptest.NewRecorder()
	server.handleBootstrap(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var boot bootstrapResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &boot); err != nil {
		t.Fatal(err)
	}
	if !boot.NeedsSetup || !boot.StorageConfigured {
		t.Fatalf("unexpected bootstrap: %+v", boot)
	}

	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user, err := appStore.CreateUser(context.Background(), store.CreateUserInput{
		Email: "admin@example.com", Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, GroupID: store.DefaultUserGroupID,
	})
	if err != nil {
		t.Fatal(err)
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := appStore.CreateSession(context.Background(), store.Session{
		ID: "sess1", UserID: user.ID, TokenHash: tokenHash, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	req.AddCookie(&http.Cookie{Name: server.sessionCookie, Value: token})
	rec = httptest.NewRecorder()
	server.handleBootstrap(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}
	boot = bootstrapResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &boot); err != nil {
		t.Fatal(err)
	}
	if boot.NeedsSetup {
		t.Fatal("expected needsSetup=false after admin exists")
	}
	if boot.User == nil || boot.User.Email != "admin@example.com" {
		t.Fatalf("expected authenticated user, got %+v", boot.User)
	}
	if boot.Quota == nil {
		t.Fatal("expected quota for authenticated user")
	}
}
