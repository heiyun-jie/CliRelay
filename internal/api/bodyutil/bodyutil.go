package bodyutil

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	DefaultRequestBodyLimit   int64 = 16 << 20
	ManagementBodyLimit       int64 = 2 << 20
	ConfigYAMLBodyLimit       int64 = 2 << 20
	AuthFileBodyLimit         int64 = 2 << 20
	VertexCredentialBodyLimit int64 = 2 << 20
	cachedRequestBodyKey            = "bodyutil.cachedRequestBody"
)

var ErrBodyTooLarge = errors.New("request body too large")

func normalizeLimit(limit int64) int64 {
	if limit <= 0 {
		return DefaultRequestBodyLimit
	}
	return limit
}

// ReadRequestBody reads and restores an incoming HTTP request body with a strict size limit.
func ReadRequestBody(c *gin.Context, limit int64) ([]byte, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil
	}

	limit = normalizeLimit(limit)
	if c.Writer == nil {
		body, err := ReadAll(c.Request.Body, limit)
		if err != nil {
			return nil, err
		}
		RestoreRequestBody(c, body)
		return body, nil
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	RestoreRequestBody(c, body)
	return body, nil
}

// RestoreRequestBody replaces the request body with an in-memory copy so downstream readers can reuse it.
func RestoreRequestBody(c *gin.Context, body []byte) {
	if c == nil || c.Request == nil {
		return
	}

	cloned := cloneRequestBody(body)
	c.Request.Body = io.NopCloser(bytes.NewReader(cloned))
	c.Request.ContentLength = int64(len(cloned))
}

// GetCachedRequestBody returns the cached request body when present and restores it for reuse.
func GetCachedRequestBody(c *gin.Context) ([]byte, bool) {
	if c == nil {
		return nil, false
	}

	value, exists := c.Get(cachedRequestBodyKey)
	if !exists {
		return nil, false
	}
	body, ok := value.([]byte)
	if !ok {
		return nil, false
	}

	cloned := cloneRequestBody(body)
	RestoreRequestBody(c, cloned)
	return cloned, true
}

// SetCachedRequestBody stores the latest request body snapshot and restores it for downstream readers.
func SetCachedRequestBody(c *gin.Context, body []byte) {
	if c == nil {
		return
	}

	cloned := cloneRequestBody(body)
	c.Set(cachedRequestBodyKey, cloned)
	RestoreRequestBody(c, cloned)
}

// GetOrReadRequestBody prefers the cached request body and falls back to reading and caching the request once.
func GetOrReadRequestBody(c *gin.Context, limit int64) ([]byte, error) {
	if body, ok := GetCachedRequestBody(c); ok {
		return body, nil
	}

	body, err := ReadRequestBody(c, limit)
	if err != nil {
		return nil, err
	}
	SetCachedRequestBody(c, body)
	return cloneRequestBody(body), nil
}

// ReadAll reads from any reader with a strict size limit.
func ReadAll(r io.Reader, limit int64) ([]byte, error) {
	limit = normalizeLimit(limit)
	limited := &io.LimitedReader{R: r, N: limit + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, ErrBodyTooLarge
	}
	return body, nil
}

func IsTooLarge(err error) bool {
	if errors.Is(err, ErrBodyTooLarge) {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

// LimitBodyMiddleware eagerly reads and restores request bodies with a hard limit.
// It is intended for small management JSON payloads so downstream binders can reuse the body safely.
func LimitBodyMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil || c.Request == nil || c.Request.Body == nil {
			c.Next()
			return
		}
		if !shouldLimitRequestBody(c.Request) {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit && limit > 0 {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		if _, err := ReadRequestBody(c, limit); err != nil {
			if IsTooLarge(err) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		c.Next()
	}
}

func shouldLimitRequestBody(req *http.Request) bool {
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

func cloneRequestBody(body []byte) []byte {
	if len(body) == 0 {
		return nil
	}
	cloned := make([]byte, len(body))
	copy(cloned, body)
	return cloned
}
