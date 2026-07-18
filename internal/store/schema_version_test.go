package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLSchemaVersionIsRecorded(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v.db")
	s, err := OpenConfiguredStore(context.Background(), Config{Kind: StorageSQLite, Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sqlStore, ok := s.(*SQLStore)
	if !ok {
		t.Fatalf("expected *SQLStore, got %T", s)
	}
	version, err := sqlStore.schemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version < 2 {
		t.Fatalf("expected schema version >= 2, got %d", version)
	}
}
