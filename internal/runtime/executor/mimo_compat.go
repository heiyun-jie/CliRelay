package executor

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

const mimoMissingReasoningPlaceholder = "(this turn ran without thinking mode)"

var mimoServerSideToolTypes = map[string]struct{}{
	"code_interpreter":     {},
	"file_search":          {},
	"image_generation":     {},
	"computer_use_preview": {},
	"computer_use":         {},
	"mcp":                  {},
}

func isMimoCompatAuth(provider string, auth *cliproxyauth.Auth) bool {
	values := []string{provider}
	if auth != nil {
		values = append(values, auth.Provider, auth.Label)
		if auth.Attributes != nil {
			values = append(values,
				auth.Attributes["compat_name"],
				auth.Attributes["provider_key"],
				auth.Attributes["base_url"],
				auth.Attributes["api_key"],
			)
		}
	}
	for _, value := range values {
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "" {
			continue
		}
		if strings.Contains(lower, "mimo") ||
			strings.Contains(lower, "xiaomimimo.com") ||
			strings.Contains(lower, "token-plan-cn.xiaomimimo.com") ||
			strings.HasPrefix(lower, "tp-") {
			return true
		}
	}
	return false
}

func applyMimoCompatibilityPayload(body, originalResponses []byte, upstreamModel string, stream bool, auth *cliproxyauth.Auth, provider string) []byte {
	if !isMimoCompatAuth(provider, auth) || len(body) == 0 {
		return body
	}

	payload, ok := decodeJSONObject(body)
	if !ok {
		return body
	}

	model := strings.TrimSpace(mimoStringValue(payload["model"]))
	if model == "" {
		model = strings.TrimSpace(upstreamModel)
	}
	if model != "" {
		payload["model"] = model
	}

	if responsesRoot, ok := decodeJSONObject(originalResponses); ok && looksLikeResponsesRequest(responsesRoot) {
		if messages := mimoResponsesInputToMessages(responsesRoot, model); len(messages) > 0 {
			payload["messages"] = messages
		}
		tools := mimoResponsesToolsToChat(responsesRoot["tools"], !mimoTokenPlanAuth(auth))
		if len(tools) > 0 {
			payload["tools"] = tools
		} else {
			delete(payload, "tools")
		}
	}

	normalizeMimoChatTools(payload, !mimoTokenPlanAuth(auth))
	normalizeMimoImages(payload, model)
	normalizeMimoGenerationFields(payload, model, stream)
	backfillMimoReasoningContent(payload)

	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func decodeJSONObject(raw []byte) (map[string]any, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	var value map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, false
	}
	return value, true
}

func looksLikeResponsesRequest(root map[string]any) bool {
	if root == nil {
		return false
	}
	if _, ok := root["input"]; ok {
		return true
	}
	if _, ok := root["instructions"]; ok {
		return true
	}
	return false
}

func mimoTokenPlanAuth(auth *cliproxyauth.Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(auth.Attributes["api_key"]))
	baseURL := strings.ToLower(strings.TrimSpace(auth.Attributes["base_url"]))
	return strings.HasPrefix(key, "tp-") || strings.Contains(baseURL, "token-plan")
}

func normalizeMimoGenerationFields(payload map[string]any, model string, stream bool) {
	if value, ok := payload["max_tokens"]; ok {
		if _, exists := payload["max_completion_tokens"]; !exists {
			payload["max_completion_tokens"] = value
		}
		delete(payload, "max_tokens")
	}

	if stream || mimoBoolValue(payload["stream"]) {
		payload["stream"] = true
		payload["stream_options"] = map[string]any{"include_usage": true}
	}

	payload["parallel_tool_calls"] = true

	modelLower := strings.ToLower(strings.TrimSpace(model))
	thinking := mimoMapValue(payload["thinking"])
	if thinking == nil && modelLower != "mimo-v2-flash" {
		thinking = map[string]any{"type": "enabled"}
		payload["thinking"] = thinking
	}

	if effort := strings.ToLower(strings.TrimSpace(mimoStringValue(payload["reasoning_effort"]))); effort != "" {
		switch effort {
		case "minimal":
			payload["reasoning_effort"] = "low"
		case "none":
			delete(payload, "reasoning_effort")
		}
	}

	if strings.EqualFold(mimoStringValue(thinking["type"]), "enabled") &&
		(modelLower == "mimo-v2.5-pro" || modelLower == "mimo-v2.5") {
		delete(payload, "temperature")
	}

	if rawChoice, ok := payload["tool_choice"]; ok {
		choice := strings.ToLower(strings.TrimSpace(mimoStringValue(rawChoice)))
		if choice != "" && choice != "auto" {
			delete(payload, "tool_choice")
		}
	}
}

