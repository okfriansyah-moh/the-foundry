package mockup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

func TestRouter_HTMLFixture(t *testing.T) {
	input := filepath.Join("..", "..", "..", "test", "fixtures", "mockup", "landing.html")
	raw, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "landing.html")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	router := NewRouter(RouterConfig{CassetteDir: filepath.Join("..", "..", "..", "test", "cassettes", "mockup")})
	out, err := router.Extract(context.Background(), Artifact{Name: "landing.html", Path: path}, raw)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(out.SeedRequirements) == 0 {
		t.Fatal("expected non-empty extraction")
	}
	for _, r := range out.SeedRequirements {
		if r.Label == spec.LabelObserved && !strings.HasPrefix(r.Basis, "html:") {
			t.Fatalf("Observed HTML item missing html: basis: %+v", r)
		}
	}
}

func TestRouter_RefusesUnknownFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("plain notes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	router := NewRouter(RouterConfig{CassetteDir: t.TempDir()})
	_, err := router.Extract(context.Background(), Artifact{Name: "notes.txt", Path: path}, []byte("plain notes"))
	if err == nil || !strings.Contains(err.Error(), "unrecognized") {
		t.Fatalf("expected unrecognized format error, got %v", err)
	}
}

func TestRouter_ScannedPDFRoutesToVision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scanned.pdf")
	if err := os.WriteFile(path, scannedPDFFixture(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cassetteDir := filepath.Join("..", "..", "..", "test", "cassettes", "mockup")
	router := NewRouter(RouterConfig{CassetteDir: cassetteDir})
	out, err := router.Extract(context.Background(), Artifact{Name: "scanned.pdf", Path: path}, scannedPDFFixture())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(out.SeedRequirements) == 0 {
		t.Fatal("expected vision-routed extraction")
	}
	for _, r := range out.SeedRequirements {
		if r.Label == spec.LabelObserved {
			t.Fatalf("vision item labeled Observed: %+v", r)
		}
		if !strings.HasPrefix(r.Basis, "vision:") {
			t.Fatalf("vision basis want vision:<stage>, got %q", r.Basis)
		}
	}
}

func TestRouter_BornDigitalPDF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "landing.pdf")
	content := bornDigitalPDFFixture()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	router := NewRouter(RouterConfig{CassetteDir: t.TempDir()})
	out, err := router.Extract(context.Background(), Artifact{Name: "landing.pdf", Path: path}, content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(out.SeedRequirements) == 0 {
		t.Fatal("expected PDF extraction")
	}
	if !strings.HasPrefix(out.SeedRequirements[0].Basis, "pdf:page") {
		t.Fatalf("expected pdf:page basis, got %q", out.SeedRequirements[0].Basis)
	}
}

func TestRouter_FigmaFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "figma", "checkout_flow.json"))
	if err != nil {
		t.Fatalf("read figma: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "checkout.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	router := NewRouter(RouterConfig{CassetteDir: t.TempDir()})
	out, err := router.Extract(context.Background(), Artifact{Name: "checkout.json", Path: path}, raw)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(out.SeedRequirements) == 0 {
		t.Fatal("expected figma extraction")
	}
}

func TestHTMLGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "mockup", "landing.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	items, err := ExtractHTML(raw)
	if err != nil {
		t.Fatalf("ExtractHTML: %v", err)
	}
	got := BuildExtraction("mockup", items)
	goldenPath := filepath.Join("testdata", "landing_html_extraction.json")
	if os.Getenv("UPDATE_MOCKUP_GOLDENS") == "1" {
		writeGolden(t, goldenPath, got)
	}
	want := loadGoldenExtraction(t, goldenPath)
	assertExtractionEqual(t, want, got)
}

func writeGolden(t *testing.T, path string, ex Extraction) {
	t.Helper()
	raw, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}

func loadGoldenExtraction(t *testing.T, path string) Extraction {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with UPDATE_MOCKUP_GOLDENS=1)", path, err)
	}
	var ex Extraction
	if err := json.Unmarshal(raw, &ex); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	return ex
}

func assertExtractionEqual(t *testing.T, want, got Extraction) {
	t.Helper()
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	if string(w) != string(g) {
		t.Fatalf("extraction mismatch:\nwant %s\ngot  %s", w, g)
	}
}
