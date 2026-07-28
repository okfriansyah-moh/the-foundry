package spec

import (
	"sort"
	"strings"
)

func RenderMarkdown(title string, s Specification) string {
	var b strings.Builder
	b.WriteString("# ")
	if strings.TrimSpace(title) == "" {
		b.WriteString("Specification")
	} else {
		b.WriteString(title)
	}
	b.WriteString("\n\n")

	sections := orderedSections(s.Sections)
	for _, section := range sections {
		b.WriteString("## ")
		b.WriteString(section)
		b.WriteString("\n\n")
		for _, idx := range s.BySection[section] {
			r := s.Requirements[idx]
			b.WriteString("- **[")
			b.WriteString(string(r.Label))
			b.WriteString("]** ")
			b.WriteString(r.Text)
			b.WriteString(" _(id: ")
			b.WriteString(r.ID)
			b.WriteString("; basis: ")
			b.WriteString(r.Basis)
			b.WriteString(")_\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func orderedSections(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	rank := map[string]int{}
	for i, s := range completenessSections {
		rank[s] = i
	}
	out := append([]string(nil), input...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := rank[out[i]]
		rj, jok := rank[out[j]]
		if iok && jok {
			return ri < rj
		}
		if iok {
			return true
		}
		if jok {
			return false
		}
		return out[i] < out[j]
	})
	return out
}
