package responses

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIChatCompletionsStreamLengthFinishMarksIncomplete(t *testing.T) {
	var param any
	chunk := []byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1779888657,"model":"mimo-v2.5","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":0,"total_tokens":10}}`)

	events := ConvertOpenAIChatCompletionsResponseToOpenAIResponses(context.Background(), "mimo-v2.5", []byte(`{"model":"gpt-5.5"}`), []byte(`{"model":"gpt-5.5"}`), chunk, &param)

	var completed string
	for _, event := range events {
		if strings.HasPrefix(event, "event: response.completed") {
			completed = strings.TrimSpace(strings.TrimPrefix(strings.SplitN(event, "data:", 2)[1], " "))
		}
	}
	if completed == "" {
		t.Fatalf("missing response.completed event: %v", events)
	}
	if got := gjson.Get(completed, "response.status").String(); got != "incomplete" {
		t.Fatalf("response.status = %q, want incomplete; payload=%s", got, completed)
	}
	if got := gjson.Get(completed, "response.incomplete_details.reason").String(); got != "max_output_tokens" {
		t.Fatalf("incomplete_details.reason = %q, want max_output_tokens; payload=%s", got, completed)
	}
}

func TestConvertOpenAIChatCompletionsNonStreamLengthFinishMarksIncomplete(t *testing.T) {
	body := []byte(`{"id":"chatcmpl_1","created":1779888657,"model":"mimo-v2.5","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":0,"total_tokens":10}}`)

	got := ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(context.Background(), "mimo-v2.5", []byte(`{"model":"gpt-5.5"}`), []byte(`{"model":"gpt-5.5"}`), body, nil)

	if status := gjson.Get(got, "status").String(); status != "incomplete" {
		t.Fatalf("status = %q, want incomplete; payload=%s", status, got)
	}
	if reason := gjson.Get(got, "incomplete_details.reason").String(); reason != "max_output_tokens" {
		t.Fatalf("incomplete_details.reason = %q, want max_output_tokens; payload=%s", reason, got)
	}
}

func TestConvertOpenAIChatCompletionsStreamReasoningPinsEncryptedContent(t *testing.T) {
	var param any
	chunks := [][]byte{
		[]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1779888657,"model":"mimo-v2.5","choices":[{"index":0,"delta":{"reasoning_content":"think"},"finish_reason":null}]}`),
		[]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","created":1779888657,"model":"mimo-v2.5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
	}

	var events []string
	for _, chunk := range chunks {
		events = append(events, ConvertOpenAIChatCompletionsResponseToOpenAIResponses(context.Background(), "mimo-v2.5", []byte(`{"model":"gpt-5.5"}`), []byte(`{"model":"gpt-5.5"}`), chunk, &param)...)
	}

	var completed string
	for _, event := range events {
		if strings.HasPrefix(event, "event: response.completed") {
			completed = strings.TrimSpace(strings.TrimPrefix(strings.SplitN(event, "data:", 2)[1], " "))
		}
	}
	if completed == "" {
		t.Fatalf("missing response.completed event: %v", events)
	}
	if got := gjson.Get(completed, "response.output.0.encrypted_content").String(); got != "think" {
		t.Fatalf("encrypted_content = %q, want think; payload=%s", got, completed)
	}
}

func TestConvertOpenAIChatCompletionsNonStreamReasoningPinsEncryptedContent(t *testing.T) {
	body := []byte(`{"id":"chatcmpl_1","created":1779888657,"model":"mimo-v2.5","choices":[{"index":0,"message":{"role":"assistant","reasoning_content":"think","content":"ok"},"finish_reason":"stop"}]}`)

	got := ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(context.Background(), "mimo-v2.5", []byte(`{"model":"gpt-5.5"}`), []byte(`{"model":"gpt-5.5"}`), body, nil)

	if gotReasoning := gjson.Get(got, "output.0.encrypted_content").String(); gotReasoning != "think" {
		t.Fatalf("encrypted_content = %q, want think; payload=%s", gotReasoning, got)
	}
}
