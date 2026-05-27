package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestOpenAICompatManagementPreservesTestModelMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	h := &Handler{cfg: &config.Config{}, configFilePath: configPath}

	putBody := []byte(`[
		{
			"name": "xiaomi-mimo",
			"base-url": " https://token-plan-cn.xiaomimimo.com/v1 ",
			"priority": 6,
			"test-model": " MiMo-V2.5-Pro ",
			"api-key-entries": [{"api-key": " sk-test "}],
			"models": [
				{"name": " mimo-v2.5-pro ", "alias": " MiMo-V2.5-Pro ", "priority": 7, "test-model": " gpt-5.4 "},
				{"name": " mimo-v2.5 ", "alias": " MiMo-V2.5 ", "test-model": " gpt-5.5 "}
			]
		}
	]`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/v0/management/openai-compatibility", bytes.NewReader(putBody))
	h.PutOpenAICompat(c)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", w.Code, w.Body.String())
	}

	if len(h.cfg.OpenAICompatibility) != 1 {
		t.Fatalf("OpenAICompatibility after PUT = %+v", h.cfg.OpenAICompatibility)
	}
	got := h.cfg.OpenAICompatibility[0]
	if got.TestModel != "MiMo-V2.5-Pro" || got.Priority != 6 {
		t.Fatalf("provider metadata after PUT = %+v", got)
	}
	if len(got.Models) != 2 || got.Models[0].Priority != 7 || got.Models[0].TestModel != "gpt-5.4" || got.Models[1].TestModel != "gpt-5.5" {
		t.Fatalf("model metadata after PUT = %+v", got.Models)
	}

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/openai-compatibility", nil)
	h.GetOpenAICompat(c)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", w.Code, w.Body.String())
	}
	var getBody struct {
		Items []config.OpenAICompatibility `json:"openai-compatibility"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode GET body: %v", err)
	}
	if len(getBody.Items) != 1 || getBody.Items[0].TestModel != "MiMo-V2.5-Pro" {
		t.Fatalf("GET provider metadata = %+v", getBody)
	}
	if len(getBody.Items[0].Models) != 2 || getBody.Items[0].Models[0].TestModel != "gpt-5.4" || getBody.Items[0].Models[0].Priority != 7 {
		t.Fatalf("GET model metadata = %+v", getBody.Items[0].Models)
	}

	patchBody := []byte(`{
		"index": 0,
		"value": {
			"priority": 8,
			"test-model": " MiMo-V2.5 ",
			"models": [
				{"name": "mimo-v2.5-pro", "alias": "MiMo-V2.5-Pro", "priority": 9, "test-model": " gpt-5.5 "}
			]
		}
	}`)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/openai-compatibility", bytes.NewReader(patchBody))
	h.PatchOpenAICompat(c)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d body=%s", w.Code, w.Body.String())
	}
	got = h.cfg.OpenAICompatibility[0]
	if got.Priority != 8 || got.TestModel != "MiMo-V2.5" {
		t.Fatalf("provider metadata after PATCH = %+v", got)
	}
	if len(got.Models) != 1 || got.Models[0].Priority != 9 || got.Models[0].TestModel != "gpt-5.5" {
		t.Fatalf("model metadata after PATCH = %+v", got.Models)
	}
}
