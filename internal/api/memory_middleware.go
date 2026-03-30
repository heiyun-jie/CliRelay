package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/sce"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maxMemoryCapturedResponseBytes = 256 << 10

type memoryCaptureWriter struct {
	gin.ResponseWriter
	body     bytes.Buffer
	maxBytes int
}

func (w *memoryCaptureWriter) Write(data []byte) (int, error) {
	if remaining := w.maxBytes - w.body.Len(); remaining > 0 {
		if len(data) > remaining {
			w.body.Write(data[:remaining])
		} else {
			w.body.Write(data)
		}
	}
	return w.ResponseWriter.Write(data)
}

func (w *memoryCaptureWriter) WriteString(data string) (int, error) {
	if remaining := w.maxBytes - w.body.Len(); remaining > 0 {
		if len(data) > remaining {
			w.body.WriteString(data[:remaining])
		} else {
			w.body.WriteString(data)
		}
	}
	return w.ResponseWriter.WriteString(data)
}

// MemoryHydrationMiddleware injects persisted memories, recent turns, and SCE project knowledge
// into supported request bodies. When sceEngine is non-nil, SCE results are queried in-process
// and merged into the injection text.
func MemoryHydrationMiddleware(sceEngine *sce.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
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

		apiKey, _ := c.Get("apiKey")
		apiKeyStr, _ := apiKey.(string)
		queryText := extractLatestUserTurn(bodyBytes)
		model := strings.TrimSpace(gjson.GetBytes(bodyBytes, "model").String())
		captureWriter := &memoryCaptureWriter{
			ResponseWriter: c.Writer,
			maxBytes:       maxMemoryCapturedResponseBytes,
		}
		c.Writer = captureWriter

		// Query local memory.
		memories, memErr := usage.SearchMemoryEntries(apiKeyStr, queryText, 5)
		if memErr != nil {
			log.WithError(memErr).Warn("memory hydration search failed")
		}
		turns, turnErr := usage.SearchConversationTurns(apiKeyStr, queryText, 3)
		if turnErr != nil {
			log.WithError(turnErr).Warn("memory hydration relevant turns query failed")
		}

		// Query SCE engine (in-process, ~5ms).
		var sceResult *sce.HydrateResult
		if sceEngine != nil && queryText != "" {
			var sceErr error
			sceResult, sceErr = sceEngine.HydrateContext(queryText, 0)
			if sceErr != nil {
				log.WithError(sceErr).Warn("SCE memory hydration query failed")
			}
		}

		injectedText := buildUnifiedMemoryText(memories, turns, sceResult)
		if strings.TrimSpace(injectedText) != "" {
			newBody, changed := injectMemoryIntoBody(bodyBytes, injectedText)
			if changed {
				bodyBytes = newBody
				bodyutil.SetCachedRequestBody(c, bodyBytes)
				for _, entry := range memories {
					matchReason := "always_apply"
					if !entry.AlwaysApply {
						matchReason = "query_match"
					}
					if err := usage.InsertMemoryApplication(apiKeyStr, c.FullPath(), queryText, entry.ID, injectedText, matchReason); err != nil {
						log.WithError(err).Warn("memory hydration application log failed")
					}
				}
			}
		}

		c.Next()

		if c.Writer != nil && c.Writer.Status() >= http.StatusInternalServerError {
			return
		}
		assistantText := extractAssistantReply(captureWriter.body.Bytes())
		if err := usage.InsertConversationTurn(apiKeyStr, model, c.FullPath(), queryText, assistantText); err != nil {
			log.WithError(err).Warn("memory hydration conversation turn insert failed")
		}
	}
}

func injectMemoryIntoBody(bodyBytes []byte, memoryText string) ([]byte, bool) {
	if gjson.GetBytes(bodyBytes, "messages").Exists() && gjson.GetBytes(bodyBytes, "messages").IsArray() {
		sysMsg := map[string]interface{}{
			"role":    "system",
			"content": memoryText,
		}
		var body map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			return bodyBytes, false
		}
		messages, _ := body["messages"].([]interface{})
		newMessages := make([]interface{}, 0, len(messages)+1)
		newMessages = append(newMessages, sysMsg)
		newMessages = append(newMessages, messages...)
		body["messages"] = newMessages
		newBody, err := json.Marshal(body)
		if err != nil {
			return bodyBytes, false
		}
		return newBody, true
	}

	if gjson.GetBytes(bodyBytes, "input").Exists() {
		existing := strings.TrimSpace(gjson.GetBytes(bodyBytes, "instructions").String())
		combined := memoryText
		if existing != "" {
			combined += "\n\n" + existing
		}
		newBody, err := sjson.SetBytes(bodyBytes, "instructions", combined)
		if err != nil {
			return bodyBytes, false
		}
		return newBody, true
	}

	return bodyBytes, false
}

