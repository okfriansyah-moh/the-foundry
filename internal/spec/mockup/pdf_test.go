package mockup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDetect_Formats(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "mockup", "landing.html"))
	if err != nil {
		t.Fatalf("read landing.html: %v", err)
	}
	got, err := Detect(html, ".html")
	if err != nil || got != FormatHTML {
		t.Fatalf("Detect(html) = %q, %v; want html", got, err)
	}

	pdf := bornDigitalPDFFixture()
	got, err = Detect(pdf, ".pdf")
	if err != nil || got != FormatPDF {
		t.Fatalf("Detect(pdf) = %q, %v; want pdf", got, err)
	}

	figma, err := os.ReadFile(filepath.Join("testdata", "figma", "checkout_flow.json"))
	if err != nil {
		t.Fatalf("read figma fixture: %v", err)
	}
	got, err = Detect(figma, ".json")
	if err != nil || got != FormatFigma {
		t.Fatalf("Detect(figma) = %q, %v; want figma", got, err)
	}

	png := minimalPNG()
	got, err = Detect(png, ".png")
	if err != nil || got != FormatPNG {
		t.Fatalf("Detect(png) = %q, %v; want png", got, err)
	}

	if _, err := Detect([]byte("not a mockup"), ".txt"); err == nil {
		t.Fatal("expected unrecognized format error")
	}
}

func bornDigitalPDFFixture() []byte {
	return []byte(`%PDF-1.4
1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj
2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj
3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>endobj
4 0 obj<< /Length 55 >>stream
BT /F1 12 Tf 72 720 Td (Landing page headline) Tj ET
endstream
endobj
5 0 obj<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>endobj
xref
0 6
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000260 00000 n 
0000000366 00000 n 
trailer<< /Size 6 /Root 1 0 R >>
startxref
435
%%EOF`)
}

func scannedPDFFixture() []byte {
	return []byte(`%PDF-1.4
1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj
2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj
3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>endobj
4 0 obj<< /Length 8 >>stream

endstream
endobj
xref
0 5
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000210 00000 n 
trailer<< /Size 5 /Root 1 0 R >>
startxref
280
%%EOF`)
}

func minimalPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x18, 0xDD,
		0x8D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
		0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	}
}

func writeFixturePDF(t *testing.T, name string, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
	return path
}

func TestPDFHasTextLayer(t *testing.T) {
	if !PDFHasTextLayer(bornDigitalPDFFixture()) {
		t.Fatal("born-digital PDF should have text layer")
	}
	if PDFHasTextLayer(scannedPDFFixture()) {
		t.Fatal("scanned PDF should not have text layer")
	}
}

func TestExtractPDFText_Deterministic(t *testing.T) {
	a, err := ExtractPDFText(bornDigitalPDFFixture())
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	if len(a) == 0 {
		t.Fatal("expected text items")
	}
	b, err := ExtractPDFText(bornDigitalPDFFixture())
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	if !bytes.Equal([]byte(a[0].Text), []byte(b[0].Text)) {
		t.Fatalf("non-deterministic text: %q vs %q", a[0].Text, b[0].Text)
	}
}
