package mockup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/spec"
)

// FigmaNode is the subset of a Figma REST file node this ingestion reads.
type FigmaNode struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Children []FigmaNode `json:"children,omitempty"`
	// TransitionNodeID is a prototype edge: this node navigates to another on
	// interaction. Its presence is a structurally-present flow fact.
	TransitionNodeID string `json:"transitionNodeID,omitempty"`
	// A11yLabel is an accessibility label carried in Figma metadata where
	// present (e.g. an aria/alt annotation). Structurally present ⇒ Observed.
	A11yLabel string `json:"a11yLabel,omitempty"`
}

// FigmaComponentMeta is one entry of a file's components map.
type FigmaComponentMeta struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// FigmaFlowStart is one prototype flow starting point.
type FigmaFlowStart struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
}

// FigmaFile is the subset of the Figma REST GET /v1/files/:key response this
// ingestion consumes: the node tree, the component set, and prototype flows.
type FigmaFile struct {
	Name               string                        `json:"name"`
	Document           FigmaNode                     `json:"document"`
	Components         map[string]FigmaComponentMeta `json:"components"`
	FlowStartingPoints []FigmaFlowStart              `json:"flowStartingPoints,omitempty"`
}

// FigmaClient fetches a Figma file. Read-only by contract — this package
// never mutates a Figma file.
type FigmaClient interface {
	GetFile(ctx context.Context, fileKey string) (FigmaFile, error)
}

// ReplayFigmaClient serves a recorded Figma file (a cassette), so tests and
// replay runs never hit the network.
type ReplayFigmaClient struct {
	File FigmaFile
}

// GetFile implements FigmaClient.
func (c ReplayFigmaClient) GetFile(context.Context, string) (FigmaFile, error) {
	return c.File, nil
}

// LoadFigmaCassette loads a recorded Figma file response from path.
func LoadFigmaCassette(path string) (ReplayFigmaClient, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReplayFigmaClient{}, fmt.Errorf("mockup figma: read cassette %s: %w", path, err)
	}
	var f FigmaFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return ReplayFigmaClient{}, fmt.Errorf("mockup figma: decode cassette %s: %w", path, err)
	}
	return ReplayFigmaClient{File: f}, nil
}

// RESTFigmaClient calls the real Figma REST API with a READ-ONLY personal
// access token. The token is supplied by the caller from the secrets store
// (Task 35), never hardcoded; this client only ever issues GETs.
type RESTFigmaClient struct {
	Token      string
	BaseURL    string // default https://api.figma.com
	HTTPClient *http.Client
}

// GetFile implements FigmaClient via GET /v1/files/:key.
func (c RESTFigmaClient) GetFile(ctx context.Context, fileKey string) (FigmaFile, error) {
	base := c.BaseURL
	if base == "" {
		base = "https://api.figma.com"
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v1/files/"+fileKey, nil)
	if err != nil {
		return FigmaFile{}, fmt.Errorf("mockup figma: build request: %w", err)
	}
	req.Header.Set("X-Figma-Token", c.Token)
	resp, err := client.Do(req)
	if err != nil {
		return FigmaFile{}, fmt.Errorf("mockup figma: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return FigmaFile{}, fmt.Errorf("mockup figma: API status %d", resp.StatusCode)
	}
	var f FigmaFile
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return FigmaFile{}, fmt.Errorf("mockup figma: decode response: %w", err)
	}
	return f, nil
}

// nodeBasis is the Basis stamped on a Figma-sourced Observed item: a stable
// reference back to the exact Figma node/component/edge it came from.
func nodeBasis(kind, ref string) string { return "figma:" + kind + ":" + ref }

// ExtractFigma maps a Figma file into the SAME extraction shape as the
// vision pipeline (RunPipeline), with one difference the card mandates:
// structurally-present facts (a component exists, a prototype flow edge
// exists, an a11y label is present) are labeled Observed and carry the Figma
// node ref as Basis — because Figma provides structural ground truth, not a
// vision model's inference. Inference stages are never emitted here.
func ExtractFigma(file FigmaFile) Extraction {
	var items []ExtractedItem

	// Component set: each declared component is a structurally-present fact.
	for id, comp := range file.Components {
		ref := comp.Key
		if ref == "" {
			ref = id
		}
		items = append(items, ExtractedItem{
			Stage:      StageScreenComponents,
			Text:       "component: " + comp.Name,
			Section:    "components",
			Label:      spec.LabelObserved,
			Confidence: 1.0,
			NodeRef:    nodeBasis("component", ref),
		})
	}

	// Node tree: components/instances/frames present, prototype edges, a11y.
	walkFigmaNodes(file.Document, &items)

	// Prototype flow starting points.
	for _, fs := range file.FlowStartingPoints {
		items = append(items, ExtractedItem{
			Stage:      StageUserFlow,
			Text:       "flow start: " + fs.Name,
			Section:    "user-flow",
			Label:      spec.LabelObserved,
			Confidence: 1.0,
			NodeRef:    nodeBasis("node", fs.NodeID),
		})
	}

	return buildFigmaExtraction(items)
}

func walkFigmaNodes(n FigmaNode, out *[]ExtractedItem) {
	switch strings.ToUpper(n.Type) {
	case "COMPONENT", "INSTANCE", "FRAME":
		*out = append(*out, ExtractedItem{
			Stage:      StageScreenComponents,
			Text:       n.Type + ": " + n.Name,
			Section:    "screen-components",
			Label:      spec.LabelObserved,
			Confidence: 1.0,
			NodeRef:    nodeBasis("node", n.ID),
		})
	}
	if n.TransitionNodeID != "" {
		*out = append(*out, ExtractedItem{
			Stage:      StageUserFlow,
			Text:       "flow edge: " + n.Name + " → " + n.TransitionNodeID,
			Section:    "user-flow",
			Label:      spec.LabelObserved,
			Confidence: 1.0,
			NodeRef:    nodeBasis("edge", n.ID+"->"+n.TransitionNodeID),
		})
	}
	if n.A11yLabel != "" {
		*out = append(*out, ExtractedItem{
			Stage:      StageA11y,
			Text:       "a11y: " + n.A11yLabel,
			Section:    "a11y",
			Label:      spec.LabelObserved,
			Confidence: 1.0,
			NodeRef:    nodeBasis("node", n.ID),
		})
	}
	for _, c := range n.Children {
		walkFigmaNodes(c, out)
	}
}

// buildFigmaExtraction runs the same normalization + requirement-seeding as
// RunPipeline, but stamps Basis from each item's NodeRef so Figma-sourced
// Observed items carry their node ref as Basis (Task 80 acceptance).
func buildFigmaExtraction(items []ExtractedItem) Extraction {
	reqs := make([]spec.Requirement, 0, len(items))
	high := make([]spec.Requirement, 0)
	normalized := make([]ExtractedItem, 0, len(items))
	for i, item := range items {
		item.Label = NormalizeLabel(item.Stage, item.Confidence, item.Label)
		normalized = append(normalized, item)
		basis := item.NodeRef
		if basis == "" {
			basis = string(item.Stage)
		}
		req := spec.Requirement{
			ID:      fmt.Sprintf("figma-%d", i+1),
			Section: item.Section,
			Text:    item.Text,
			Label:   item.Label,
			Basis:   basis,
			Impact:  spec.ImpactMedium,
		}
		if HighImpactUnresolved(item.Text, item.Label) {
			req.Impact = spec.ImpactHigh
			high = append(high, req)
		}
		reqs = append(reqs, req)
	}
	return Extraction{Items: normalized, HighImpactUnresolved: high, SeedRequirements: reqs}
}