func backfillMimoReasoningContent(payload map[string]any) {
	thinking := mimoMapValue(payload["thinking"])
	if strings.EqualFold(mimoStringValue(thinking["type"]), "disabled") {
		return
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return
	}
	for _, raw := range messages {
		msg := mimoMapValue(raw)
		if msg == nil || !strings.EqualFold(mimoStringValue(msg["role"]), "assistant") {
			continue
		}
		if strings.TrimSpace(mimoStringValue(msg["reasoning_content"])) != "" {
			continue
		}
		msg["reasoning_content"] = mimoMissingReasoningPlaceholder
	}
}

func normalizeMimoImages(payload map[string]any, model string) {
	messages, ok := payload["messages"].([]any)
	if !ok {
		return
	}
	supportsImages := mimoModelSupportsImages(model)
	for _, raw := range messages {
		msg := mimoMapValue(raw)
		if msg == nil {
			continue
		}
		parts, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		normalized := normalizeMimoContentParts(parts, supportsImages)
		msg["content"] = normalized
	}
}

func normalizeMimoChatTools(payload map[string]any, enableWebSearch bool) {
	rawTools, ok := payload["tools"].([]any)
	if !ok {
		return
	}
	tools := make([]any, 0, len(rawTools))
	for _, raw := range rawTools {
		tool := mimoMapValue(raw)
		if tool == nil {
			continue
		}
		toolType := strings.ToLower(strings.TrimSpace(mimoStringValue(tool["type"])))
		switch {
		case toolType == "function":
			fn := mimoMapValue(tool["function"])
			if fn == nil || strings.TrimSpace(mimoStringValue(fn["name"])) == "" {
				continue
			}
			if _, ok := fn["strict"].(bool); !ok {
				delete(fn, "strict")
			}
			tools = append(tools, tool)
		case toolType == "web_search" || toolType == "web_search_preview":
			if enableWebSearch {
				if toolType == "web_search_preview" {
					tool["type"] = "web_search"
				}
				tools = append(tools, tool)
			}
		default:
			if _, skip := mimoServerSideToolTypes[toolType]; skip {
				continue
			}
			tools = append(tools, tool)
		}
	}
	tools = dedupeMimoTools(tools)
	if len(tools) == 0 {
		delete(payload, "tools")
		return
	}
	payload["tools"] = tools
}

func normalizeMimoContentParts(parts []any, supportsImages bool) any {
	out := make([]any, 0, len(parts)+1)
	hasImage := false
	hasText := false
	droppedImages := 0
	for _, raw := range parts {
		part := mimoMapValue(raw)
		if part == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(mimoStringValue(part["type"]))) {
		case "text":
			text := mimoStringValue(part["text"])
			if text == "" {
				continue
			}
			out = append(out, map[string]any{"type": "text", "text": text})
			hasText = true
		case "image_url":
			if supportsImages {
				out = append(out, part)
				hasImage = true
			} else {
				droppedImages++
			}
		default:
			out = append(out, part)
		}
	}
	if droppedImages > 0 {
		plural := ""
		if droppedImages > 1 {
			plural = "s"
		}
		out = append(out, map[string]any{
			"type": "text",
			"text": "[" + mimoIntString(droppedImages) + " image attachment" + plural + " omitted because the active MiMo model does not support image input. Use mimo-v2.5 or mimo-v2-omni for vision.]",
		})
		hasText = true
	}
	if hasImage && !hasText {
		out = append(out, map[string]any{"type": "text", "text": " "})
		hasText = true
	}
	if len(out) == 0 {
		return ""
	}
	if !hasImage && mimoAllTextParts(out) {
		var b strings.Builder
		for _, raw := range out {
			part := mimoMapValue(raw)
			b.WriteString(mimoStringValue(part["text"]))
		}
		return b.String()
	}
	return out
}

