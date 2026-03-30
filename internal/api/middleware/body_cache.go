package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
)

// BodyCacheMiddleware reads JSON-like request bodies once and caches the latest snapshot in Gin context.
func BodyCacheMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil || c.Request == nil || c.Request.Body == nil {
			c.Next()
			return
		}
		if !shouldCacheRequestBody(c.Request) {
			c.Next()
			return
		}
		if _, ok := bodyutil.GetCachedRequestBody(c); ok {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit && limit > 0 {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		if _, err := bodyutil.GetOrReadRequestBody(c, limit); err != nil {
			if bodyutil.IsTooLarge(err) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		c.Next()
	}
}

func shouldCacheRequestBody(req *http.Request) bool {
	if req == nil || req.Body == nil {
		return false
	}
	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Type")))
	return !strings.HasPrefix(contentType, "multipart/form-data")
}
