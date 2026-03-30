package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/sce"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	intentUpgradeHeader     = "X-Intent-Upgrade-Skip"
	intentUpgradeContextKey = "intent-upgrade-in-progress"
)

// IntentAnalysis is the structured result from the analysis model.
type IntentAnalysis struct {
	Intent               string   `json:"intent"`
	ImplicitRequirements []string `json:"implicit_requirements"`
	SuggestedContext     string   `json:"suggested_context"`
}

// intentUpgradeState tracks per-key rate limiting for feedback writes.
type intentUpgradeState struct {
	mu            sync.Mutex
	lastWriteBack map[string]time.Time // apiKey -> last write time
}

func newIntentUpgradeState() *intentUpgradeState {
	return &intentUpgradeState{lastWriteBack: make(map[string]time.Time)}
}

func (s *intentUpgradeState) canWriteBack(apiKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastWriteBack[apiKey]
	if !ok || time.Since(last) > 5*time.Minute {
		s.lastWriteBack[apiKey] = time.Now()
		return true
	}
	return false
}

// IntentUpgradeMiddleware performs two-pass prompt enhancement: first analyzing
// the user's intent with a lightweight model, then injecting the analysis as
// additional context for the target model.
//
// The analysis call is routed through CliRelay's own /v1/chat/completions
// endpoint (using the configured channel). A header guard prevents recursion.
func IntentUpgradeMiddleware(cfg config.IntentUpgradeConfig, localPort int, sceEngine *sce.Engine) gin.HandlerFunc {
	return IntentUpgradeMiddlewareWithEngine(cfg, localPort, func(fn func(*sce.Engine)) {
		if fn != nil {
			fn(sceEngine)
		}
	})
}

func IntentUpgradeMiddlewareWithEngine(cfg config.IntentUpgradeConfig, localPort int, withEngine func(func(*sce.Engine))) gin.HandlerFunc {
	if !cfg.Enable || cfg.Model == "" {
		return func(c *gin.Context) { c.Next() }
	}

	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 8000
	}
	maxInput := cfg.MaxInputTokens
	if maxInput <= 0 {
		maxInput = 2000
	}

	httpClient := &http.Client{
		Timeout: time.Duration(timeoutMs) * time.Millisecond,
	}
	state := newIntentUpgradeState()

	excludeSet := make(map[string]bool, len(cfg.ExcludeModels))
	for _, m := range cfg.ExcludeModels {
		excludeSet[strings.ToLower(m)] = true
	}
	apiKeySet := make(map[string]bool, len(cfg.APIKeys))
	for _, k := range cfg.APIKeys {
		apiKeySet[k] = true
	}

	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		// Recursion guard: skip if this request is already an intent upgrade call.
		if c.Request.Header.Get(intentUpgradeHeader) == "true" {
			c.Next()
			return
		}
		if _, exists := c.Get(intentUpgradeContextKey); exists {
			c.Next()
			return
		}

		bodyBytes, err := bodyutil.GetOrReadRequestBody(c, bodyutil.DefaultRequestBodyLimit)
		if err != nil {
			if bodyutil.IsTooLarge(err) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.Next()
			return
		}

		// Check if this request should be upgraded.
		model := strings.TrimSpace(gjson.GetBytes(bodyBytes, "model").String())
		apiKey, _ := c.Get("apiKey")
		apiKeyStr, _ := apiKey.(string)

		if !shouldUpgrade(excludeSet, apiKeySet, apiKeyStr, model) {
			c.Next()
			return
		}

		c.Set(intentUpgradeContextKey, true)

		// Extract user prompt and any already-injected memory context.
		userPrompt := extractLatestUserTurn(bodyBytes)
		if userPrompt == "" {
			c.Next()
			return
		}

		// Truncate prompt for analysis to control cost.
		userPromptRunes := []rune(userPrompt)
		if len(userPromptRunes) > maxInput {
			userPrompt = string(userPromptRunes[:maxInput])
		}

		// Extract existing memory context (injected by MemoryHydrationMiddleware).
		memoryContext := extractExistingSystemContent(bodyBytes)

		// Call analysis model.
		analysis, err := callIntentAnalysis(c, httpClient, localPort, cfg.Model, userPrompt, memoryContext, model)
		if err != nil {
			log.WithError(err).Warn("intent upgrade analysis call failed, proceeding without upgrade")
			c.Next()
			return
		}

		// Inject analysis into request body.
		newBody, changed := injectIntentAnalysis(bodyBytes, analysis)
		if changed {
			bodyBytes = newBody
			bodyutil.SetCachedRequestBody(c, bodyBytes)
		}

		c.Next()

		// Async feedback write-back to SCE.
		if withEngine != nil && state.canWriteBack(apiKeyStr) {
			go func() {
				withEngine(func(engine *sce.Engine) {
					if engine == nil {
						return
					}
					_, _ = engine.Remember(
						fmt.Sprintf("Intent: %s | Context: %s", analysis.Intent, analysis.SuggestedContext),
						"auto-feedback", "user", "clirelay-intent-upgrade", "low",
						[]string{"intent-upgrade", "auto"},
					)
				})
			}()
		}
	}
}

