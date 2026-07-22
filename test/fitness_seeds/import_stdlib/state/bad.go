// Package state is a seeded fitness violation (docs/PLAN.md Task 18 /
// SKP-16, rule (c)): it imports a non-stdlib package, standing in for
// internal/state so scripts/check_stdlib_only.sh has a fixture that must
// fail without needing to break the real internal/state package.
package state

import "golang.org/x/text/language"

// Tag references the non-stdlib import so it is not compiled away.
var Tag = language.English