func mimoModelSupportsImages(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "mimo-v2.5" || strings.Contains(model, "omni")
}

type mimoAssemblyState struct {
	pendingReasoning string
	pendingText      *string
	pendingTools     []any
}

func mimoResponsesInputToMessages(root map[string]any, model string) []any {
	var messages []any
	supportsImages := mimoModelSupportsImages(model)
	if instructions := mimoStringValue(root["instructions"]); instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}

	flush := func(state *mimoAssemblyState) {
		if state.pendingReasoning == "" && state.pendingText == nil && len(state.pendingTools) == 0 {
			return
		}
		msg := map[string]any{"role": "assistant"}
		if state.pendingText != nil {
			msg["content"] = *state.pendingText
		} else if len(state.pendingTools) == 0 {
			msg["content"] = ""
		}
		if len(state.pendingTools) > 0 {
			msg["tool_calls"] = state.pendingTools
		}
		if state.pendingReasoning != "" {
			msg["reasoning_content"] = state.pendingReasoning
		}
		messages = append(messages, msg)
		state.pendingReasoning = ""
		state.pendingText = nil
		state.pendingTools = nil
	}

	state := &mimoAssemblyState{}
	switch input := root["input"].(type) {
	case string:
		messages = append(messages, map[string]any{"role": "user", "content": input})
	case []any:
		for _, raw := range input {
			item := mimoMapValue(raw)
			if item == nil {
				continue
			}
			itemType := strings.ToLower(strings.TrimSpace(mimoStringValue(item["type"])))
			if itemType == "" && mimoStringValue(item["role"]) != "" {
				itemType = "message"
			}
			switch itemType {
			case "message", "":
				role := strings.ToLower(strings.TrimSpace(mimoStringValue(item["role"])))
				if role == "developer" {
					role = "system"
				}
				if role == "" {
					role = "user"
				}
				content := mimoResponsesContentToChat(item["content"], supportsImages)
				if role == "assistant" {
					if state.pendingText != nil {
						flush(state)
					}
					text := ""
					if s, ok := content.(string); ok {
						text = s
					}
					state.pendingText = &text
				} else {
					flush(state)
					messages = append(messages, map[string]any{"role": role, "content": content})
				}
			case "reasoning":
				text := mimoReasoningText(item)
				if text == "" {
					continue
				}
				if len(state.pendingTools) > 0 || state.pendingText != nil {
					state.pendingReasoning += text
				} else {
					flush(state)
					state.pendingReasoning = text
				}
			case "function_call":
				callID := mimoStringValue(item["call_id"])
				if callID == "" {
					callID = mimoStringValue(item["id"])
				}
				state.pendingTools = append(state.pendingTools, map[string]any{
					"id":   callID,
					"type": "function",
					"function": map[string]any{
						"name":      mimoStringValue(item["name"]),
						"arguments": mimoStringValue(item["arguments"]),
					},
				})
			case "function_call_output":
				flush(state)
				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": mimoStringValue(item["call_id"]),
					"content":      mimoToolOutputToString(item["output"]),
				})
			}
		}
	}
	flush(state)
	removeMimoOrphanToolMessages(&messages)
	ensureMimoToolCallsHaveOutputs(&messages)
	return messages
}

func mimoResponsesContentToChat(raw any, supportsImages bool) any {
	switch content := raw.(type) {
	case string:
		return content
	case []any:
		parts := make([]any, 0, len(content))
		for _, rawPart := range content {
			part := mimoMapValue(rawPart)
			if part == nil {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(mimoStringValue(part["type"]))) {
			case "input_text", "output_text", "text", "":
				text := mimoStringValue(part["text"])
				if text != "" {
					parts = append(parts, map[string]any{"type": "text", "text": text})
				}
			case "input_image", "image_url":
				imageURL := mimoStringValue(part["image_url"])
				if imageURL == "" {
					if imageObj := mimoMapValue(part["image_url"]); imageObj != nil {
						imageURL = mimoStringValue(imageObj["url"])
					}
				}
				if imageURL == "" {
					continue
				}
				imagePart := map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}}
				if detail := mimoStringValue(part["detail"]); detail != "" {
					imagePart["image_url"].(map[string]any)["detail"] = detail
				}
				parts = append(parts, imagePart)
			}
		}
		return normalizeMimoContentParts(parts, supportsImages)
	default:
		return ""
	}
}

