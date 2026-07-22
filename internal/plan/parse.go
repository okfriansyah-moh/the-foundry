package plan

import (
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

const frontMatterDelim = "---"

// Parse reads an executable PLAN.md document from r: a YAML front-matter
// block delimited by "---" lines, followed by a Markdown body sectioned by
// "## " headings.
//
// Parsing is strict: unknown front-matter fields are a parse error
// (docs/PLAN.md Task 6 Acceptance — "strict-mode rejects unknown keys with
// line numbers").
func Parse(r io.Reader) (*Document, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("plan: read source: %w", err)
	}
	return ParseBytes(raw)
}

// ParseBytes parses raw as an executable PLAN.md document. See Parse.
func ParseBytes(raw []byte) (*Document, error) {
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")

	if len(lines) == 0 || strings.TrimSpace(lines[0]) != frontMatterDelim {
		return nil, fmt.Errorf("plan: line 1: expected %q front-matter delimiter", frontMatterDelim)
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == frontMatterDelim {
			end = i
			break
		}
	}
	if end == -1 {
		return nil, fmt.Errorf("plan: unterminated front matter: no closing %q", frontMatterDelim)
	}

	frontMatter := strings.Join(lines[1:end], "\n")
	body := strings.Join(lines[end+1:], "\n")

	var doc Document
	if strings.TrimSpace(frontMatter) != "" {
		dec := yaml.NewDecoder(strings.NewReader(frontMatter))
		dec.KnownFields(true)
		if err := dec.Decode(&doc); err != nil {
			return nil, fmt.Errorf("plan: front matter: %w", err)
		}
	}

	doc.raw = raw
	doc.Sections = parseSections(body)

	if err := doc.validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// parseSections splits a Markdown body into "## Heading" blocks, preserving
// document order. Text before the first "## " heading is discarded — the
// schema fields in front matter are authoritative for anything machine-read.
func parseSections(body string) []Section {
	var sections []Section
	var cur *Section
	var buf []string

	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimSpace(strings.Join(buf, "\n"))
			sections = append(sections, *cur)
		}
	}

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			cur = &Section{Heading: heading}
			buf = nil
			continue
		}
		if cur != nil {
			buf = append(buf, line)
		}
	}
	flush()

	return sections
}

// Serialize renders the document back to canonical PLAN.md source: YAML
// front matter (via the same tags Parse decodes) followed by the sectioned
// Markdown body. Serialize is deterministic — the same Document always
// produces byte-identical output — which is what makes the
// parse-reserialize-reparse digest round trip stable.
func (d *Document) Serialize() ([]byte, error) {
	fm, err := yaml.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("plan: serialize front matter: %w", err)
	}

	var b strings.Builder
	b.WriteString(frontMatterDelim)
	b.WriteString("\n")
	b.Write(fm)
	b.WriteString(frontMatterDelim)
	b.WriteString("\n")
	for _, s := range d.Sections {
		b.WriteString("## ")
		b.WriteString(s.Heading)
		b.WriteString("\n")
		if s.Body != "" {
			b.WriteString(s.Body)
			b.WriteString("\n")
		}
	}

	return []byte(b.String()), nil
}
