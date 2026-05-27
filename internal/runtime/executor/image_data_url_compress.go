package executor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"strings"
)

const (
	imageDataURLPrefix              = "data:image/"
	imageDataURLBase64Marker        = ";base64,"
	imageDataURLCompressedJPEGMIME  = "image/jpeg"
	imageDataURLCompressMinURLBytes = 32 << 10
	imageDataURLJPEGQuality         = 75
)

// compressImageDataURLsInJSON rewrites inline PNG/JPEG data URLs to compressed
// JPEG data URLs before forwarding provider payloads. Public image URLs and
// data URLs that cannot be decoded are left unchanged.
func compressImageDataURLsInJSON(body []byte) []byte {
	if !bytes.Contains(body, []byte(imageDataURLPrefix)) {
		return body
	}

	var payload any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return body
	}
	if !compressImageDataURLsInValue(&payload) {
		return body
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func compressImageDataURLsInValue(value *any) bool {
	if value == nil {
		return false
	}
	switch typed := (*value).(type) {
	case string:
		compressed, ok := compressImageDataURLString(typed)
		if !ok {
			return false
		}
		*value = compressed
		return true
	case []any:
		changed := false
		for i := range typed {
			item := typed[i]
			if compressImageDataURLsInValue(&item) {
				typed[i] = item
				changed = true
			}
		}
		return changed
	case map[string]any:
		changed := false
		for key, item := range typed {
			if compressImageDataURLsInValue(&item) {
				typed[key] = item
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func compressImageDataURLString(raw string) (string, bool) {
	if len(raw) < imageDataURLCompressMinURLBytes {
		return "", false
	}

	header, encoded, ok := strings.Cut(raw, ",")
	if !ok || header == "" || encoded == "" {
		return "", false
	}
	headerLower := strings.ToLower(header)
	if !strings.HasPrefix(headerLower, imageDataURLPrefix) || !strings.Contains(headerLower, strings.TrimSuffix(imageDataURLBase64Marker, ",")) {
		return "", false
	}

	mimeType := strings.TrimPrefix(strings.Split(headerLower, ";")[0], "data:")
	if mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/jpg" {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", false
	}

	img, err := decodeCompressibleImage(mimeType, decoded)
	if err != nil {
		return "", false
	}
	bounds := img.Bounds()
	if bounds.Empty() {
		return "", false
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flattenImageForJPEG(img), &jpeg.Options{Quality: imageDataURLJPEGQuality}); err != nil {
		return "", false
	}
	compressed := "data:" + imageDataURLCompressedJPEGMIME + imageDataURLBase64Marker + base64.StdEncoding.EncodeToString(buf.Bytes())
	if len(compressed) >= len(raw) {
		return "", false
	}
	return compressed, true
}

func decodeCompressibleImage(mimeType string, data []byte) (image.Image, error) {
	reader := bytes.NewReader(data)
	switch mimeType {
	case "image/png":
		return png.Decode(reader)
	case "image/jpeg", "image/jpg":
		return jpeg.Decode(reader)
	default:
		return nil, image.ErrFormat
	}
}

func flattenImageForJPEG(src image.Image) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Over)
	return dst
}
