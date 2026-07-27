// Package bad is a seeded fitness violation (docs/PLAN.md Task 28 / FND-09,
// rule (b)): it lives under a path whose trailing segments are
// ".../internal/pec/bad" — the same shape the real internal/pec package
// will have once Task 56 builds it — and imports internal/kernel, which
// `fitlint authority` must flag even though the real internal/pec package
// does not exist yet (Constitution C5 — PEC only proposes, it never gains
// decision/side-effect authority).
package bad

import _ "github.com/okfriansyah-moh/the-foundry/internal/kernel"
