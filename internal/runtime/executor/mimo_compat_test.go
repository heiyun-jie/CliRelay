package executor

import (
	"encoding/json"
	"strings"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

func TestApplyMimoCompatibilityPayloadNormalizesResponsesRequest(t *testing.T) {
	translated := []byte(`{
		"model":"mimo-v2.5-pro",
		"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}},{"type":"text","text":"read it"}]}],
		"max_tokens":123,
		"temperature":0.2,
		"tool_choice":"required",
		"stream":true
	}`)
	original := []byte(`{
		"model":"gpt-5.5",
		"instructions":"be terse",
		"input":[
			{"type":"message","role":"user","content":[
				{"type":"input_image","image_url":"data:image/png;base64,abc"},
				{"type":"input_text","text":"read it"}
			]},
			{"type":"reasoning","encrypted_content":"kept reasoning"},
			{"type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":[\"pwd\"]}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		],
		"tools":[
			{"type":"local_shell"},
			{"type":"tool_search","description":"search tools","parameters":{"type":"object"}},
			{"type":"file_search"}
		]
	}`)
	auth := &cliproxyauth.Auth{
		Provider: "openai-compatibility",
		Label:    "xiaomi-mimo",
		Attributes: map[string]string{
			"base_url": "https://token-plan-cn.xiaomimimo.com/v1",
			"api_key":  "tp-test",
		},
	}

	got := applyMimoCompatibilityPayload(translated, original, "mimo-v2.5-pro", true, auth, "openai-compatibility")

	if gjson.GetBytes(got, "max_tokens").Exists() {
		t.Fatalf("max_tokens should be removed: %s", got)
	}
	if gotMax := gjson.GetBytes(got, "max_completion_tokens").Int(); gotMax != 123 {
		t.Fatalf("max_completion_tokens = %d, want 123; body=%s", gotMax, got)
	}
	if gotThinking := gjson.GetBytes(got, "thinking.type").String(); gotThinking != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled; body=%s", gotThinking, got)
	}
	if gjson.GetBytes(got, "temperature").Exists() {
		t.Fatalf("temperature should be removed for mimo-v2.5-pro thinking mode: %s", got)
	}
	if gjson.GetBytes(got, "tool_choice").Exists() {
		t.Fatalf("non-auto tool_choice should be removed: %s", got)
	}
	if !gjson.GetBytes(got, "parallel_tool_calls").Bool() {
		t.Fatalf("parallel_tool_calls should be true: %s", got)
	}
	if !gjson.GetBytes(got, "stream_options.include_usage").Bool() {
		t.Fatalf("stream_options.include_usage should be true: %s", got)
	}
	if gotTool := gjson.GetBytes(got, "tools.0.function.name").String(); gotTool != "shell" {
		t.Fatalf("first tool = %q, want shell; body=%s", gotTool, got)
	}
	if gotTool := gjson.GetBytes(got, "tools.1.function.name").String(); gotTool != "tool_search" {
		t.Fatalf("second tool = %q, want tool_search; body=%s", gotTool, got)
	}
	if gjson.GetBytes(got, "tools.2").Exists() {
		t.Fatalf("unsupported file_search tool should be dropped: %s", got)
	}
	if gjson.GetBytes(got, "messages.2.reasoning_content").String() != "kept reasoning" {
		t.Fatalf("reasoning should be folded onto assistant tool call: %s", got)
	}
	if gotRole := gjson.GetBytes(got, "messages.3.role").String(); gotRole != "tool" {
		t.Fatalf("messages.3.role = %q, want tool; body=%s", gotRole, got)
	}
	content := gjson.GetBytes(got, "messages.2.content").String()
	if content != "" {
		t.Fatalf("assistant tool call content should be omitted or empty, got %q; body=%s", content, got)
	}
	if strings.Contains(gjson.GetBytes(got, "messages.1.content").Raw, "image_url") {
		t.Fatalf("mimo-v2.5-pro should not receive image_url parts: %s", got)
	}
	if !strings.Contains(gjson.GetBytes(got, "messages.1.content").String(), "image attachment omitted") {
		t.Fatalf("image-stripped user message should retain placeholder text: %s", got)
	}
}

func TestApplyMimoCompatibilityPayloadKeepsImagesForVisionModel(t *testing.T) {
	translated := []byte(`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,abc"}}]}]}`)
	auth := &cliproxyauth.Auth{Label: "xiaomi-mimo", Attributes: map[string]string{"api_key": "tp-test"}}

	got := applyMimoCompatibilityPayload(translated, nil, "mimo-v2.5", false, auth, "openai-compatibility")

	if !gjson.GetBytes(got, `messages.0.content.#(type=="image_url")`).Exists() {
		t.Fatalf("vision model should keep image_url: %s", got)
	}
	if gotText := gjson.GetBytes(got, `messages.0.content.#(type=="text").text`).String(); gotText != " " {
		t.Fatalf("image-only message should get blank text part, got %q; body=%s", gotText, got)
	}
}

func TestApplyMimoCompatibilityPayloadSanitizesChatTools(t *testing.T) {
	body := []byte(`{
		"model":"mimo-v2.5-pro",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning_effort":"none",
		"tools":[
			{"type":"function","function":{"name":"_fetch","strict":null,"parameters":{"type":"object"}}},
			{"type":"function","function":{"name":"_fetch","strict":true}},
			{"type":"web_search_preview"},
			{"type":"mcp","server_label":"github"}
		]
	}`)
	auth := &cliproxyauth.Auth{
		Label: "xiaomi-mimo",
		Attributes: map[string]string{
			"api_key":  "tp-test",
			"base_url": "https://token-plan-cn.xiaomimimo.com/v1",
		},
	}

	got := applyMimoCompatibilityPayload(body, nil, "mimo-v2.5-pro", false, auth, "openai-compatibility")

	if gjson.GetBytes(got, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort=none should be stripped for MiMo: %s", got)
	}
	if gjson.GetBytes(got, "tools.0.function.strict").Exists() {
		t.Fatalf("strict:null should be omitted before MiMo upstream: %s", got)
	}
	if gjson.GetBytes(got, "tools.1").Exists() {
		t.Fatalf("duplicate/web_search/mcp tools should be removed for token-plan MiMo: %s", got)
	}
}

func TestApplyMimoCompatibilityPayloadIgnoresNonMimo(t *testing.T) {
	body := []byte(`{"model":"qwen","max_tokens":123}`)
	auth := &cliproxyauth.Auth{Label: "qwen", Attributes: map[string]string{"base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1"}}
	got := applyMimoCompatibilityPayload(body, nil, "qwen", false, auth, "openai-compatibility")
	if string(got) != string(body) {
		var gotObj, wantObj any
		_ = json.Unmarshal(got, &gotObj)
		_ = json.Unmarshal(body, &wantObj)
		t.Fatalf("non-mimo body changed: got %#v want %#v", gotObj, wantObj)
	}
}
