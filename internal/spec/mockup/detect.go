package mockup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Format identifies a mockup input by content, not extension alone.
type Format string

const (
	FormatHTML   Format = "html"
	FormatPDF    Format = "pdf"
	FormatPNG    Format = "png"
	FormatJPEG   Format = "jpeg"
	FormatGIF    Format = "gif"
	FormatWebP   Format = "webp"
	FormatFigma  Format = "figma"
	FormatUnknown Format = ""
)

// Detect resolves input format by content sniffing. Extension is a hint only
// when magic bytes are inconclusive.
func Detect(content []byte, ext string) (Format, error) {
	if len(content) == 0 {
		return FormatUnknown, fmt.Errorf("mockup detect: empty input")
	}
	if isPDF(content) {
		return FormatPDF, nil
	}
	if isPNG(content) {
		return FormatPNG, nil
	}
	if isJPEG(content) {
		return FormatJPEG, nil
	}
	if isGIF(content) {
		return FormatGIF, nil
	}
	if isWebP(content) {
		return FormatWebP, nil
	}
	if isHTML(content) {
		return FormatHTML, nil
	}
	if isFigmaJSON(content) {
		return FormatFigma, nil
	}
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "html", "htm":
		if looksLikeHTML(content) {
			return FormatHTML, nil
		}
	case "pdf":
		return FormatUnknown, fmt.Errorf("mockup detect: %q does not look like a PDF", ext)
	case "png":
		return FormatUnknown, fmt.Errorf("mockup detect: %q does not look like a PNG", ext)
	case "jpg", "jpeg":
		return FormatUnknown, fmt.Errorf("mockup detect: %q does not look like a JPEG", ext)
	case "gif":
		return FormatUnknown, fmt.Errorf("mockup detect: %q does not look like a GIF", ext)
	case "webp":
		return FormatUnknown, fmt.Errorf("mockup detect: %q does not look like WebP", ext)
	case "json":
		if isFigmaJSON(content) {
			return FormatFigma, nil
		}
	}
	return FormatUnknown, fmt.Errorf("mockup detect: unrecognized format")
}

func isPDF(b []byte) bool {
	return len(b) >= 5 && bytes.HasPrefix(b, []byte("%PDF-"))
}

func isPNG(b []byte) bool {
	return len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
}

func isJPEG(b []byte) bool {
	return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

func isGIF(b []byte) bool {
	return len(b) >= 6 && (bytes.HasPrefix(b, []byte("GIF87a")) || bytes.HasPrefix(b, []byte("GIF89a")))
}

func isWebP(b []byte) bool {
	return len(b) >= 12 && bytes.HasPrefix(b, []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP"))
}

func isHTML(b []byte) bool {
	trim := bytes.TrimSpace(b)
	if len(trim) == 0 {
		return false
	}
	if bytes.HasPrefix(trim, []byte("<!DOCTYPE")) || bytes.HasPrefix(trim, []byte("<!doctype")) {
		return true
	}
	if bytes.HasPrefix(trim, []byte("<html")) || bytes.HasPrefix(trim, []byte("<HTML")) {
		return true
	}
	return looksLikeHTML(trim)
}

func looksLikeHTML(b []byte) bool {
	lower := bytes.ToLower(b)
	return bytes.Contains(lower, []byte("<html")) ||
		bytes.Contains(lower, []byte("<head")) ||
		bytes.Contains(lower, []byte("<body")) ||
		bytes.Contains(lower, []byte("<div")) ||
		bytes.Contains(lower, []byte("<h1"))
}

func isFigmaJSON(b []byte) bool {
	trim := bytes.TrimSpace(b)
	if len(trim) == 0 || trim[0] != '{' {
		return false
	}
	var probe struct {
		Document json.RawMessage `json:"document"`
	}
	if err := json.Unmarshal(trim, &probe); err != nil {
		return false
	}
	return len(probe.Document) > 0
}

// IsImageFormat reports whether f is a raster image format routed to vision.
func IsImageFormat(f Format) bool {
	switch f {
	case FormatPNG, FormatJPEG, FormatGIF, FormatWebP:
		return true
	default:
		return false
	}
}