func buildUnifiedMemoryText(memories []usage.MemoryEntry, turns []usage.ConversationTurn, sceResult *sce.HydrateResult) string {
	lines := make([]string, 0, 32)

	// Section 1: CliRelay local memories.
	if len(memories) > 0 {
		lines = append(lines, "Relevant memory:")
		for i, entry := range memories {
			lines = append(lines, formatInjectedBlock("Memory", i+1, entry.Content)...)
		}
	}

	// Section 2: SCE project knowledge (user memory + code/rule hits).
	if sceResult != nil && (len(sceResult.AppliedUserMemory) > 0 || len(sceResult.RelevantHits) > 0) {
		lines = append(lines, "Project knowledge:")

		for _, um := range sceResult.AppliedUserMemory {
			lines = append(lines, fmt.Sprintf("[User Preference] %s",
				truncateInjectionText(um.Content, 220)))
		}
		for _, hit := range sceResult.RelevantHits {
			label := hit.Kind
			switch {
			case strings.HasPrefix(hit.Kind, "memory:rule"):
				label = "Rule"
			case strings.HasPrefix(hit.Kind, "memory:consolidated"):
				label = "Pattern"
			case strings.HasPrefix(hit.Kind, "code:symbol"):
				label = "Code Symbol"
			case strings.HasPrefix(hit.Kind, "code:file"):
				label = "Code File"
			}
			lines = append(lines, fmt.Sprintf("[%s] score=%d %s [%s]",
				label, hit.Score,
				truncateInjectionText(hit.Title, 180),
				truncateInjectionText(hit.Path, 120)))
		}
	}

	// Section 3: Recent conversation context.
	if len(turns) > 0 {
		sortedTurns := append([]usage.ConversationTurn(nil), turns...)
		sort.SliceStable(sortedTurns, func(i, j int) bool {
			return sortedTurns[i].Timestamp.Before(sortedTurns[j].Timestamp)
		})
		lines = append(lines, "Recent conversation:")
		for i, turn := range sortedTurns {
			userText := strings.TrimSpace(turn.UserText)
			assistantText := strings.TrimSpace(turn.AssistantText)
			if userText == "" && assistantText == "" {
				continue
			}
			block := make([]string, 0, 3)
			block = append(block, fmt.Sprintf("[Turn %d]", i+1))
			if userText != "" {
				block = append(block, "  User: "+truncateInjectionText(userText, 240))
			}
			if assistantText != "" {
				block = append(block, "  Assistant: "+truncateInjectionText(assistantText, 320))
			}
			lines = append(lines, block...)
		}
	}

	if len(lines) == 0 {
		return ""
	}

	lines = append(lines, "Use this as supporting context only. Follow the current user request if there is any conflict.")
	return strings.Join(lines, "\n")
}

func formatInjectedBlock(label string, index int, content string) []string {
	lines := []string{fmt.Sprintf("[%s %d]", label, index)}
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, "  "+truncateInjectionText(line, 220))
	}
	return lines
}

func extractLatestUserTurn(bodyBytes []byte) string {
	if messages := gjson.GetBytes(bodyBytes, "messages"); messages.Exists() && messages.IsArray() {
		for i := len(messages.Array()) - 1; i >= 0; i-- {
			message := messages.Array()[i]
			role := strings.ToLower(strings.TrimSpace(message.Get("role").String()))
			if role != "user" {
				continue
			}
			if text := extractTextValue(message.Get("content")); text != "" {
				return text
			}
		}
		return ""
	}

	if input := gjson.GetBytes(bodyBytes, "input"); input.Exists() {
		return strings.TrimSpace(extractLatestInputText(input))
	}

	return ""
}

func extractLatestInputText(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}

	switch value.Type {
	case gjson.String:
		return strings.TrimSpace(value.String())
	case gjson.JSON:
		if value.IsArray() {
			items := value.Array()
			for i := len(items) - 1; i >= 0; i-- {
				if text := extractLatestInputText(items[i]); text != "" {
					return text
				}
			}
			return ""
		}

		itemType := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch itemType {
		case "function_call_output", "computer_call_output", "function_call", "tool_result", "output_text", "reasoning":
			return ""
		case "message":
			if role := strings.ToLower(strings.TrimSpace(value.Get("role").String())); role == "user" {
				return extractTextValue(value.Get("content"))
			}
			return ""
		case "input_text", "text":
			if text := strings.TrimSpace(value.Get("text").String()); text != "" {
				return text
			}
		}

		if role := strings.ToLower(strings.TrimSpace(value.Get("role").String())); role == "user" {
			if text := extractTextValue(value.Get("content")); text != "" {
				return text
			}
		}
		if text := strings.TrimSpace(value.Get("input_text").String()); text != "" {
			return text
		}
		if text := strings.TrimSpace(value.Get("text").String()); text != "" {
			return text
		}
		if content := value.Get("content"); content.Exists() {
			if text := extractLatestInputText(content); text != "" {
				return text
			}
		}
		if input := value.Get("input"); input.Exists() {
			if text := extractLatestInputText(input); text != "" {
				return text
			}
		}
	}

	return strings.TrimSpace(value.String())
}

