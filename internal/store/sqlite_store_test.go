package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteFreshStoreHasGroupAndRequestJSONColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")

	s, err := OpenConfiguredStore(context.Background(), Config{Kind: StorageSQLite, Path: dbPath})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	needsSetup, err := s.NeedsSetup(context.Background())
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if !needsSetup {
		t.Fatalf("expected fresh store to need setup")
	}

	settings, err := s.GetPromptSettings(context.Background())
	if err != nil {
		t.Fatalf("GetPromptSettings: %v", err)
	}
	// Ensure request JSON fields exist and round-trip.
	settings.Architect.SystemPrompt = "architect"
	settings.SVG.SystemPrompt = "svg"
	settings.Architect.ModelConfig = ModelConfig{Provider: "openai", APIKey: "k", BaseURL: "https://example.com", Model: "m"}
	settings.SVG.ModelConfig = ModelConfig{Provider: "openai", APIKey: "k", BaseURL: "https://example.com", Model: "m"}
	settings.Architect.RequestJSON = `{"temperature":0.1}`
	settings.SVG.RequestJSON = `{"max_tokens":100}`
	if err := s.SavePromptSettings(context.Background(), settings); err != nil {
		t.Fatalf("SavePromptSettings: %v", err)
	}
	loaded, err := s.GetPromptSettings(context.Background())
	if err != nil {
		t.Fatalf("reload GetPromptSettings: %v", err)
	}
	if loaded.Architect.RequestJSON != `{"temperature":0.1}` {
		t.Fatalf("architect request json not persisted: %q", loaded.Architect.RequestJSON)
	}
	if loaded.SVG.RequestJSON != `{"max_tokens":100}` {
		t.Fatalf("svg request json not persisted: %q", loaded.SVG.RequestJSON)
	}

	groups, err := s.ListUserGroups(context.Background())
	if err != nil {
		t.Fatalf("ListUserGroups: %v", err)
	}
	if len(groups) == 0 {
		t.Fatalf("expected default user group")
	}
	if groups[0].SlideConcurrencyLimit < 1 {
		t.Fatalf("expected default concurrency >= 1, got %d", groups[0].SlideConcurrencyLimit)
	}
}
