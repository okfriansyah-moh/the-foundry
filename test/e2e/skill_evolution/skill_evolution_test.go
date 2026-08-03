package skillevolution_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
	"github.com/okfriansyah-moh/the-foundry/internal/packaging"
)

const evolvedSkillID = "code-reviewer-correctness"

func TestSkillEvolutionPackageE2E(t *testing.T) {
	repository := repositoryRoot(t)
	authorityPaths := []string{"config", "internal/policy", "internal/scm", "deploy"}
	authorityBefore := digestPaths(t, repository, authorityPaths...)
	writeEvidence(t, "authority-before.sha256", []byte(authorityBefore))
	t.Cleanup(func() {
		evolve.Unfreeze()
		authorityAfter := digestPaths(t, repository, authorityPaths...)
		writeEvidence(t, "authority-after.sha256", []byte(authorityAfter))
		if authorityAfter != authorityBefore {
			t.Errorf("skill evolution changed an authority-bearing repository tree")
		}
	})

	personalRoot := copyCapabilityRoot(t, repository)
	base := initialSkillVersion(t, personalRoot)
	bridge := packageBridge(base, personalRoot)

	baseInstalled := installAndDoctorSkill(t, personalRoot, filepath.Join(personalRoot, "workspaces", "base"))
	if !bytes.Equal(baseInstalled, []byte(base.Prompt)) {
		t.Fatal("base CAP-02 install does not match the canonical base package")
	}

	proposed := base
	proposed.Prompt = strings.TrimSuffix(base.Prompt, "\n") + "\n\nRequire findings to cite the smallest relevant line range.\n"
	outcome, err := bridge.Promote(
		context.Background(),
		evolve.L1Candidate{Base: base, Proposed: proposed, Profile: "personal"},
		evolve.L1Stages{ShadowPass: true, CanaryPass: true},
	)
	if err != nil {
		t.Fatalf("promote personal skill: %v", err)
	}
	if !outcome.Applied || outcome.Record.Stage != evolve.StagePromoted {
		t.Fatalf("personal candidate was not promoted: %+v", outcome)
	}

	version1 := filepath.Join(personalRoot, "skills", evolvedSkillID, "versions", "v1", "SKILL.md")
	version2 := filepath.Join(personalRoot, "skills", evolvedSkillID, "versions", "v2", "SKILL.md")
	assertFileBytes(t, version1, []byte(base.Prompt))
	assertFileBytes(t, version2, []byte(proposed.Prompt))
	assertPersonalCatalogSource(t, personalRoot, filepath.ToSlash(filepath.Join("skills", evolvedSkillID, "versions", "v2", "SKILL.md")))

	promotedInstalled := installAndDoctorSkill(t, personalRoot, filepath.Join(personalRoot, "workspaces", "promoted"))
	if !bytes.Equal(promotedInstalled, []byte(proposed.Prompt)) {
		t.Fatal("fresh CAP-02 install did not select the promoted package")
	}
	if bytes.Equal(promotedInstalled, baseInstalled) {
		t.Fatal("promotion did not change installed skill bytes")
	}
	organizationInstalled := installAndDoctorSkillForProfile(t, personalRoot, t.TempDir(), "organization-10x")
	if !bytes.Equal(organizationInstalled, baseInstalled) {
		t.Fatal("personal promotion changed the organization materialization input")
	}
	writeEvidence(t, "profile-isolation-proof.txt", []byte(fmt.Sprintf(
		"personal_promoted_sha256=%s\norganization_after_personal_promote_sha256=%s\nbaseline_sha256=%s\norganization_retained_baseline=true\n",
		digest(promotedInstalled), digest(organizationInstalled), digest(baseInstalled),
	)))

	rollbackOutput := runRollbackCommand(t, repository, personalRoot)
	writeEvidence(t, "rollback-command.txt", rollbackOutput)
	version3 := filepath.Join(personalRoot, "skills", evolvedSkillID, "versions", "v3", "SKILL.md")
	assertFileBytes(t, version3, []byte(base.Prompt))
	assertFileBytes(t, version2, []byte(proposed.Prompt))
	assertPersonalCatalogSource(t, personalRoot, filepath.ToSlash(filepath.Join("skills", evolvedSkillID, "versions", "v3", "SKILL.md")))

	rollbackInstalled := installAndDoctorSkill(t, personalRoot, filepath.Join(personalRoot, "workspaces", "rollback"))
	if !bytes.Equal(rollbackInstalled, baseInstalled) {
		t.Fatal("rollback did not restore the previous CAP-02 materialization input")
	}

	promotionRows := mustRead(t, filepath.Join(personalRoot, "skills", ".evolution", "promotions.jsonl"))
	writeEvidence(t, "promotion-rows.jsonl", promotionRows)
	writeEvidence(t, "version-diff.txt", []byte(versionDiff(baseInstalled, promotedInstalled)))
	writeEvidence(t, "rollback-proof.txt", []byte(fmt.Sprintf(
		"promote_source=skills/%s/versions/v2/SKILL.md\nrollback_source=skills/%s/versions/v3/SKILL.md\nbase_sha256=%s\npromoted_sha256=%s\nrollback_sha256=%s\nretained=v1,v2,v3\n",
		evolvedSkillID,
		evolvedSkillID,
		digest(baseInstalled),
		digest(promotedInstalled),
		digest(rollbackInstalled),
	)))

	orgRows := proveOrganizationProposalOnly(t, repository)
	permissionProof := provePermissionExpansionRejected(t, repository)
	freezeProof := proveFreezeBlocksPromotion(t, repository)
	writeEvidence(t, "org-proposal-rows.jsonl", orgRows)
	writeEvidence(t, "gate-proof.txt", []byte(permissionProof+freezeProof))
	writeEvidence(t, "e2e-summary.txt", []byte(
		"PASS personal promotion selected v2 in a fresh CAP-02 install and doctor\n"+
			"PASS organization materialization retained baseline bytes\n"+
			"PASS one-command rollback selected append-only v3 with baseline bytes\n"+
			"PASS permission expansion and drift freeze produced no package activation\n"+
			"PASS config, policy, SCM, and deploy authority trees remained unchanged\n",
	))
	writeEvidence(t, "README.md", []byte(evidenceReadme()))
}