func extractAssistantReply(bodyBytes []byte) string {
	bodyBytes = bytes.TrimSpace(bodyBytes)
	if len(bodyBytes) == 0 {
		return ""
	}

	if text := extractAssistantReplyFromJSON(bodyBytes); text != "" {
		return text
	}
	if bytes.Contains(bodyBytes, []byte("\ndata:")) || bytes.HasPrefix(bodyBytes, []byte("data:")) {
		return extractAssistantReplyFromSSE(bodyBytes)
	}
	return ""
}

func extractAssistantReplyFromJSON(bodyBytes []byte) string {
	root := gjson.ParseBytes(bodyBytes)
	if !root.Exists() {
		return ""
	}

	if content := root.Get("choices.0.message.content"); content.Exists() {
		if text := extractTextValue(content); text != "" {
			return text
		}
	}
	if text := strings.TrimSpace(root.Get("choices.0.text").String()); text != "" {
		return text
	}
	if output := root.Get("output"); output.Exists() && output.IsArray() {
		parts := make([]string, 0, len(output.Array()))
		for _, item := range output.Array() {
			if strings.ToLower(strings.TrimSpace(item.Get("role").String())) != "assistant" {
				continue
			}
			if text := extractTextValue(item.Get("content")); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	if content := root.Get("content"); content.Exists() {
		return extractTextValue(content)
	}
	return ""
}

func extractAssistantReplyFromSSE(bodyBytes []byte) string {
	lines := bytes.Split(bodyBytes, []byte("\n"))
	var deltaParts []string
	var finalParts []string
	for _, rawLine := range lines {
		line := strings.TrimSpace(string(rawLine))
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		root := gjson.Parse(payload)
		if !root.Exists() {
			continue
		}

		if delta := strings.TrimSpace(root.Get("choices.0.delta.content").String()); delta != "" {
			deltaParts = append(deltaParts, delta)
			continue
		}
		switch root.Get("type").String() {
		case "response.output_text.delta":
			if delta := strings.TrimSpace(root.Get("delta").String()); delta != "" {
				deltaParts = append(deltaParts, delta)
			}
		case "content_block_delta":
			if root.Get("delta.type").String() == "text_delta" {
				if delta := strings.TrimSpace(root.Get("delta.text").String()); delta != "" {
					deltaParts = append(deltaParts, delta)
				}
			}
		case "response.output_item.done":
			item := root.Get("item")
			if item.Get("type").String() == "message" && strings.ToLower(strings.TrimSpace(item.Get("role").String())) == "assistant" {
				if text := extractTextValue(item.Get("content")); text != "" {
					finalParts = append(finalParts, text)
				}
			}
		case "response.completed":
			output := root.Get("response.output")
			if output.Exists() && output.IsArray() {
				for _, item := range output.Array() {
					if strings.ToLower(strings.TrimSpace(item.Get("role").String())) != "assistant" {
						continue
					}
					if text := extractTextValue(item.Get("content")); text != "" {
						finalParts = append(finalParts, text)
					}
				}
			}
		}
	}

	if len(deltaParts) > 0 {
		return strings.TrimSpace(strings.Join(deltaParts, ""))
	}
	return strings.TrimSpace(strings.Join(finalParts, "\n"))
}

func extractTextValue(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}

	switch value.Type {
	case gjson.String:
		return strings.TrimSpace(value.String())
	case gjson.JSON:
		if value.IsArray() {
			parts := make([]string, 0, len(value.Array()))
			for _, item := range value.Array() {
				if text := extractTextValue(item); text != "" {
					parts = append(parts, text)
				}
			}
			return strings.TrimSpace(strings.Join(parts, "\n"))
		}

		if text := strings.TrimSpace(value.Get("text").String()); text != "" {
			return text
		}
		if text := strings.TrimSpace(value.Get("input_text").String()); text != "" {
			return text
		}
		if content := value.Get("content"); content.Exists() {
			if text := extractTextValue(content); text != "" {
				return text
			}
		}
		if input := value.Get("input"); input.Exists() {
			if text := extractTextValue(input); text != "" {
				return text
			}
		}
	}

	return strings.TrimSpace(value.String())
}

func truncateInjectionText(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
