package executor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestCompressImageDataURLsInJSONCompressesResponsesPNG(t *testing.T) {
	dataURL := testNoisyPNGDataURL(t)
	raw := []byte(`{"model":"gpt-5.5","input":[{"role":"user","content":[{"type":"input_text","text":"what is this?"},{"type":"input_image","image_url":"` + dataURL + `"}]}]}`)

	out := compressImageDataURLsInJSON(raw)
	if bytes.Equal(out, raw) {
		t.Fatal("expected image data URL to be compressed")
	}
	if len(out) >= len(raw) {
		t.Fatalf("compressed payload len = %d, want smaller than %d", len(out), len(raw))
	}
	if !bytes.Contains(out, []byte(`data:image/jpeg;base64,`)) {
		t.Fatalf("compressed payload should contain JPEG data URL, got prefix: %s", string(out[:minInt(len(out), 200)]))
	}

	var parsed struct {
		Input []struct {
			Content []struct {
				Type     string `json:"type"`
				ImageURL string `json:"image_url"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("compressed payload should be valid JSON: %v", err)
	}
	got := parsed.Input[0].Content[1].ImageURL
	if !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Fatalf("image_url = %q, want JPEG data URL", got[:minInt(len(got), 64)])
	}
}

func TestCompressImageDataURLsInJSONCompressesChatImageURL(t *testing.T) {
	dataURL := testNoisyPNGDataURL(t)
	raw := []byte(`{"model":"mimo-v2.5","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"` + dataURL + `"}}]}]}`)

	out := compressImageDataURLsInJSON(raw)
	if bytes.Equal(out, raw) {
		t.Fatal("expected nested image_url.url data URL to be compressed")
	}
	if !bytes.Contains(out, []byte(`data:image/jpeg;base64,`)) {
		t.Fatalf("compressed payload should contain JPEG data URL")
	}
}

func TestCompressImageDataURLsInJSONLeavesExternalAndInvalidURLs(t *testing.T) {
	raw := []byte(`{"messages":[{"content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}},{"type":"image_url","image_url":{"url":"data:image/png;base64,not-valid"}}]}]}`)

	out := compressImageDataURLsInJSON(raw)
	if !bytes.Equal(out, raw) {
		t.Fatalf("payload changed unexpectedly:\n%s", out)
	}
}

func testNoisyPNGDataURL(t *testing.T) string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	var seed uint32 = 1
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			seed = seed*1664525 + 1013904223
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(seed >> 24),
				G: uint8(seed >> 16),
				B: uint8(seed >> 8),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
