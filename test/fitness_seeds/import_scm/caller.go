// Package caller is a seeded fitness violation (docs/PLAN.md Task 18 /
// SKP-16, rule (c)): it imports internal/scm from outside internal/kernel,
// which scripts/check_scm_boundary.sh must flag (Constitution C4 — only the
// kernel performs side effects backed by internal/scm/write).
package caller

import _ "github.com/okfriansyah-moh/the-foundry/internal/scm"
