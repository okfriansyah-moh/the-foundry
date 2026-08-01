package mockup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

func htmlBasis(cssPath string) string { return "html:" + cssPath }

// ExtractHTML parses HTML deterministically into extracted items.
func ExtractHTML(content []byte) ([]ExtractedItem, error) {
	doc, err := html.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("mockup html: parse: %w", err)
	}
	var items []ExtractedItem
	walkHTML(doc, nil, &items)
	return items, nil
}

func walkHTML(n *html.Node, ancestors []string, out *[]ExtractedItem) {
	if n.Type == html.ElementNode {
		path := cssPath(append(ancestors, tagName(n)))
		switch n.DataAtom {
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			text := strings.TrimSpace(collectText(n))
			if text != "" {
				*out = append(*out, ExtractedItem{
					Stage:      StageScreenComponents,
					Text:       "screen: " + text,
					Section:    "responsive",
					Label:      spec.LabelObserved,
					Confidence: 1.0,
					NodeRef:    htmlBasis(path),
				})
			}
		case atom.Form:
			action := attrVal(n, "action")
			method := attrVal(n, "method")
			text := "form"
			if action != "" {
				text += " action=" + action
			}
			if method != "" {
				text += " method=" + method
			}
			*out = append(*out, ExtractedItem{
				Stage:      StageScreenComponents,
				Text:       text,
				Section:    "screen-components",
				Label:      spec.LabelObserved,
				Confidence: 1.0,
				NodeRef:    htmlBasis(path),
			})
			if action != "" {
				*out = append(*out, ExtractedItem{
					Stage:      StageUserFlow,
					Text:       "flow edge: form → " + action,
					Section:    "user-flow",
					Label:      spec.LabelObserved,
					Confidence: 1.0,
					NodeRef:    htmlBasis(path + ":action"),
				})
			}
			*out = append(*out, ExtractedItem{
				Stage:      StageBackendInference,
				Text:       "backend endpoint may handle " + action,
				Section:    "apis",
				Label:      spec.LabelInferred,
				Confidence: 0.75,
			})
		case atom.Input:
			name := attrVal(n, "name")
			typ := attrVal(n, "type")
			if typ == "" {
				typ = "text"
			}
			*out = append(*out, ExtractedItem{
				Stage:      StageScreenComponents,
				Text:       fmt.Sprintf("input %s type=%s", name, typ),
				Section:    "screen-components",
				Label:      spec.LabelObserved,
				Confidence: 1.0,
				NodeRef:    htmlBasis(path),
			})
		case atom.Button:
			text := strings.TrimSpace(collectText(n))
			*out = append(*out, ExtractedItem{
				Stage:      StageScreenComponents,
				Text:       "button: " + text,
				Section:    "screen-components",
				Label:      spec.LabelObserved,
				Confidence: 1.0,
				NodeRef:    htmlBasis(path),
			})
		case atom.A:
			href := attrVal(n, "href")
			if href != "" {
				label := strings.TrimSpace(collectText(n))
				if label == "" {
					label = href
				}
				*out = append(*out, ExtractedItem{
					Stage:      StageUserFlow,
					Text:       "flow edge: link → " + href,
					Section:    "user-flow",
					Label:      spec.LabelObserved,
					Confidence: 1.0,
					NodeRef:    htmlBasis(path),
				})
				_ = label
			}
		case atom.Img:
			if alt := attrVal(n, "alt"); alt != "" {
				*out = append(*out, ExtractedItem{
					Stage:      StageA11y,
					Text:       "a11y: " + alt,
					Section:    "a11y",
					Label:      spec.LabelObserved,
					Confidence: 1.0,
					NodeRef:    htmlBasis(path),
				})
			}
		case atom.Label:
			text := strings.TrimSpace(collectText(n))
			if text != "" {
				*out = append(*out, ExtractedItem{
					Stage:      StageA11y,
					Text:       "a11y: " + text,
					Section:    "a11y",
					Label:      spec.LabelObserved,
					Confidence: 1.0,
					NodeRef:    htmlBasis(path),
				})
			}
		}
		for _, a := range n.Attr {
			if strings.HasPrefix(a.Key, "aria-") && strings.TrimSpace(a.Val) != "" {
				*out = append(*out, ExtractedItem{
					Stage:      StageA11y,
					Text:       fmt.Sprintf("a11y: %s=%s", a.Key, a.Val),
					Section:    "a11y",
					Label:      spec.LabelObserved,
					Confidence: 1.0,
					NodeRef:    htmlBasis(path + ":" + a.Key),
				})
			}
		}
		childAncestors := append(ancestors, tagName(n))
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkHTML(c, childAncestors, out)
		}
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkHTML(c, ancestors, out)
	}
}

func tagName(n *html.Node) string {
	if n.DataAtom != 0 {
		return n.DataAtom.String()
	}
	return n.Data
}

func cssPath(segments []string) string {
	return strings.Join(segments, ">")
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func collectText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.TextNode {
			b.WriteString(cur.Data)
		}
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func extractHTML(ctx context.Context, artifact Artifact) (Extraction, error) {
	_ = ctx
	raw, err := os.ReadFile(artifact.Path)
	if err != nil {
		return Extraction{}, fmt.Errorf("mockup html: read %s: %w", artifact.Path, err)
	}
	items, err := ExtractHTML(raw)
	if err != nil {
		return Extraction{}, err
	}
	return BuildExtraction("mockup", items), nil
}