func packageBridge(base evolve.SkillVersion, root string) *evolve.SkillPackageBridge {
	registry := evolve.NewSkillRegistry()
	registry.Register(base)
	return &evolve.SkillPackageBridge{
		Root: root,
		Pipeline: evolve.L1Pipeline{
			Registry: registry,
			Suite: evolve.GoldenSuite{Tasks: []evolve.GoldenTask{{
				Name:  "non-empty-review-procedure",
				Check: func(version evolve.SkillVersion) bool { return strings.Contains(version.Prompt, "review") },
			}}},
			Limits: evolve.DefaultChangeBudgetLimits(),
			Now:    func() time.Time { return time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) },
		},
		DurableFreeze: evolve.FreezeReaderFunc(func(context.Context, string) (bool, evolve.FreezeCondition, error) {
			return false, "", nil
		}),
	}
}

func initialSkillVersion(t *testing.T, root string) evolve.SkillVersion {
	t.Helper()
	catalogs, err := packaging.LoadCatalogs(root)
	if err != nil {
		t.Fatalf("load base catalog: %v", err)
	}
	for _, skill := range catalogs.Skills.Skills {
		if skill.Name == evolvedSkillID {
			prompt := mustRead(t, filepath.Join(root, filepath.FromSlash(skill.Source)))
			return evolve.SkillVersion{
				SkillID:     evolvedSkillID,
				Version:     1,
				Prompt:      string(prompt),
				Permissions: []string{"read"},
				DataClasses: []string{"code"},
				BudgetUSD:   1,
			}
		}
	}
	t.Fatalf("skill %q is missing from fixture catalog", evolvedSkillID)
	return evolve.SkillVersion{}
}

