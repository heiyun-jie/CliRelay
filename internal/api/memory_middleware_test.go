package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"github.com/tidwall/gjson"
)

func TestMemoryHydrationMiddlewareInjectsMemoryAndRecordsTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	usage.CloseDB()
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	if err := usage.InitDB(dbPath, config.RequestLogStorageConfig{StoreContent: false}, time.UTC); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(usage.CloseDB)

	if _, err := usage.InsertMemoryEntry(usage.MemoryEntry{
		ScopeType:   "api_key",
		ScopeValue:  "sk-test",
		Kind:        "preference",
		Content:     "User prefers concise factual answers.",
		AlwaysApply: true,
		Active:      true,
	}); err != nil {
		t.Fatalf("InsertMemoryEntry() error = %v", err)
	}
	if err := usage.InsertConversationTurn("sk-test", "gpt-test", "/v1/chat/completions", "Earlier user request about the audit page layout.", "We already fixed the layout padding regression."); err != nil {
		t.Fatalf("InsertConversationTurn() error = %v", err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("apiKey", "sk-test")
		c.Next()
	})
	router.Use(MemoryHydrationMiddleware(nil))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"choices": []gin.H{
				{
					"message": gin.H{
						"role":    "assistant",
						"content": "Continue from the existing memory middleware changes.",
					},
				},
			},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
	  "model": "gpt-test",
	  "messages": [
	    {"role": "user", "content": "Please continue the CliRelay memory gateway work."}
	  ]
	}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got := gjson.GetBytes(rr.Body.Bytes(), "choices.0.message.content").String(); got == "" {
		t.Fatalf("expected assistant response body, got %s", rr.Body.String())
	}

	turns, err := usage.ListRecentConversationTurns("sk-test", 5)
	if err != nil {
		t.Fatalf("ListRecentConversationTurns() error = %v", err)
	}
	if len(turns) < 2 {
		t.Fatalf("expected current turn to be recorded, got %+v", turns)
	}
	if turns[0].AssistantText != "Continue from the existing memory middleware changes." {
		t.Fatalf("assistant response not recorded: %+v", turns[0])
	}

	apps, err := usage.ListMemoryApplications("sk-test", 5)
	if err != nil {
		t.Fatalf("ListMemoryApplications() error = %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 memory application log, got %+v", apps)
	}
}

func TestMemoryHydrationMiddlewareRecordsLatestUserTurnOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	usage.CloseDB()
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	if err := usage.InitDB(dbPath, config.RequestLogStorageConfig{StoreContent: false}, time.UTC); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(usage.CloseDB)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("apiKey", "sk-test")
		c.Next()
	})
	router.Use(MemoryHydrationMiddleware(nil))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"choices": []gin.H{
				{
					"message": gin.H{
						"role":    "assistant",
						"content": "Latest answer",
					},
				},
			},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
	  "model": "gpt-test",
	  "messages": [
	    {"role": "user", "content": "First question"},
	    {"role": "assistant", "content": "First answer"},
	    {"role": "user", "content": "Latest question"}
	  ]
	}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	turns, err := usage.ListRecentConversationTurns("sk-test", 5)
	if err != nil {
		t.Fatalf("ListRecentConversationTurns() error = %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 recorded turn, got %+v", turns)
	}
	if turns[0].UserText != "Latest question" {
		t.Fatalf("recorded turn = %q, want latest user turn", turns[0].UserText)
	}
	if turns[0].AssistantText != "Latest answer" {
		t.Fatalf("recorded assistant turn = %q, want latest assistant answer", turns[0].AssistantText)
	}
}

func TestMemoryHydrationMiddlewareIgnoresFunctionCallOutputAsLatestUserTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	usage.CloseDB()
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	if err := usage.InitDB(dbPath, config.RequestLogStorageConfig{StoreContent: false}, time.UTC); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(usage.CloseDB)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("apiKey", "sk-test")
		c.Next()
	})
	router.Use(MemoryHydrationMiddleware(nil))
	router.POST("/v1/responses", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"output": []gin.H{
				{
					"type": "message",
					"role": "assistant",
					"content": []gin.H{
						{"type": "output_text", "text": "已继续处理。"},
					},
				},
			},
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
	  "model": "gpt-test",
	  "input": [
	    {
	      "type": "message",
	      "role": "user",
	      "content": [{"type": "input_text", "text": "真正的问题"}]
	    },
	    {
	      "type": "function_call_output",
	      "call_id": "call_123",
	      "output": "工具输出，不应被当成用户问题"
	    }
	  ]
	}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	turns, err := usage.ListRecentConversationTurns("sk-test", 5)
	if err != nil {
		t.Fatalf("ListRecentConversationTurns() error = %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 recorded turn, got %+v", turns)
	}
	if turns[0].UserText != "真正的问题" {
		t.Fatalf("recorded turn = %q, want true user message", turns[0].UserText)
	}
}
