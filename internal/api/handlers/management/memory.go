package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

type memoryEntryRequest struct {
	ScopeType   string   `json:"scope_type"`
	ScopeValue  string   `json:"scope_value"`
	Kind        string   `json:"kind"`
	Content     string   `json:"content"`
	Tags        []string `json:"tags"`
	Source      string   `json:"source"`
	Priority    int      `json:"priority"`
	AlwaysApply bool     `json:"always_apply"`
	Active      *bool    `json:"active"`
}

// GetMemoryEntries returns stored memory entries.
func (h *Handler) GetMemoryEntries(c *gin.Context) {
	scopeType := strings.TrimSpace(c.Query("scope_type"))
	scopeValue := strings.TrimSpace(c.Query("scope_value"))
	if apiKey := strings.TrimSpace(c.Query("api_key")); apiKey != "" {
		scopeType = "api_key"
		scopeValue = apiKey
	}
	activeOnly := true
	if raw := strings.TrimSpace(c.Query("active_only")); raw != "" {
		activeOnly = raw != "0" && !strings.EqualFold(raw, "false")
	}

	items, err := usage.ListMemoryEntries(scopeType, scopeValue, activeOnly, intQueryDefault(c, "limit", 50))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// PostMemoryEntry creates a new memory entry.
func (h *Handler) PostMemoryEntry(c *gin.Context) {
	var body memoryEntryRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	active := true
	if body.Active != nil {
		active = *body.Active
	}

	entry, err := usage.InsertMemoryEntry(usage.MemoryEntry{
		ScopeType:   body.ScopeType,
		ScopeValue:  body.ScopeValue,
		Kind:        body.Kind,
		Content:     body.Content,
		Tags:        body.Tags,
		Source:      body.Source,
		Priority:    body.Priority,
		AlwaysApply: body.AlwaysApply,
		Active:      active,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, entry)
}

// GetMemoryApplications returns recent memory injection records.
func (h *Handler) GetMemoryApplications(c *gin.Context) {
	items, err := usage.ListMemoryApplications(strings.TrimSpace(c.Query("api_key")), intQueryDefault(c, "limit", 50))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// GetConversationTurns returns observed user turns stored by the gateway.
func (h *Handler) GetConversationTurns(c *gin.Context) {
	limit := intQueryDefault(c, "limit", 20)
	apiKey := strings.TrimSpace(c.Query("api_key"))
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api_key parameter is required"})
		return
	}

	items, err := usage.ListRecentConversationTurns(apiKey, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
