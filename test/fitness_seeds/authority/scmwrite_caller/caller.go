// Package caller is a seeded fitness violation (docs/PLAN.md Task 28 /
// FND-09, rule (a)): it imports internal/scm/write directly from outside
// internal/kernel, which `fitlint authority` must flag (Constitution C4 —
// only the kernel performs side effects, including SCM writes).
package caller

import _ "github.com/okfriansyah-moh/the-foundry/internal/scm/write"
