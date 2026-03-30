package management

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"golang.org/x/crypto/bcrypt"
)

func initManagementTestUsageDB(t *testing.T) {
	t.Helper()
	usage.CloseDB()
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	if err := usage.InitDB(dbPath, config.RequestLogStorageConfig{StoreContent: false}, time.UTC); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(usage.CloseDB)
}

func TestManagementMiddlewareAuditsDeniedAndAllowedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initManagementTestUsageDB(t)

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	cfg := &config.Config{}
	cfg.RemoteManagement.AllowRemote = true
	cfg.RemoteManagement.SecretKey = string(hash)

	handler := NewHandlerWithoutConfigFilePath(cfg, nil)
	t.Cleanup(handler.Close)

	router := gin.New()
	router.Use(handler.Middleware())
	router.GET("/v0/management/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	deniedReq := httptest.NewRequest(http.MethodGet, "/v0/management/ping", nil)
	deniedReq.RemoteAddr = "10.10.10.10:4567"
	deniedRec := httptest.NewRecorder()
	router.ServeHTTP(deniedRec, deniedReq)
	if deniedRec.Code != http.StatusUnauthorized {
		t.Fatalf("denied status = %d, want %d", deniedRec.Code, http.StatusUnauthorized)
	}

	allowedReq := httptest.NewRequest(http.MethodGet, "/v0/management/ping", nil)
	allowedReq.RemoteAddr = "10.10.10.11:4567"
	allowedReq.Header.Set("Authorization", "Bearer secret")
	allowedReq.Header.Set("User-Agent", "Mozilla/5.0")
	allowedReq.Header.Set("X-Forwarded-For", "198.51.100.20")
	allowedRec := httptest.NewRecorder()
	router.ServeHTTP(allowedRec, allowedReq)
	if allowedRec.Code != http.StatusOK {
		t.Fatalf("allowed status = %d, want %d", allowedRec.Code, http.StatusOK)
	}

	result, err := usage.QueryManagementAccessLogs(usage.ManagementAccessLogQueryParams{
		Page: 1,
		Size: 10,
		Days: 1,
	})
	if err != nil {
		t.Fatalf("QueryManagementAccessLogs() error = %v", err)
	}
	if got := len(result.Items); got != 2 {
		t.Fatalf("len(result.Items) = %d, want 2", got)
	}

	if result.Items[0].AuthSubject != "config-secret" || !result.Items[0].Allowed {
		t.Fatalf("latest row = %+v, want allowed config-secret", result.Items[0])
	}
	if result.Items[0].ClientIP != "198.51.100.20" {
		t.Fatalf("allowed ClientIP = %q, want %q", result.Items[0].ClientIP, "198.51.100.20")
	}
	if result.Items[0].ForwardedFor != "198.51.100.20" {
		t.Fatalf("allowed ForwardedFor = %q, want %q", result.Items[0].ForwardedFor, "198.51.100.20")
	}
	if result.Items[1].AuthSubject != "missing-key" || result.Items[1].Allowed {
		t.Fatalf("previous row = %+v, want denied missing-key", result.Items[1])
	}
}
