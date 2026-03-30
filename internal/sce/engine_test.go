package sce

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	_ "modernc.org/sqlite"
)

func TestHydrateContextReturnsRelevantDBHits(t *testing.T) {
	engine := newTestEngine(t)
	defer engine.Close()

	now := time.Now().UTC()
	insertUserMemoryRow(t, engine.db, "user", "preference", "cache invalidation should keep middleware stable", "test", `["cache","middleware"]`, "high", now.Add(-5*time.Minute))
	insertSymbolRow(t, engine.db, "internal/api/server.go", "func", "BodyCacheMiddleware", "api.BodyCacheMiddleware", 120, "shared body cache for v1 requests", `["middleware","cache"]`)
	insertFileRow(t, engine.db, "internal/sce/engine.go", "sce query optimization and candidate filtering", `["sce","query","cache"]`)

	result, err := engine.HydrateContext("cache middleware", 6)
	if err != nil {
		t.Fatalf("HydrateContext() error = %v", err)
	}
	if len(result.RelevantHits) == 0 {
		t.Fatalf("expected relevant hits, got %+v", result)
	}
	if !containsHitKind(result.RelevantHits, "user-memory:preference") {
		t.Fatalf("expected user-memory hit, got %+v", result.RelevantHits)
	}
	if !containsHitKind(result.RelevantHits, "code:symbol") {
		t.Fatalf("expected code:symbol hit, got %+v", result.RelevantHits)
	}
	if !containsHitKind(result.RelevantHits, "code:file") {
		t.Fatalf("expected code:file hit, got %+v", result.RelevantHits)
	}
}

func TestBuildAppliedUserMemoryFindsOlderMatchingRows(t *testing.T) {
	engine := newTestEngine(t)
	defer engine.Close()

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		insertUserMemoryRow(t, engine.db, "user", "note", fmt.Sprintf("recent unrelated note %d", i), "test", `["other"]`, "low", now.Add(time.Duration(-i)*time.Minute))
	}
	insertUserMemoryRow(t, engine.db, "user", "preference", "router bug should reuse body cache", "test", `["router","cache"]`, "high", now.Add(-24*time.Hour))

	items := engine.buildAppliedUserMemory(tokenizeQuery("router cache"), 6, 3)
	if len(items) == 0 {
		t.Fatal("expected applied user memory items")
	}
	found := false
	for _, item := range items {
		if strings.Contains(item.Content, "router bug should reuse body cache") {
			found = true
			if item.Matched == 0 {
				t.Fatalf("expected matched count > 0 for %+v", item)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected older matching row to be returned, got %+v", items)
	}
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "runtime.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := db.Exec(`
CREATE TABLE user_memory (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	scope TEXT,
	memory_kind TEXT,
	content TEXT,
	source TEXT,
	tags_json TEXT,
	confidence TEXT,
	active INTEGER,
	created_at TEXT,
	updated_at TEXT
);
CREATE TABLE symbol_index (
	file_path TEXT,
	symbol_kind TEXT,
	symbol_name TEXT,
	symbol_path TEXT,
	line_no INTEGER,
	summary TEXT,
	tags_json TEXT
);
CREATE TABLE file_index (
	path TEXT,
	summary TEXT,
	tags_json TEXT
);`); err != nil {
		t.Fatalf("create schema error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	engine, err := NewEngine(config.SCEMemoryConfig{
		Enable:     true,
		DBPath:     dbPath,
		MaxResults: 8,
		ScoreFloor: 0,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	return engine
}

func insertUserMemoryRow(t *testing.T, db *sql.DB, scope, kind, content, source, tagsJSON, confidence string, updatedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO user_memory(scope, memory_kind, content, source, tags_json, confidence, active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		scope, kind, content, source, tagsJSON, confidence, updatedAt.Format(time.RFC3339Nano), updatedAt.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert user_memory error = %v", err)
	}
}

func insertSymbolRow(t *testing.T, db *sql.DB, filePath, symbolKind, symbolName, symbolPath string, lineNo int, summary, tagsJSON string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO symbol_index(file_path, symbol_kind, symbol_name, symbol_path, line_no, summary, tags_json) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		filePath, symbolKind, symbolName, symbolPath, lineNo, summary, tagsJSON,
	); err != nil {
		t.Fatalf("insert symbol_index error = %v", err)
	}
}

func insertFileRow(t *testing.T, db *sql.DB, path, summary, tagsJSON string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO file_index(path, summary, tags_json) VALUES (?, ?, ?)`,
		path, summary, tagsJSON,
	); err != nil {
		t.Fatalf("insert file_index error = %v", err)
	}
}

func containsHitKind(hits []Hit, kind string) bool {
	for _, hit := range hits {
		if hit.Kind == kind {
			return true
		}
	}
	return false
}