func proveOrganizationProposalOnly(t *testing.T, repository string) []byte {
	t.Helper()
	root := copyCapabilityRoot(t, repository)
	base := initialSkillVersion(t, root)
	workspace := filepath.Join(root, "workspaces", "org")
	installAndDoctorSkill(t, root, workspace)
	immutablePaths := []string{
		"skills/catalog.yaml",
		filepath.ToSlash(filepath.Join("skills", evolvedSkillID)),
		"templates/product/.foundry/skills/enabled.yaml",
		"workspaces/org",
	}
	before := digestPaths(t, root, immutablePaths...)
	proposed := base
	proposed.Prompt += "\nProposal-only organization refinement.\n"
	outcome, err := packageBridge(base, root).Promote(
		context.Background(),
		evolve.L1Candidate{Base: base, Proposed: proposed, Profile: "organization"},
		evolve.L1Stages{ShadowPass: true, CanaryPass: true},
	)
	if err != nil {
		t.Fatalf("organization proposal: %v", err)
	}
	if outcome.Applied || !outcome.ProposalOnly || outcome.Record.Stage != evolve.StageProposed {
		t.Fatalf("organization candidate was not proposal-only: %+v", outcome)
	}
	after := digestPaths(t, root, immutablePaths...)
	if after != before {
		t.Fatal("organization proposal changed catalog, package, enablement, or materialization")
	}
	if _, err := os.Stat(filepath.Join(root, "skills", evolvedSkillID, "versions", "v2", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("organization proposal created an active v2 package: %v", err)
	}
	postProposal := installAndDoctorSkillForProfile(t, root, t.TempDir(), "organization-10x")
	if !bytes.Equal(postProposal, []byte(base.Prompt)) {
		t.Fatal("fresh install after organization proposal did not retain v1")
	}
	return mustRead(t, filepath.Join(root, "skills", ".evolution", "promotions.jsonl"))
}

func provePermissionExpansionRejected(t *testing.T, repository string) string {
	t.Helper()
	root := copyCapabilityRoot(t, repository)
	base := initialSkillVersion(t, root)
	activationPaths := []string{
		"skills/catalog.yaml",
		filepath.ToSlash(filepath.Join("skills", evolvedSkillID)),
		"templates/product/.foundry/skills/enabled.yaml",
	}
	before := digestPaths(t, root, activationPaths...)
	proposed := base
	proposed.Prompt += "\nAttempt authority expansion.\n"
	proposed.Permissions = []string{"read", "write"}
	outcome, err := packageBridge(base, root).Promote(
		context.Background(),
		evolve.L1Candidate{Base: base, Proposed: proposed, Profile: "personal"},
		evolve.L1Stages{ShadowPass: true, CanaryPass: true},
	)
	if err != nil {
		t.Fatalf("permission rejection: %v", err)
	}
	if outcome.Applied || outcome.ConditionFailure != evolve.L1NewPermission || outcome.Record.Stage != evolve.StageRejected {
		t.Fatalf("permission-expanding candidate was not rejected: %+v", outcome)
	}
	after := digestPaths(t, root, activationPaths...)
	if after != before {
		t.Fatal("permission rejection changed durable package inputs")
	}
	return fmt.Sprintf("permission_expansion=blocked stage=%s condition=%s\n", outcome.Record.Stage, outcome.ConditionFailure)
}

func proveFreezeBlocksPromotion(t *testing.T, repository string) string {
	t.Helper()
	root := copyCapabilityRoot(t, repository)
	base := initialSkillVersion(t, root)
	activationPaths := []string{
		"skills/catalog.yaml",
		filepath.ToSlash(filepath.Join("skills", evolvedSkillID)),
		"templates/product/.foundry/skills/enabled.yaml",
	}
	before := digestPaths(t, root, activationPaths...)
	proposed := base
	proposed.Prompt += "\nCandidate during freeze.\n"
	bridge := packageBridge(base, root)
	bridge.DurableFreeze = evolve.FreezeReaderFunc(func(context.Context, string) (bool, evolve.FreezeCondition, error) {
		return true, evolve.FreezeQualityRegression, nil
	})
	outcome, err := bridge.Promote(
		context.Background(),
		evolve.L1Candidate{Base: base, Proposed: proposed, Profile: "personal"},
		evolve.L1Stages{ShadowPass: true, CanaryPass: true},
	)
	if err != nil {
		t.Fatalf("freeze rejection: %v", err)
	}
	if outcome.Applied || outcome.Record.Stage != evolve.StageQuarantined {
		t.Fatalf("freeze did not block promotion: %+v", outcome)
	}
	after := digestPaths(t, root, activationPaths...)
	if after != before {
		t.Fatal("frozen promotion changed durable package inputs")
	}
	return fmt.Sprintf("drift_freeze=blocked stage=%s\n", outcome.Record.Stage)
}

func assertPersonalCatalogSource(t *testing.T, root, want string) {
	t.Helper()
	catalogs, err := packaging.LoadCatalogs(root)
	if err != nil {
		t.Fatalf("load evolved catalog: %v", err)
	}
	for _, skill := range catalogs.Skills.Skills {
		if skill.Name == evolvedSkillID {
			if skill.ProfileSources == nil || skill.ProfileSources.PersonalAutonomousVenture == nil {
				t.Fatal("catalog lacks personal-autonomous-venture source")
			}
			if skill.ProfileSources.PersonalAutonomousVenture.Source != want {
				t.Fatalf("personal catalog source=%q, want %q", skill.ProfileSources.PersonalAutonomousVenture.Source, want)
			}
			return
		}
	}
	t.Fatalf("evolved skill %q missing from catalog", evolvedSkillID)
}

func runRollbackCommand(t *testing.T, repository, catalogRoot string) []byte {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "foundry")
	build := exec.Command("go", "build", "-o", binary, "./cmd/foundry")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build foundry CLI: %v\n%s", err, output)
	}
	command := exec.Command(binary, "skills", "rollback", "-root", catalogRoot, "-skill", evolvedSkillID)
	command.Dir = repository
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("one-command rollback: %v\n%s", err, output)
	}
	return output
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got := mustRead(t, path)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s bytes differ from expected immutable version", path)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return content
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func versionDiff(base, promoted []byte) string {
	return fmt.Sprintf(
		"base_sha256=%s\npromoted_sha256=%s\nbase_bytes=%d\npromoted_bytes=%d\nchange=bounded-review-procedure-appended\n",
		digest(base),
		digest(promoted),
		len(base),
		len(promoted),
	)
}

func evidenceReadme() string {
	return `# Task 155 evidence

This archive is produced by the hermetic skill-evolution end-to-end test.

- ` + "`promotion-rows.jsonl`" + ` shows the personal promotion and append-only rollback records; ` + "`org-proposal-rows.jsonl`" + ` records a proposal without activation.
- ` + "`profile-isolation-proof.txt`" + ` proves the same catalog selects personal v2 while the organization profile remains on baseline v1.
- ` + "`version-diff.txt`" + ` and ` + "`rollback-proof.txt`" + ` contain bounded byte counts and digests proving the promoted change, restored bytes, and retained v1/v2/v3 history without copying executable prompt text into evidence.
- ` + "`gate-proof.txt`" + ` records the permission-expansion and cumulative-drift freeze negative paths.
- ` + "`authority-before.sha256`" + ` and ` + "`authority-after.sha256`" + ` cover config, policy, SCM, and deploy trees and must match byte-for-byte.

No credentials, network calls, canonical repository writes, SCM operations, or deploy operations are used.
`
}
