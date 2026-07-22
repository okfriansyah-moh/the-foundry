package compiler

import (
	"fmt"
	"strings"
)

// Explain renders r's overrides as human-readable lines, one per
// override, in the form `foundry policy resolve` prints alongside the
// effective policy. Acceptance requires explanations to list every
// override — this is a straight, ordered walk of r.Overrides, no
// filtering.
func Explain(r *Resolved) string {
	if len(r.Overrides) == 0 {
		return "no overrides: effective policy equals the platform default"
	}
	var b strings.Builder
	for _, ov := range r.Overrides {
		fmt.Fprintf(&b, "layer %s field %q: %s -> %s (%s)\n", ov.FromLayer, ov.Field, ov.Old, ov.New, ov.Direction)
	}
	return b.String()
}
