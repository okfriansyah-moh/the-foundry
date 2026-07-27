package authn

import "fmt"

// TelegramApproveResult is the outcome of routing a Telegram `approve`
// command through the high-risk step-up gate.
type TelegramApproveResult struct {
	Allowed bool
	Reply   string
}

// SecureSurfaceURLFunc builds the URL a rejected Telegram approval points
// the operator to, given the plan id it was attempting to approve.
type SecureSurfaceURLFunc func(planID string) string

// TelegramApprove implements the C11 rule that Telegram is never a valid
// high-risk approval surface
// (docs/foundry/docs/security/approval-and-provenance.md §3: "Telegram
// alone is never valid approval for high-risk actions"). When
// planCtx.RequiresStrongAuth(), it refuses the command outright and
// points the operator at the secure (WebAuthn-capable) surface instead of
// accepting, queuing, or otherwise partially processing the approval.
//
// internal/notify's full Telegram command-router engine (dedupe, rate
// limiting, nonces) is docs/PLAN.md Task 30, not yet built. This function
// is the smallest standalone piece Task 25 needs to prove the rejection
// behavior on its own — it is deliberately not a reimplementation of that
// engine and does not attempt to record a low-risk approval either; a
// low-risk `allowed: true` result still requires the caller to route the
// actual approval through Store.AddApprover itself.
func TelegramApprove(planID string, planCtx PlanContext, secureSurfaceURL SecureSurfaceURLFunc) TelegramApproveResult {
	if planCtx.RequiresStrongAuth() {
		return TelegramApproveResult{
			Allowed: false,
			Reply:   fmt.Sprintf("high-risk approval requires the secure surface: %s", secureSurfaceURL(planID)),
		}
	}
	return TelegramApproveResult{Allowed: true, Reply: "approval accepted"}
}