func shouldUpgrade(excludeSet, apiKeySet map[string]bool, apiKey, model string) bool {
	if excludeSet[strings.ToLower(model)] {
		return false
	}
	if len(apiKeySet) > 0 && !apiKeySet[apiKey] {
		return false
	}
	return true
}

func callIntentAnalysis(c *gin.Context, httpClient *http.Client, localPort int, analysisModel, userPrompt, memoryContext, targetModel string) (*IntentAnalysis, error) {
	systemPrompt := `You are a development task intent analyzer. Analyze the user's request considering any provided project context, then return a JSON object with exactly these keys:
- "intent": What the user truly wants to accomplish (one sentence)
- "implicit_requirements": Array of requirements not explicitly stated but implied
- "suggested_context": Additional context that would help the target model produce a better response

Keep your response concise. Return ONLY valid JSON, no markdown fences.`

	userContent := fmt.Sprintf("User request: %s\nTarget model: %s", userPrompt, targetModel)
	if memoryContext != "" {
		userContent = fmt.Sprintf("Project context:\n%s\n\nUser request: %s\nTarget model: %s",
			truncateInjectionText(memoryContext, 1500), userPrompt, targetModel)
	}

	reqBody := map[string]interface{}{
		"model": analysisModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userContent},
		},
		"temperature": 0.3,
		"max_tokens":  512,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal analysis request: %w", err)
	}

	// Call CliRelay's own endpoint with recursion guard header.
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", localPort)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create analysis request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(intentUpgradeHeader, "true")

	// Forward the API key for auth.
	if apiKey, exists := c.Get("apiKey"); exists {
		if key, ok := apiKey.(string); ok {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("analysis http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("analysis status %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	if err != nil {
		return nil, fmt.Errorf("read analysis response: %w", err)
	}

	// Extract assistant content from OpenAI response format.
	content := gjson.GetBytes(respBody, "choices.0.message.content").String()
	if content == "" {
		return nil, fmt.Errorf("empty analysis response")
	}

	// Strip markdown code fences if present.
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var analysis IntentAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		return nil, fmt.Errorf("parse analysis JSON: %w (content: %s)", err, truncateInjectionText(content, 200))
	}

	return &analysis, nil
}

func extractExistingSystemContent(bodyBytes []byte) string {
	messages := gjson.GetBytes(bodyBytes, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return ""
	}
	for _, msg := range messages.Array() {
		if strings.ToLower(msg.Get("role").String()) == "system" {
			return strings.TrimSpace(msg.Get("content").String())
		}
	}
	return ""
}

func injectIntentAnalysis(bodyBytes []byte, analysis *IntentAnalysis) ([]byte, bool) {
	if analysis == nil || analysis.Intent == "" {
		return bodyBytes, false
	}

	var lines []string
	lines = append(lines, "Intent analysis: "+analysis.Intent)
	if len(analysis.ImplicitRequirements) > 0 {
		lines = append(lines, "Implicit requirements:")
		for _, req := range analysis.ImplicitRequirements {
			lines = append(lines, "  - "+req)
		}
	}
	if analysis.SuggestedContext != "" {
		lines = append(lines, "Additional context: "+analysis.SuggestedContext)
	}
	analysisText := strings.Join(lines, "\n")

	// Inject as a system message into OpenAI messages format.
	if gjson.GetBytes(bodyBytes, "messages").Exists() && gjson.GetBytes(bodyBytes, "messages").IsArray() {
		sysMsg := map[string]interface{}{
			"role":    "system",
			"content": analysisText,
		}
		var body map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			return bodyBytes, false
		}
		messages, _ := body["messages"].([]interface{})
		// Insert after existing system messages but before user messages.
		insertIdx := 0
		for i, msg := range messages {
			if m, ok := msg.(map[string]interface{}); ok {
				if role, _ := m["role"].(string); strings.ToLower(role) == "system" {
					insertIdx = i + 1
				}
			}
		}
		newMessages := make([]interface{}, 0, len(messages)+1)
		newMessages = append(newMessages, messages[:insertIdx]...)
		newMessages = append(newMessages, sysMsg)
		newMessages = append(newMessages, messages[insertIdx:]...)
		body["messages"] = newMessages
		newBody, err := json.Marshal(body)
		if err != nil {
			return bodyBytes, false
		}
		return newBody, true
	}

	// Responses API format: append to instructions.
	if gjson.GetBytes(bodyBytes, "input").Exists() {
		existing := strings.TrimSpace(gjson.GetBytes(bodyBytes, "instructions").String())
		combined := analysisText
		if existing != "" {
			combined = existing + "\n\n" + analysisText
		}
		newBody, err := sjson.SetBytes(bodyBytes, "instructions", combined)
		if err != nil {
			return bodyBytes, false
		}
		return newBody, true
	}

	return bodyBytes, false
}
