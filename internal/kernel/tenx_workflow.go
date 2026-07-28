package kernel

import (
	"fmt"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// TenXDeliverResult is the terminal result of the 10x branch handoff workflow.
// Constitution C15: the only success shape is SUCCEEDED/TEN_X_BRANCH_HANDOFF_READY.
// No PR, merge, staging, or deployment is ever performed (enforced by
// cmd/fitlint's call-graph check, wired in Task 61).
type TenXDeliverResult struct {
	Status     state.Status     `json:"status"`
	ResultCode state.ResultCode `json:"result_code"`

	// Branches lists each branch that received a successful push.
	Branches []string `json:"branches"`
	// Receipts are the push receipts from the Branch Integrator.
	Receipts []integrator.Receipt `json:"receipts"`
	// ManifestDigests are the ChangeSet digests of each pushed atomic group.
	ManifestDigests []string `json:"manifest_digests"`
	// EvidenceLinks are references to validation evidence records.
	EvidenceLinks []string `json:"evidence_links"`
	// HandoffNote is the human-readable summary statement included in the
	// org-channel notification (Constitution C15: must state no PR/merge/deploy).
	HandoffNote string `json:"handoff_note"`
}

// TenXHandoffNotification is the org-channel Telegram notification template
// for a successful 10x branch handoff.
type TenXHandoffNotification struct {
	Branches        []string
	Receipts        []integrator.Receipt
	ManifestDigests []string
	EvidenceLinks   []string
	IssuedAt        time.Time
}

// FormatTenXHandoffNotification formats the org-channel notification message.
// It always includes the C15 statement: "no PR/merge/deploy was performed".
func FormatTenXHandoffNotification(n TenXHandoffNotification) string {
	var sb strings.Builder
	sb.WriteString("✅ *10x Branch Handoff Ready*\n\n")
	sb.WriteString(fmt.Sprintf("*Branches:* %s\n", strings.Join(n.Branches, ", ")))

	if len(n.Receipts) > 0 {
		sb.WriteString("\n*Push Receipts:*\n")
		for _, r := range n.Receipts {
			sb.WriteString(fmt.Sprintf("  • `%s`: `%s` → `%s`\n", r.Branch, shortSHA(r.BeforeSHA), shortSHA(r.AfterSHA)))
		}
	}

	if len(n.ManifestDigests) > 0 {
		sb.WriteString("\n*Manifest Digests:*\n")
		for _, d := range n.ManifestDigests {
			sb.WriteString(fmt.Sprintf("  • `%s`\n", d[:min(12, len(d))]))
		}
	}

	if len(n.EvidenceLinks) > 0 {
		sb.WriteString("\n*Evidence:*\n")
		for _, l := range n.EvidenceLinks {
			sb.WriteString(fmt.Sprintf("  • %s\n", l))
		}
	}

	// C15 mandatory statement.
	sb.WriteString("\n_No PR, merge, staging, or deployment was performed._")
	sb.WriteString(fmt.Sprintf("\n_Issued: %s_", n.IssuedAt.UTC().Format("2006-01-02T15:04:05Z")))
	return sb.String()
}

// TenXHandoffTerminal produces the terminal TenXDeliverResult for a successful
// 10x branch handoff. The Status/ResultCode pair is always exactly
// SUCCEEDED/TEN_X_BRANCH_HANDOFF_READY — no other terminal shape exists for
// this workflow (Constitution C15).
func TenXHandoffTerminal(receipts []integrator.Receipt, evidenceLinks []string) TenXDeliverResult {
	branches := make([]string, 0, len(receipts))
	digests := make([]string, 0, len(receipts))
	seen := map[string]struct{}{}
	for _, r := range receipts {
		if _, ok := seen[r.Branch]; !ok {
			branches = append(branches, r.Branch)
			seen[r.Branch] = struct{}{}
		}
		digests = append(digests, r.ManifestDigest)
	}

	note := fmt.Sprintf(
		"10x handoff complete: %d group(s) pushed to %s — no PR, merge, staging, or deployment was performed",
		len(receipts), strings.Join(branches, ", "),
	)

	return TenXDeliverResult{
		Status:          state.StatusSucceeded,
		ResultCode:      state.ResultTenXBranchHandoffReady,
		Branches:        branches,
		Receipts:        receipts,
		ManifestDigests: digests,
		EvidenceLinks:   evidenceLinks,
		HandoffNote:     note,
	}
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
