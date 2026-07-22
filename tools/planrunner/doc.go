// Command planrunner is a standalone bootstrap tool (Task 3 / RUN-01) that drives
// docs/PLAN.md end to end: it parses the Master Index, picks the next eligible task,
// invokes the same implementation protocol a human would run manually, validates the
// result, and gates high-risk tasks behind a Telegram approval.
//
// Authority limits (Constitution C4/C5, docs/PLAN.md Task 3 Rationale):
//   - This tool never modifies internal/* directly. It only invokes the implementation
//     protocol headlessly (e.g. `claude -p`), the same way a human would.
//   - It never invents its own risk classifier — Classify reads each card's own Risk/Rev
//     fields verbatim (Constitution C6).
//   - It is a temporary bootstrap tool, not a second kernel or a second PEC: it gains no
//     authority beyond what each downstream task card already grants via Risk/Rev, and it
//     is retired once Foundry's own kernel, classifier, and Telegram engine exist (Task 3
//     Step 8 exit condition).
package main