func mimoReasoningText(item map[string]any) string {
	if text := mimoStringValue(item["encrypted_content"]); text != "" {
		return text
	}
	var b strings.Builder
	if summaries, ok := item["summary"].([]any); ok {
		for _, raw := range summaries {
			summary := mimoMapValue(raw)
			if strings.EqualFold(mimoStringValue(summary["type"]), "summary_text") {
				b.WriteString(mimoStringValue(summary["text"]))
			}
		}
	}
	return b.String()
}

func mimoToolOutputToString(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case []any:
		var b strings.Builder
		droppedImages := 0
		for _, rawPart := range value {
			part := mimoMapValue(rawPart)
			if part == nil {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(mimoStringValue(part["type"]))) {
			case "input_text", "output_text", "text", "":
				b.WriteString(mimoStringValue(part["text"]))
			case "input_image", "image_url":
				droppedImages++
			}
		}
		if droppedImages > 0 {
			b.WriteString("[" + mimoIntString(droppedImages) + " image attachment(s) omitted from tool output.]")
		}
		return b.String()
	default:
		if raw == nil {
			return ""
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return mimoStringValue(raw)
		}
		return string(encoded)
	}
}

func removeMimoOrphanToolMessages(messages *[]any) {
	valid := map[string]struct{}{}
	out := make([]any, 0, len(*messages))
	for _, raw := range *messages {
		msg := mimoMapValue(raw)
		if msg == nil {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(mimoStringValue(msg["role"])))
		switch role {
		case "assistant":
			valid = map[string]struct{}{}
			if calls, ok := msg["tool_calls"].([]any); ok {
				for _, rawCall := range calls {
					if id := mimoStringValue(mimoMapValue(rawCall)["id"]); id != "" {
						valid[id] = struct{}{}
					}
				}
			}
			out = append(out, msg)
		case "tool":
			if _, ok := valid[mimoStringValue(msg["tool_call_id"])]; ok {
				out = append(out, msg)
			}
		default:
			valid = map[string]struct{}{}
			out = append(out, msg)
		}
	}
	*messages = out
}

func ensureMimoToolCallsHaveOutputs(messages *[]any) {
	for i := 0; i < len(*messages); i++ {
		msg := mimoMapValue((*messages)[i])
		if msg == nil || !strings.EqualFold(mimoStringValue(msg["role"]), "assistant") {
			continue
		}
		calls, ok := msg["tool_calls"].([]any)
		if !ok || len(calls) == 0 {
			continue
		}
		seen := map[string]struct{}{}
		j := i + 1
		for j < len(*messages) {
			next := mimoMapValue((*messages)[j])
			if next == nil || !strings.EqualFold(mimoStringValue(next["role"]), "tool") {
				break
			}
			seen[mimoStringValue(next["tool_call_id"])] = struct{}{}
			j++
		}
		var placeholders []any
		for _, rawCall := range calls {
			call := mimoMapValue(rawCall)
			id := mimoStringValue(call["id"])
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			placeholders = append(placeholders, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
				"content":      "[tool output missing - no function_call_output was provided for this call_id]",
			})
		}
		if len(placeholders) == 0 {
			continue
		}
		*messages = append((*messages)[:j], append(placeholders, (*messages)[j:]...)...)
		i = j + len(placeholders) - 1
	}
}

func mimoResponsesToolsToChat(raw any, enableWebSearch bool) []any {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	var out []any
	for _, rawTool := range items {
		out = append(out, mimoToolToChat(rawTool, enableWebSearch)...)
	}
	return dedupeMimoTools(out)
}

