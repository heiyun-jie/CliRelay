package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestBodyCacheMiddlewareSharesUpdatedBodyAcrossChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(BodyCacheMiddleware(128))
	r.Use(func(c *gin.Context) {
		body, err := bodyutil.GetOrReadRequestBody(c, 128)
		if err != nil {
			t.Fatalf("GetOrReadRequestBody() error = %v", err)
		}
		updated, err := sjson.SetBytes(body, "memory.context", "cached")
		if err != nil {
			t.Fatalf("SetBytes() error = %v", err)
		}
		bodyutil.SetCachedRequestBody(c, updated)
		c.Next()
	})
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		body, err := bodyutil.GetOrReadRequestBody(c, 128)
		if err != nil {
			t.Fatalf("GetOrReadRequestBody() error = %v", err)
		}
		c.Data(http.StatusOK, "application/json", body)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-test"}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}
	if got := gjson.GetBytes(rr.Body.Bytes(), "memory.context").String(); got != "cached" {
		t.Fatalf("memory.context = %q, want cached; body=%s", got, rr.Body.String())
	}
}

func TestBodyCacheMiddlewareRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(BodyCacheMiddleware(8))
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"value":"too-large"}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, rr.Code)
	}
}
