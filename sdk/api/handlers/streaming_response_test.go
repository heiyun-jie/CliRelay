package handlers

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type deadlineTrackingResponseWriter struct {
	gin.ResponseWriter
	deadlines []time.Time
}

func (w *deadlineTrackingResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func (w *deadlineTrackingResponseWriter) sawZeroDeadline() bool {
	for i := range w.deadlines {
		if w.deadlines[i].IsZero() {
			return true
		}
	}
	return false
}

func TestPrepareStreamingResponseClearsServerWriteDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	tracking := &deadlineTrackingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = tracking

	PrepareStreamingResponse(c)

	if !tracking.sawZeroDeadline() {
		t.Fatal("expected streaming response setup to clear the server write deadline")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
}