func mimoToolToChat(raw any, enableWebSearch bool) []any {
	tool := mimoMapValue(raw)
	if tool == nil {
		return nil
	}
	toolType := strings.ToLower(strings.TrimSpace(mimoStringValue(tool["type"])))
	switch toolType {
	case "function", "":
		name := mimoStringValue(tool["name"])
		if name == "" {
			return nil
		}
		fn := map[string]any{"name": name}
		if desc := mimoStringValue(tool["description"]); desc != "" {
			fn["description"] = desc
		}
		if params, ok := tool["parameters"]; ok {
			fn["parameters"] = params
		}
		if strict, ok := tool["strict"].(bool); ok {
			fn["strict"] = strict
		}
		return []any{map[string]any{"type": "function", "function": fn}}
	case "local_shell":
		return []any{mimoLocalShellTool()}
	case "custom":
		name := mimoStringValue(tool["name"])
		if name == "" {
			return nil
		}
		desc := mimoStringValue(tool["description"])
		if format := mimoMapValue(tool["format"]); format != nil && mimoStringValue(format["type"]) != "" {
			desc = strings.TrimSpace(desc + ` (originally a "` + mimoStringValue(format["type"]) + `"-format custom tool; output should follow that format).`)
		}
		return []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": desc,
				"parameters": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
					"properties": map[string]any{
						"input": map[string]any{"type": "string", "description": "Input text for the tool."},
					},
				},
			},
		}}
	case "namespace":
		nested, ok := tool["tools"].([]any)
		if !ok {
			return nil
		}
		var out []any
		for _, rawNested := range nested {
			out = append(out, mimoToolToChat(rawNested, enableWebSearch)...)
		}
		return out
	case "tool_search":
		fn := map[string]any{"name": "tool_search"}
		if desc := mimoStringValue(tool["description"]); desc != "" {
			fn["description"] = desc
		}
		if params, ok := tool["parameters"]; ok {
			fn["parameters"] = params
		}
		return []any{map[string]any{"type": "function", "function": fn}}
	case "web_search", "web_search_preview":
		if !enableWebSearch {
			return nil
		}
		webTool := map[string]any{"type": "web_search"}
		for _, key := range []string{"user_location", "max_keyword", "force_search", "limit"} {
			if value, ok := tool[key]; ok {
				webTool[key] = value
			}
		}
		return []any{webTool}
	default:
		if _, skip := mimoServerSideToolTypes[toolType]; skip {
			return nil
		}
		return nil
	}
}

func mimoLocalShellTool() any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "shell",
			"description": "Execute a shell command on the local machine. Returns stdout, stderr and exit code.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": `Argv array, e.g. ["ls", "-la"]. The first element is the program; remaining elements are arguments.`},
					"workdir":    map[string]any{"type": "string", "description": "Working directory to run the command in (optional)."},
					"timeout_ms": map[string]any{"type": "number", "description": "Timeout in milliseconds (optional, default 30000)."},
				},
				"required": []any{"command"},
			},
		},
	}
}

func dedupeMimoTools(tools []any) []any {
	seen := map[string]struct{}{}
	out := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool := mimoMapValue(raw)
		if tool == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(mimoStringValue(tool["type"])))
		if key == "function" {
			key = "fn:" + strings.ToLower(strings.TrimSpace(mimoStringValue(mimoMapValue(tool["function"])["name"])))
		} else {
			key = "builtin:" + key
		}
		if key == "fn:" || key == "builtin:" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tool)
	}
	return out
}

func mimoMapValue(raw any) map[string]any {
	if raw == nil {
		return nil
	}
	if value, ok := raw.(map[string]any); ok {
		return value
	}
	return nil
}

func mimoStringValue(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	case nil:
		return ""
	default:
		return strings.TrimSpace(strings.Trim(mimoStringFromJSON(value), `"`))
	}
}

func mimoBoolValue(raw any) bool {
	value, ok := raw.(bool)
	return ok && value
}

func mimoAllTextParts(parts []any) bool {
	if len(parts) == 0 {
		return false
	}
	for _, raw := range parts {
		if !strings.EqualFold(mimoStringValue(mimoMapValue(raw)["type"]), "text") {
			return false
		}
	}
	return true
}

func mimoStringFromJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func mimoIntString(value int) string {
	return strconv.Itoa(value)
}
