package notify

import "fmt"

// docs/PLAN.md Task 148: TenX handoff prohibition notification helper.

// FormatTenXProhibitions returns the C15 prohibition statement for Telegram.
func FormatTenXProhibitions(branches []string) string {
	return fmt.Sprintf(
		"10x handoff ready on %v — no PR, merge, staging, or deployment was performed.",
		branches,
	)
}
