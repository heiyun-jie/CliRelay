package usage

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestSearchMemoryEntriesIncludesAlwaysApplyAndScopedMatches(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{StoreContent: false})

	if _, err := InsertMemoryEntry(MemoryEntry{
		ScopeType:   "api_key",
		ScopeValue:  "sk-test",
		Kind:        "preference",
		Content:     "User prefers concise factual responses.",
		AlwaysApply: true,
		Active:      true,
	}); err != nil {
		t.Fatalf("InsertMemoryEntry(always apply) error = %v", err)
	}

	if _, err := InsertMemoryEntry(MemoryEntry{
		ScopeType:  "api_key",
		ScopeValue: "sk-test",
		Kind:       "project",
		Content:    "CliRelay is the selected gateway for memory storage.",
		Tags:       []string{"clirelay", "memory"},
		Active:     true,
	}); err != nil {
		t.Fatalf("InsertMemoryEntry(query match) error = %v", err)
	}

	items, err := SearchMemoryEntries("sk-test", "请继续修改 CliRelay 的 memory gateway", 10)
	if err != nil {
		t.Fatalf("SearchMemoryEntries() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("SearchMemoryEntries() len = %d, want 2", len(items))
	}
	if !items[0].AlwaysApply {
		t.Fatalf("expected always_apply memory to rank first")
	}
}

func TestConversationTurnsAndApplicationsPersist(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{StoreContent: false})

	entry, err := InsertMemoryEntry(MemoryEntry{
		ScopeType:   "global",
		Kind:        "preference",
		Content:     "Always keep answers short.",
		AlwaysApply: true,
		Active:      true,
	})
	if err != nil {
		t.Fatalf("InsertMemoryEntry() error = %v", err)
	}

	if err := InsertConversationTurn("sk-test", "gpt-5", "/v1/chat/completions", "请查看 CliRelay 项目", "这是之前整理过的项目说明。"); err != nil {
		t.Fatalf("InsertConversationTurn() error = %v", err)
	}
	if err := InsertMemoryApplication("sk-test", "/v1/chat/completions", "CliRelay", entry.ID, "Relevant memory:\n- Always keep answers short.", "always_apply"); err != nil {
		t.Fatalf("InsertMemoryApplication() error = %v", err)
	}

	turns, err := ListRecentConversationTurns("sk-test", 5)
	if err != nil {
		t.Fatalf("ListRecentConversationTurns() error = %v", err)
	}
	if len(turns) != 1 || !strings.Contains(turns[0].UserText, "CliRelay") {
		t.Fatalf("unexpected turns: %+v", turns)
	}
	if !strings.Contains(turns[0].AssistantText, "项目说明") {
		t.Fatalf("assistant turn not stored: %+v", turns[0])
	}

	apps, err := ListMemoryApplications("sk-test", 5)
	if err != nil {
		t.Fatalf("ListMemoryApplications() error = %v", err)
	}
	if len(apps) != 1 || apps[0].MemoryEntryID != entry.ID {
		t.Fatalf("unexpected applications: %+v", apps)
	}
}

func TestSearchMemoryEntriesDoesNotApplyPriorityOnlyEntries(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{StoreContent: false})

	if _, err := InsertMemoryEntry(MemoryEntry{
		ScopeType:  "api_key",
		ScopeValue: "sk-test",
		Kind:       "project",
		Content:    "Internal deployment runbook for the billing gateway.",
		Priority:   50,
		Active:     true,
	}); err != nil {
		t.Fatalf("InsertMemoryEntry(priority only) error = %v", err)
	}

	items, err := SearchMemoryEntries("sk-test", "Please draft a landing page headline", 10)
	if err != nil {
		t.Fatalf("SearchMemoryEntries() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no matches for unrelated query, got %+v", items)
	}
}

func TestSearchConversationTurnsMatchesAssistantReply(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{StoreContent: false})

	if err := InsertConversationTurn("sk-test", "gpt-5.4", "/v1/responses", "监控中心怎么恢复", "需要检查 management 面板接口兼容层。"); err != nil {
		t.Fatalf("InsertConversationTurn(first) error = %v", err)
	}
	if err := InsertConversationTurn("sk-test", "gpt-5.4", "/v1/responses", "记忆系统怎么接", "先把对话和回复成对存储，再做相关性召回。"); err != nil {
		t.Fatalf("InsertConversationTurn(second) error = %v", err)
	}

	turns, err := SearchConversationTurns("sk-test", "兼容层", 3)
	if err != nil {
		t.Fatalf("SearchConversationTurns() error = %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("SearchConversationTurns() len = %d, want 1", len(turns))
	}
	if !strings.Contains(turns[0].AssistantText, "兼容层") {
		t.Fatalf("unexpected matched turn: %+v", turns[0])
	}
}
