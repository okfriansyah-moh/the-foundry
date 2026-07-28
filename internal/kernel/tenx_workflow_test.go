package kernel_test

import (
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

func makeReceipt(branch, before, after, groupID string) integrator.Receipt {
	return integrator.Receipt{
		Branch:         branch,
		BeforeSHA:      before,
		AfterSHA:       after,
		GroupID:        groupID,
		ManifestDigest: "deadbeef0000" + groupID,
		IssuedAt:       time.Now(),
	}
}

// TestTenXHandoffTerminal_ExactStatusResultCode verifies the only allowed terminal pair.
func TestTenXHandoffTerminal_ExactStatusResultCode(t *testing.T) {
	receipts := []integrator.Receipt{makeReceipt("10x/feature", "sha-a", "sha-b", "g1")}
	result := kernel.TenXHandoffTerminal(receipts, nil)
	if result.Status != state.StatusSucceeded {
		t.Errorf("Status=%q, want SUCCEEDED", result.Status)
	}
	if result.ResultCode != state.ResultTenXBranchHandoffReady {
		t.Errorf("ResultCode=%q, want TEN_X_BRANCH_HANDOFF_READY", result.ResultCode)
	}
}

// TestTenXHandoffTerminal_ReceiptsPresent verifies receipts are included.
func TestTenXHandoffTerminal_ReceiptsPresent(t *testing.T) {
	receipts := []integrator.Receipt{
		makeReceipt("10x/feature", "sha-a", "sha-b", "g1"),
		makeReceipt("10x/other", "sha-c", "sha-d", "g2"),
	}
	result := kernel.TenXHandoffTerminal(receipts, nil)
	if len(result.Receipts) != 2 {
		t.Errorf("len(Receipts)=%d, want 2", len(result.Receipts))
	}
	if len(result.Branches) != 2 {
		t.Errorf("len(Branches)=%d, want 2 unique branches", len(result.Branches))
	}
}

// TestTenXHandoffTerminal_HandoffNoteContainsNoDeployStatement verifies C15 statement.
func TestTenXHandoffTerminal_HandoffNoteContainsNoDeployStatement(t *testing.T) {
	receipts := []integrator.Receipt{makeReceipt("10x/feature", "sha-a", "sha-b", "g1")}
	result := kernel.TenXHandoffTerminal(receipts, nil)
	if !strings.Contains(result.HandoffNote, "no PR") {
		t.Errorf("HandoffNote missing C15 statement; got: %q", result.HandoffNote)
	}
}

// TestFormatTenXHandoffNotification_ContainsC15Statement verifies notification
// always includes the C15 "no PR/merge/deploy" statement.
func TestFormatTenXHandoffNotification_ContainsC15Statement(t *testing.T) {
	n := kernel.TenXHandoffNotification{
		Branches: []string{"10x/feature"},
		Receipts: []integrator.Receipt{makeReceipt("10x/feature", "sha-a", "sha-b", "g1")},
		IssuedAt: time.Now(),
	}
	msg := kernel.FormatTenXHandoffNotification(n)
	if !strings.Contains(msg, "No PR, merge, staging, or deployment was performed") {
		t.Errorf("notification missing C15 statement; got:\n%s", msg)
	}
}

// TestTenXHandoffTerminal_AliasNotEmitted verifies the deprecated alias
// is never the emitted ResultCode.
func TestTenXHandoffTerminal_AliasNotEmitted(t *testing.T) {
	receipts := []integrator.Receipt{makeReceipt("10x/feature", "sha-a", "sha-b", "g1")}
	result := kernel.TenXHandoffTerminal(receipts, nil)
	// state.DeprecatedAliasTenXBranchesReady holds the deprecated value;
	// NormalizeResultCode maps it to the canonical form. The terminal must
	// always emit the canonical form, never the deprecated alias.
	if string(result.ResultCode) == state.DeprecatedAliasTenXBranchesReady {
		t.Errorf("ResultCode emitted deprecated alias — must use canonical %s",
			state.ResultTenXBranchHandoffReady)
	}
}
