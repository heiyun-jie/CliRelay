package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestImportSCEMemoryImportsAndUpdatesEntries(t *testing.T) {
	usage.CloseDB()

	root := filepath.Join(t.TempDir(), ".sce", "memory")
	if err := os.MkdirAll(filepath.Join(root, "consolidated"), 0755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "raw"), 0755); err != nil {
		t.Fatalf("MkdirAll(raw) error = %v", err)
	}

	index := `{
  "version": 1,
  "entries": [
    {
      "id": "consolidated-api-contract",
      "stage": "consolidated",
      "kind": "pattern",
      "title": "API 契约",
      "area": "backend",
      "path": ".sce/memory/consolidated/api-contract.md",
      "tags": ["api", "contract"],
      "confidence": "high"
    },
    {
      "id": "raw-debug-note",
      "stage": "raw",
      "kind": "note",
      "title": "临时记录",
      "area": "ops",
      "path": ".sce/memory/raw/debug-note.md",
      "tags": ["debug"],
      "confidence": "low"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(root, "index.json"), []byte(index), 0644); err != nil {
		t.Fatalf("WriteFile(index) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "consolidated", "api-contract.md"), []byte("# API 契约\n\n需要先确认真实字段。"), 0644); err != nil {
		t.Fatalf("WriteFile(content) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "raw", "debug-note.md"), []byte("# 临时记录"), 0644); err != nil {
		t.Fatalf("WriteFile(raw) error = %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8318\n"), 0644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	cfg := &config.Config{}
	cfg.SDKConfig.RequestLogStorage = config.RequestLogStorageConfig{StoreContent: false}

	result, err := importSCEMemory(cfg, configPath, root, "global", "")
	if err != nil {
		t.Fatalf("importSCEMemory(first) error = %v", err)
	}
	if result.Imported != 1 || result.Updated != 0 || result.Skipped != 1 {
		t.Fatalf("unexpected first result: %+v", result)
	}

	items, err := usage.ListMemoryEntries("global", "", false, 50)
	if err != nil {
		t.Fatalf("ListMemoryEntries() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 imported item, got %d", len(items))
	}
	if items[0].Source != "sce-import:consolidated-api-contract" {
		t.Fatalf("unexpected source: %+v", items[0])
	}
	if !strings.Contains(items[0].Content, "Anchor: API 契约") {
		t.Fatalf("expected anchor summary, got %q", items[0].Content)
	}
	if !strings.Contains(items[0].Content, "Core: 需要先确认真实字段。") {
		t.Fatalf("expected concise core summary, got %q", items[0].Content)
	}

	if err := os.WriteFile(filepath.Join(root, "consolidated", "api-contract.md"), []byte("# API 契约\n\n已经更新为最新说明。"), 0644); err != nil {
		t.Fatalf("WriteFile(update) error = %v", err)
	}

	result, err = importSCEMemory(cfg, configPath, root, "global", "")
	if err != nil {
		t.Fatalf("importSCEMemory(second) error = %v", err)
	}
	if result.Imported != 0 || result.Updated != 1 || result.Skipped != 1 {
		t.Fatalf("unexpected second result: %+v", result)
	}

	items, err = usage.ListMemoryEntries("global", "", false, 50)
	if err != nil {
		t.Fatalf("ListMemoryEntries(second) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 imported item after update, got %d", len(items))
	}
	if !strings.Contains(items[0].Content, "已经更新为最新说明") {
		t.Fatalf("expected updated content, got %q", items[0].Content)
	}

	usage.CloseDB()
}
