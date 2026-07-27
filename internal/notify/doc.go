// Package notify implements the Telegram engine (docs/PLAN.md Task 30 /
// FND-11, Constitution C11): event classification, per-chat/global flood
// control, P2/P3 digest batching, outbound delivery via the
// `notifications` table (internal/db/migrations/00007_notifications.sql,
// Task 20), and a command router for the three low-risk commands this
// engine supports (/status, /pause, /resume).
//
// Authority boundary (Constitution C4/C11): this package never decides or
// performs a side effect on its own authority. Pause/resume/status are
// dispatched through an injected WorkflowController interface owned by
// whatever kernel-side code wires this engine up — this package only
// validates the command's nonce and chat-principal binding and forwards
// it. `/approve` is never executed here at all: handleApprove always
// terminates at internal/authn.TelegramApprove's reply text (Task 25's
// existing C11 guard) and this package has no code path that calls
// provenance.Store.AddApprover or any other approval side effect,
// regardless of TelegramApprove's Allowed value — so no high-risk (or
// low-risk) approval can ever complete through this engine.
package notify
