package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// PrepareStreamingResponse resets any inherited error status and applies the
// standard SSE headers before the first response bytes are written.
func PrepareStreamingResponse(c *gin.Context) {
	if c == nil {
		return
	}
	if c.Writer != nil {
		_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	}
	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
}
