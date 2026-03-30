package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/tidwall/gjson"
)

func TestIntentUpgradeMiddlewareDynamicUsesLatestConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	mockAnalysis := http.NewServeMux()
	mockAnalysis.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"intent":"修复问题","implicit_requirements":["保持稳定"],"suggested_context":"使用当前项目上下文"}`,
					},
				},
			},
		})
	})
	mockServer := &http.Server{Handler: mockAnalysis}
	defer mockServer.Shutdown(context.Background())
	go func() {
		_ = mockServer.Serve(listener)
	}()

	port := listener.Addr().(*net.TCPAddr).Port

	var mu sync.RWMutex
	currentCfg := config.IntentUpgradeConfig{}
	getCfg := func() config.IntentUpgradeConfig {
		mu.RLock()
		defer mu.RUnlock()
		return cloneIntentUpgradeConfig(currentCfg)
	}
	setCfg := func(cfg config.IntentUpgradeConfig) {
		mu.Lock()
		currentCfg = cloneIntentUpgradeConfig(cfg)
		mu.Unlock()
	}

	router := gin.New()
	router.Use(IntentUpgradeMiddlewareDynamic(getCfg, port, nil))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		body, err := bodyutil.GetOrReadRequestBody(c, bodyutil.DefaultRequestBodyLimit)
		if err != nil {
			t.Fatalf("GetOrReadRequestBody() error = %v", err)
		}
		c.Data(http.StatusOK, "application/json", body)
	})

	reqBody := `{
	  "model": "gpt-test",
	  "messages": [
	    {"role": "user", "content": "请继续排查问题"}
	  ]
	}`

	setCfg(config.IntentUpgradeConfig{})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("disabled status = %d body=%s", rr.Code, rr.Body.String())
	}
	if got := countSystemMessagesWithPrefix(rr.Body.Bytes(), "Intent analysis:"); got != 0 {
		t.Fatalf("expected no injected intent analysis when disabled, got %d; body=%s", got, rr.Body.String())
	}

	setCfg(config.IntentUpgradeConfig{
		Enable:         true,
		Model:          "analysis-model",
		TimeoutMs:      2000,
		MaxInputTokens: 1000,
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-test")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("enabled status = %d body=%s", rr.Code, rr.Body.String())
	}
	if got := countSystemMessagesWithPrefix(rr.Body.Bytes(), "Intent analysis:"); got != 1 {
		t.Fatalf("expected injected intent analysis when enabled, got %d; body=%s", got, rr.Body.String())
	}
}

func TestCurrentIntentUpgradeConfigReturnsClone(t *testing.T) {
	server := newTestServer(t)
	server.intentUpgradeCfg.Store(cloneIntentUpgradeConfig(config.IntentUpgradeConfig{
		Enable:        true,
		APIKeys:       []string{"a"},
		ExcludeModels: []string{"m1"},
	}))

	cfg := server.currentIntentUpgradeConfig()
	cfg.APIKeys[0] = "changed"
	cfg.ExcludeModels[0] = "changed-model"

	fresh := server.currentIntentUpgradeConfig()
	if fresh.APIKeys[0] != "a" {
		t.Fatalf("stored APIKeys mutated unexpectedly: %+v", fresh.APIKeys)
	}
	if fresh.ExcludeModels[0] != "m1" {
		t.Fatalf("stored ExcludeModels mutated unexpectedly: %+v", fresh.ExcludeModels)
	}
}

func countSystemMessagesWithPrefix(body []byte, prefix string) int {
	count := 0
	for _, msg := range gjson.GetBytes(body, "messages").Array() {
		if strings.ToLower(msg.Get("role").String()) != "system" {
			continue
		}
		if strings.HasPrefix(msg.Get("content").String(), prefix) {
			count++
		}
	}
	return count
}
