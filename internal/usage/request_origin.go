package usage

import "strings"

// RequestOrigin captures the downstream request source metadata observed by the gateway.
type RequestOrigin struct {
	ClientIP     string `json:"client_ip"`
	ForwardedFor string `json:"forwarded_for"`
	UserAgent    string `json:"user_agent"`
	RequestPath  string `json:"request_path"`
}

func normalizeRequestOrigin(origin RequestOrigin) RequestOrigin {
	origin.ClientIP = truncateMemoryText(strings.TrimSpace(origin.ClientIP), 128)
	origin.ForwardedFor = truncateMemoryText(strings.TrimSpace(origin.ForwardedFor), 512)
	origin.UserAgent = truncateMemoryText(strings.TrimSpace(origin.UserAgent), 512)
	origin.RequestPath = truncateMemoryText(strings.TrimSpace(origin.RequestPath), 256)
	return origin
}
