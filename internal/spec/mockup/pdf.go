package mockup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

var (
	pdfPageMarker = regexp.MustCompile(`/Type\s*/Page[^s]`)
	pdfTextTj     = regexp.MustCompile(`\((?:\\.|[^\\)])*\)\s*Tj`)
	pdfTextTJ     = regexp.MustCompile(`\[(?:[^\]]|\[[^\]]*\])*\]\s*TJ`)
)

func pdfBasis(page int) string { return "pdf:page" + strconv.Itoa(page) }

// PDFHasTextLayer reports whether born-digital text can be extracted.
func PDFHasTextLayer(content []byte) bool {
	pages := extractPDFPageTexts(content)
	for _, p := range pages {
		if strings.TrimSpace(p) != "" {
			return true
		}
	}
	return false
}

// ExtractPDFText extracts deterministic text per page from born-digital PDFs.
func ExtractPDFText(content []byte) ([]ExtractedItem, error) {
	pages := extractPDFPageTexts(content)
	var items []ExtractedItem
	for i, text := range pages {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		page := i + 1
		items = append(items, ExtractedItem{
			Stage:      StageScreenComponents,
			Text:       "page text: " + text,
			Section:    "responsive",
			Label:      spec.LabelObserved,
			Confidence: 1.0,
			NodeRef:    pdfBasis(page),
		})
	}
	return items, nil
}

func extractPDFPageTexts(content []byte) []string {
	pageCount := len(pdfPageMarker.FindAll(content, -1))
	if pageCount == 0 {
		pageCount = 1
	}
	pages := make([]string, pageCount)
	streams := extractPDFStreams(content)
	combined := strings.Join(streams, "\n")
	text := extractPDFOperators(combined)
	if pageCount == 1 {
		pages[0] = text
		return pages
	}
	// Without page-object boundaries in streams, attribute all text to page 1.
	pages[0] = text
	return pages
}

func extractPDFStreams(content []byte) []string {
	var out []string
	needle := []byte("stream")
	end := []byte("endstream")
	for {
		i := bytes.Index(content, needle)
		if i < 0 {
			break
		}
		content = content[i+len(needle):]
		if len(content) > 0 && (content[0] == '\r' || content[0] == '\n') {
			content = content[1:]
		}
		j := bytes.Index(content, end)
		if j < 0 {
			break
		}
		out = append(out, string(content[:j]))
		content = content[j+len(end):]
	}
	return out
}

func extractPDFOperators(stream string) string {
	var parts []string
	for _, m := range pdfTextTj.FindAllString(stream, -1) {
		parts = append(parts, decodePDFString(m))
	}
	for _, m := range pdfTextTJ.FindAllString(stream, -1) {
		inner := regexp.MustCompile(`\((?:\\.|[^\\)])*\)`).FindAllString(m, -1)
		for _, s := range inner {
			parts = append(parts, decodePDFString(s))
		}
	}
	return strings.Join(parts, " ")
}

func decodePDFString(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ") Tj") {
		s = strings.TrimSuffix(s, ") Tj")
	}
	if strings.HasPrefix(s, "(") {
		s = s[1:]
	}
	if strings.HasSuffix(s, ")") {
		s = s[:len(s)-1]
	}
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\r`, "\r")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\(`, `(`)
	s = strings.ReplaceAll(s, `\)`, `)`)
	return strings.TrimSpace(s)
}

func extractPDF(ctx context.Context, artifact Artifact, vision ExtractorFunc) (Extraction, error) {
	raw, err := os.ReadFile(artifact.Path)
	if err != nil {
		return Extraction{}, fmt.Errorf("mockup pdf: read %s: %w", artifact.Path, err)
	}
	if !PDFHasTextLayer(raw) {
		return vision(ctx, artifact)
	}
	items, err := ExtractPDFText(raw)
	if err != nil {
		return Extraction{}, err
	}
	if len(items) == 0 {
		return vision(ctx, artifact)
	}
	return BuildExtraction("mockup", items), nil
}
