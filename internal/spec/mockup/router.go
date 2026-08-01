package mockup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtractorFunc is the common extractor shape every format branch satisfies.
type ExtractorFunc func(ctx context.Context, artifact Artifact) (Extraction, error)

// Router maps detected formats to extractors and fails closed on unknown input.
type Router struct {
	extractors map[Format]ExtractorFunc
	vision     CassetteVisionExtractor
}

// RouterConfig configures cassette-backed vision routing.
type RouterConfig struct {
	CassetteDir string
}

// NewRouter builds a fail-closed format router.
func NewRouter(cfg RouterConfig) *Router {
	vision := CassetteVisionExtractor{Dir: cfg.CassetteDir}
	r := &Router{
		extractors: make(map[Format]ExtractorFunc),
		vision:     vision,
	}
	visionFn := func(ctx context.Context, art Artifact) (Extraction, error) {
		return extractVision(ctx, art, vision)
	}
	r.extractors[FormatHTML] = extractHTML
	r.extractors[FormatFigma] = extractFigma
	r.extractors[FormatPDF] = func(ctx context.Context, art Artifact) (Extraction, error) {
		return extractPDF(ctx, art, visionFn)
	}
	for _, f := range []Format{FormatPNG, FormatJPEG, FormatGIF, FormatWebP} {
		r.extractors[f] = visionFn
	}
	return r
}

func extractFigma(ctx context.Context, artifact Artifact) (Extraction, error) {
	_ = ctx
	raw, err := os.ReadFile(artifact.Path)
	if err != nil {
		return Extraction{}, fmt.Errorf("mockup figma: read %s: %w", artifact.Path, err)
	}
	var file FigmaFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return Extraction{}, fmt.Errorf("mockup figma: decode %s: %w", artifact.Path, err)
	}
	return ExtractFigma(file), nil
}

// Extract detects format from artifact content and routes to the matching extractor.
func (r *Router) Extract(ctx context.Context, artifact Artifact, content []byte) (Extraction, error) {
	format, err := Detect(content, filepath.Ext(artifact.Name))
	if err != nil {
		return Extraction{}, err
	}
	fn, ok := r.extractors[format]
	if !ok || fn == nil {
		return Extraction{}, fmt.Errorf("mockup router: no extractor for format %q", format)
	}
	return fn(ctx, artifact)
}

// MediaTypeForFormat maps a detected format to an HTTP media type for ingest.
func MediaTypeForFormat(f Format) string {
	switch f {
	case FormatHTML:
		return "text/html"
	case FormatPDF:
		return "application/pdf"
	case FormatPNG:
		return "image/png"
	case FormatJPEG:
		return "image/jpeg"
	case FormatGIF:
		return "image/gif"
	case FormatWebP:
		return "image/webp"
	case FormatFigma:
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// ExtractFile is a convenience wrapper: read path, ingest artifact metadata, route.
func (r *Router) ExtractFile(ctx context.Context, inputPath string, now func() (Artifact, error)) (Extraction, error) {
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return Extraction{}, fmt.Errorf("mockup router: read %s: %w", inputPath, err)
	}
	format, err := Detect(raw, filepath.Ext(inputPath))
	if err != nil {
		return Extraction{}, err
	}
	var artifact Artifact
	if now != nil {
		artifact, err = now()
		if err != nil {
			return Extraction{}, err
		}
	} else {
		artifact = Artifact{
			Name:      filepath.Base(inputPath),
			MediaType: MediaTypeForFormat(format),
			Path:      inputPath,
		}
	}
	if strings.TrimSpace(artifact.Path) == "" {
		artifact.Path = inputPath
	}
	return r.Extract(ctx, artifact, raw)
}
