package evolve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/packaging"
	"gopkg.in/yaml.v3"
)

const packageTestSkill = "code-reviewer-correctness"

func TestSkillPackageBridgePromoteAndRestartRollback(t *testing.T) {
	Unfreeze()
	root, base := newSkillPackageFixture(t)
	catalogPath := filepath.Join(root, filepath.FromSlash(packaging.SkillCatalogPath))
	if err := os.Chmod(catalogPath, 0o644); err != nil {
		t.Fatal(err)
	}
	bridge := newSkillPackageBridge(root, base)
	proposed := cloneSkillVersion(base)
	proposed.Prompt += "\nCite the smallest relevant line range.\n"

	outcome, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages())
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if !outcome.Applied || outcome.Record.Stage != StagePromoted {
		t.Fatalf("outcome = %+v, want applied promotion", outcome)
	}
	assertPackageBytes(t, root, 1, []byte(base.Prompt))
	assertPackageBytes(t, root, 2, []byte(proposed.Prompt))
	assertPersonalSource(t, root, versionSource(packageTestSkill, 2))
	info, err := os.Stat(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("catalog mode=%v, want 0644", info.Mode().Perm())
	}

	// A fresh bridge proves rollback derives its history from durable state.
	record, err := (&SkillPackageBridge{Root: root, DurableFreeze: thawedFreeze()}).Rollback(context.Background(), packageTestSkill)
	if err != nil {
		t.Fatalf("restart rollback: %v", err)
	}
	if record.ToVersion != 3 || record.State != "committed" {
		t.Fatalf("rollback record = %+v", record)
	}
	assertPackageBytes(t, root, 2, []byte(proposed.Prompt))
	assertPackageBytes(t, root, 3, []byte(base.Prompt))
	assertPersonalSource(t, root, versionSource(packageTestSkill, 3))

	rows, err := ReadSkillPackageRecords(root)
	if err != nil {
		t.Fatalf("read records: %v", err)
	}
	if len(rows) != 4 { // prepared+committed for promote and rollback
		t.Fatalf("records = %d, want 4", len(rows))
	}
	journal, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evolutionJournal)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(journal, []byte(proposed.Prompt)) {
		t.Fatal("promotion journal contains prompt content")
	}
}

func TestSkillPackageBridgeRollbackReconcilesCoherentRegistry(t *testing.T) {
	root, base := newSkillPackageFixture(t)
	bridge := newSkillPackageBridge(root, base)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	if outcome, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err != nil || !outcome.Applied {
		t.Fatalf("promote: outcome=%+v err=%v", outcome, err)
	}
	if _, err := bridge.Rollback(context.Background(), packageTestSkill); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	current, ok := bridge.Pipeline.Registry.Current(packageTestSkill)
	if !ok || current.Version != 3 || current.Prompt != base.Prompt {
		t.Fatalf("registry current = %+v, want rollback v3", current)
	}
}

func TestSkillPackageBridgeNonAppliedOutcomesCannotActivate(t *testing.T) {
	tests := []struct {
		name       string
		profile    string
		mutate     func(*SkillVersion)
		freeze     bool
		durable    FreezeReaderFunc
		wantStage  PromotionStage
		wantReason L1ConditionFailure
	}{
		{name: "organization", profile: "organization", mutate: improvePrompt, durable: thawedFreeze(), wantStage: StageProposed},
		{name: "permission expansion", profile: "personal", mutate: func(v *SkillVersion) { v.Permissions = []string{"read", "write"} }, durable: thawedFreeze(), wantStage: StageRejected, wantReason: L1NewPermission},
		{name: "hot freeze", profile: "personal", mutate: improvePrompt, freeze: true, durable: thawedFreeze(), wantStage: StageQuarantined},
		{name: "durable freeze", profile: "personal", mutate: improvePrompt, durable: FreezeReaderFunc(func(context.Context, string) (bool, FreezeCondition, error) { return true, FreezeBudgetExceeded, nil }), wantStage: StageQuarantined},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Unfreeze()
			if test.freeze {
				Freeze(FreezeQualityRegression)
				defer Unfreeze()
			}
			root, base := newSkillPackageFixture(t)
			before := mustReadTestFile(t, filepath.Join(root, packaging.SkillCatalogPath))
			proposed := cloneSkillVersion(base)
			test.mutate(&proposed)
			bridge := newSkillPackageBridge(root, base)
			bridge.DurableFreeze = test.durable
			outcome, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: test.profile}, cleanL1Stages())
			if err != nil {
				t.Fatalf("promote: %v", err)
			}
			if outcome.Applied || outcome.Record.Stage != test.wantStage || outcome.ConditionFailure != test.wantReason {
				t.Fatalf("outcome = %+v", outcome)
			}
			after := mustReadTestFile(t, filepath.Join(root, packaging.SkillCatalogPath))
			if !bytes.Equal(before, after) {
				t.Fatal("non-applied outcome changed catalog")
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(versionSource(packageTestSkill, 2)))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("non-applied outcome created v2: %v", err)
			}
			records, err := ReadSkillPackageRecords(root)
			if err != nil || len(records) != 1 {
				t.Fatalf("non-applied audit records=%d err=%v", len(records), err)
			}
			record := records[0]
			if record.ProposedDigest == "" || record.ProposedAuthorityDigest == "" || record.Reason == "" {
				t.Fatalf("non-applied audit lacks bounded identity: %+v", record)
			}
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(evolutionJournal)))
			if err != nil || bytes.Contains(raw, []byte(proposed.Prompt)) {
				t.Fatalf("non-applied audit leaked prompt: err=%v", err)
			}
		})
	}
}

func TestSkillPackageBridgeFailsClosed(t *testing.T) {
	root, base := newSkillPackageFixture(t)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	candidate := L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}

	bridge := newSkillPackageBridge(root, base)
	bridge.DurableFreeze = nil
	if _, err := bridge.Promote(context.Background(), candidate, cleanL1Stages()); err == nil || !strings.Contains(err.Error(), "freeze reader") {
		t.Fatalf("missing durable freeze error = %v", err)
	}
	bridge.DurableFreeze = FreezeReaderFunc(func(context.Context, string) (bool, FreezeCondition, error) {
		return false, "", errors.New("store unavailable")
	})
	if _, err := bridge.Promote(context.Background(), candidate, cleanL1Stages()); err == nil || !strings.Contains(err.Error(), "store unavailable") {
		t.Fatalf("freeze store error = %v", err)
	}

	wrongID := candidate
	wrongID.Proposed.SkillID = "other"
	bridge.DurableFreeze = thawedFreeze()
	if _, err := bridge.Promote(context.Background(), wrongID, cleanL1Stages()); err == nil {
		t.Fatal("mismatched candidate ids were accepted")
	}
	stale := candidate
	stale.Base.Prompt = "stale"
	if _, err := bridge.Promote(context.Background(), stale, cleanL1Stages()); err == nil {
		t.Fatal("stale candidate base was accepted")
	}
	unknown := candidate
	unknown.Base.SkillID, unknown.Proposed.SkillID = "unknown-skill", "unknown-skill"
	unknown.Base.Prompt = base.Prompt
	unknown.Proposed.Prompt = proposed.Prompt
	unknownBridge := newSkillPackageBridge(root, unknown.Base)
	if _, err := unknownBridge.Promote(context.Background(), unknown, cleanL1Stages()); err == nil || !strings.Contains(err.Error(), "not cataloged") {
		t.Fatalf("unknown catalog skill error = %v", err)
	}
	invalidProfile := candidate
	invalidProfile.Profile = "personal\nsecret"
	if _, err := bridge.Promote(context.Background(), invalidProfile, cleanL1Stages()); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("invalid profile error = %v", err)
	}
}

func TestSkillPackageBridgeClearsOnlyStaleDurableMirror(t *testing.T) {
	Unfreeze()
	defer Unfreeze()
	root, base := newSkillPackageFixture(t)
	MirrorDurableFreeze(FreezeBudgetExceeded)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	outcome, err := newSkillPackageBridge(root, base).Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: personalEvolutionProfile}, cleanL1Stages())
	if err != nil || !outcome.Applied || IsFrozen() {
		t.Fatalf("durably thawed bridge did not clear stale mirror: outcome=%+v frozen=%v err=%v", outcome, IsFrozen(), err)
	}
}

func TestSkillPackageBridgeRecoversOrphanedVersion(t *testing.T) {
	root, base := newSkillPackageFixture(t)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	candidate := L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}
	bridge := newSkillPackageBridge(root, base)
	bridge.beforeStep = func(step string) error {
		if step == "before-catalog" {
			return errors.New("injected catalog failure")
		}
		return nil
	}
	if _, err := bridge.Promote(context.Background(), candidate, cleanL1Stages()); err == nil {
		t.Fatal("injected failure was ignored")
	}
	assertPackageBytes(t, root, 2, []byte(proposed.Prompt))
	assertNoPersonalSource(t, root)

	bridge.beforeStep = nil
	if outcome, err := bridge.Promote(context.Background(), candidate, cleanL1Stages()); err != nil || !outcome.Applied {
		t.Fatalf("recover orphaned version: outcome=%+v err=%v", outcome, err)
	}
	assertPersonalSource(t, root, versionSource(packageTestSkill, 2))
}

func TestSkillPackageBridgeRejectsVersionCollisionAndSymlinks(t *testing.T) {
	t.Run("collision", func(t *testing.T) {
		root, base := newSkillPackageFixture(t)
		collision := filepath.Join(root, filepath.FromSlash(versionSource(packageTestSkill, 2)))
		if err := os.MkdirAll(filepath.Dir(collision), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(collision, []byte("attacker-owned"), 0o600); err != nil {
			t.Fatal(err)
		}
		proposed := cloneSkillVersion(base)
		improvePrompt(&proposed)
		if _, err := newSkillPackageBridge(root, base).Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err == nil || (!strings.Contains(err.Error(), "collision") && !strings.Contains(err.Error(), "writable")) {
			t.Fatalf("collision error = %v", err)
		}
		assertNoPersonalSource(t, root)
	})
	t.Run("root symlink", func(t *testing.T) {
		root, base := newSkillPackageFixture(t)
		link := filepath.Join(t.TempDir(), "root-link")
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}
		proposed := cloneSkillVersion(base)
		improvePrompt(&proposed)
		bridge := newSkillPackageBridge(link, base)
		if _, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err == nil {
			t.Fatal("symlink root was accepted")
		}
	})
	t.Run("version parent symlink", func(t *testing.T) {
		root, base := newSkillPackageFixture(t)
		outside := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "skills", packageTestSkill), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "skills", packageTestSkill, "versions")); err != nil {
			t.Fatal(err)
		}
		proposed := cloneSkillVersion(base)
		improvePrompt(&proposed)
		if _, err := newSkillPackageBridge(root, base).Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err == nil {
			t.Fatal("symlinked version parent was accepted")
		}
		entries, err := os.ReadDir(outside)
		if err != nil || len(entries) != 0 {
			t.Fatalf("outside directory changed: entries=%v err=%v", entries, err)
		}
	})
	t.Run("lock symlink", func(t *testing.T) {
		root, base := newSkillPackageFixture(t)
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(evolutionDirectory)), 0o700); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside-lock")
		if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(evolutionLock))); err != nil {
			t.Fatal(err)
		}
		proposed := cloneSkillVersion(base)
		improvePrompt(&proposed)
		if _, err := newSkillPackageBridge(root, base).Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err == nil || !strings.Contains(err.Error(), "lock") {
			t.Fatalf("lock symlink error = %v", err)
		}
		if got := mustReadTestFile(t, outside); string(got) != "unchanged" {
			t.Fatalf("outside lock changed to %q", got)
		}
	})
	t.Run("version hardlink", func(t *testing.T) {
		root, base := newSkillPackageFixture(t)
		outside := filepath.Join(t.TempDir(), "outside-version")
		if err := os.WriteFile(outside, []byte("attacker-owned"), 0o400); err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(root, filepath.FromSlash(versionSource(packageTestSkill, 2)))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(outside, destination); err != nil {
			t.Fatal(err)
		}
		proposed := cloneSkillVersion(base)
		improvePrompt(&proposed)
		if _, err := newSkillPackageBridge(root, base).Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err == nil || !strings.Contains(err.Error(), "hard link") {
			t.Fatalf("hardlink collision error = %v", err)
		}
		if got := mustReadTestFile(t, outside); string(got) != "attacker-owned" {
			t.Fatalf("outside hardlink changed to %q", got)
		}
	})
	t.Run("journal symlink", func(t *testing.T) {
		root, base := newSkillPackageFixture(t)
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(evolutionDirectory)), 0o700); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside-journal")
		if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(evolutionJournal))); err != nil {
			t.Fatal(err)
		}
		proposed := cloneSkillVersion(base)
		improvePrompt(&proposed)
		outcome, err := newSkillPackageBridge(root, base).Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "organization"}, cleanL1Stages())
		if err != nil || !outcome.ProposalOnly {
			t.Fatalf("derived journal must not control authoritative outcome: outcome=%+v err=%v", outcome, err)
		}
		if got := mustReadTestFile(t, outside); string(got) != "unchanged" {
			t.Fatalf("outside journal changed to %q", got)
		}
	})
}

func TestSkillPackageBridgeConcurrentSameBaseAppliesExactlyOnce(t *testing.T) {
	root, base := newSkillPackageFixture(t)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	candidate := L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}
	bridges := []*SkillPackageBridge{newSkillPackageBridge(root, base), newSkillPackageBridge(root, base)}
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for _, bridge := range bridges {
		wg.Add(1)
		go func(bridge *SkillPackageBridge) {
			defer wg.Done()
			outcome, err := bridge.Promote(context.Background(), candidate, cleanL1Stages())
			results <- err == nil && outcome.Applied
		}(bridge)
	}
	wg.Wait()
	close(results)
	applied := 0
	for result := range results {
		if result {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("applied promotions = %d, want exactly 1", applied)
	}
	assertPersonalSource(t, root, versionSource(packageTestSkill, 2))
}

func TestSkillPackageBridgePinsAuthorityAndRejectsTamper(t *testing.T) {
	root, base := newSkillPackageFixture(t)
	bridge := newSkillPackageBridge(root, base)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	if outcome, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err != nil || !outcome.Applied {
		t.Fatalf("promote: outcome=%+v err=%v", outcome, err)
	}
	pin := readPackageTestCatalog(t, root).Skills[0].ProfileSources.PersonalAutonomousVenture
	if pin == nil || pin.SHA256 == "" || pin.AuthoritySHA256 == "" {
		t.Fatalf("catalog lacks content-addressed authority pins: %+v", pin)
	}
	metadata := mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(metadataSource(packageTestSkill, 2))))
	if digestSkillBytes(metadata) != pin.AuthoritySHA256 || bytes.Contains(metadata, []byte(proposed.Prompt)) {
		t.Fatal("metadata pin is wrong or metadata leaks prompt content")
	}

	t.Run("forged restart authority", func(t *testing.T) {
		forged := cloneSkillVersion(proposed)
		forged.Version = 2
		forged.Permissions = []string{"read", "write"}
		next := cloneSkillVersion(forged)
		improvePrompt(&next)
		if _, err := newSkillPackageBridge(root, forged).Promote(context.Background(), L1Candidate{Base: forged, Proposed: next, Profile: "personal"}, cleanL1Stages()); err == nil || !strings.Contains(err.Error(), "authority") {
			t.Fatalf("forged restart authority error = %v", err)
		}
	})

	t.Run("content tamper", func(t *testing.T) {
		copyRoot, copiedBase := newSkillPackageFixture(t)
		copyBridge := newSkillPackageBridge(copyRoot, copiedBase)
		copyProposed := cloneSkillVersion(copiedBase)
		improvePrompt(&copyProposed)
		if _, err := copyBridge.Promote(context.Background(), L1Candidate{Base: copiedBase, Proposed: copyProposed, Profile: "personal"}, cleanL1Stages()); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(copyRoot, filepath.FromSlash(versionSource(packageTestSkill, 2)))
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("tampered\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		active := cloneSkillVersion(copyProposed)
		active.Version = 2
		next := cloneSkillVersion(active)
		improvePrompt(&next)
		if _, err := newSkillPackageBridge(copyRoot, active).Promote(context.Background(), L1Candidate{Base: active, Proposed: next, Profile: "personal"}, cleanL1Stages()); err == nil {
			t.Fatal("tampered content was accepted")
		}
	})

	t.Run("metadata tamper", func(t *testing.T) {
		path := filepath.Join(root, filepath.FromSlash(metadataSource(packageTestSkill, 2)))
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		active := cloneSkillVersion(proposed)
		active.Version = 2
		next := cloneSkillVersion(active)
		improvePrompt(&next)
		if _, err := newSkillPackageBridge(root, active).Promote(context.Background(), L1Candidate{Base: active, Proposed: next, Profile: "personal"}, cleanL1Stages()); err == nil {
			t.Fatal("tampered metadata was accepted")
		}
	})
}

func TestSkillPackageBridgeCrashRecoveryAndTornJSONL(t *testing.T) {
	root, base := newSkillPackageFixture(t)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	bridge := newSkillPackageBridge(root, base)
	bridge.beforeStep = func(step string) error {
		if step == "after-catalog" {
			return errors.New("crash after activation")
		}
		return nil
	}
	if _, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err == nil || !strings.Contains(err.Error(), "recovery required") {
		t.Fatalf("after-catalog failure = %v", err)
	}
	active := cloneSkillVersion(proposed)
	active.Version = 2
	next := cloneSkillVersion(active)
	improvePrompt(&next)
	if _, err := newSkillPackageBridge(root, active).Promote(context.Background(), L1Candidate{Base: active, Proposed: next, Profile: "personal"}, L1Stages{ShadowPass: false}); err != nil {
		t.Fatalf("startup reconciliation: %v", err)
	}
	records, err := ReadSkillPackageRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	foundRecovered := false
	for _, record := range records {
		foundRecovered = foundRecovered || record.State == "recovered-committed"
	}
	if !foundRecovered {
		t.Fatalf("records lack recovered commit: %+v", records)
	}
	journal := filepath.Join(root, filepath.FromSlash(evolutionJournal))
	file, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("{torn")
	_ = file.Close()
	if recovered, err := ReadSkillPackageRecords(root); err != nil || len(recovered) != len(records) {
		t.Fatalf("torn derived JSONL affected authority: records=%d err=%v", len(recovered), err)
	}
}

func TestSkillPackageAuditRejectsSemanticForgeryAndIgnoresDerivedJournal(t *testing.T) {
	t.Run("semantic forgery", func(t *testing.T) {
		rootPath, _ := newSkillPackageFixture(t)
		root, err := openEvolutionRoot(rootPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := ensureDirectories(root, evolutionRecords); err != nil {
			t.Fatal(err)
		}
		forged := SkillPackageRecord{
			Sequence: 1, Action: "execute", State: "committed", SkillID: packageTestSkill,
			Profile: personalEvolutionProfile, Stage: StagePromoted, FromVersion: 1, ToVersion: 2,
			Source: versionSource(packageTestSkill, 2), Digest: digestSkillBytes([]byte("content")),
			AuthorityDigest: digestSkillBytes([]byte("authority")), CreatedAt: time.Now().UTC(),
		}
		raw, _ := json.Marshal(forged)
		raw = append(raw, '\n')
		if err := writeImmutable(root, evolutionRecords+"/00000000000000000001.json", raw); err != nil {
			t.Fatal(err)
		}
		_ = root.Close()
		if _, err := ReadSkillPackageRecords(rootPath); err == nil || !strings.Contains(err.Error(), "invalid authoritative record") {
			t.Fatalf("semantic forgery error = %v", err)
		}
	})

	t.Run("derived journal is not authority", func(t *testing.T) {
		rootPath, _ := newSkillPackageFixture(t)
		if err := os.MkdirAll(filepath.Join(rootPath, filepath.FromSlash(evolutionDirectory)), 0o700); err != nil {
			t.Fatal(err)
		}
		fake := []byte(`{"sequence":1,"action":"promote"}` + "\n")
		if err := os.WriteFile(filepath.Join(rootPath, filepath.FromSlash(evolutionJournal)), fake, 0o600); err != nil {
			t.Fatal(err)
		}
		records, err := ReadSkillPackageRecords(rootPath)
		if err != nil || len(records) != 0 {
			t.Fatalf("derived journal became authority: records=%+v err=%v", records, err)
		}
	})
}

func TestSkillPackageBridgeDurableBudgetSurvivesRestart(t *testing.T) {
	Unfreeze()
	defer Unfreeze()
	root, current := newSkillPackageFixture(t)
	for promotion := 0; promotion < 3; promotion++ {
		bridge := newSkillPackageBridge(root, current)
		bridge.Pipeline.Limits = DefaultChangeBudgetLimits()
		bridge.Pipeline.Limits.MaxPromotions = 2
		proposed := cloneSkillVersion(current)
		proposed.Prompt += "\nPromotion " + string(rune('A'+promotion)) + ".\n"
		outcome, err := bridge.Promote(context.Background(), L1Candidate{Base: current, Proposed: proposed, Profile: "personal"}, cleanL1Stages())
		if err != nil {
			t.Fatalf("promotion %d: %v", promotion+1, err)
		}
		if promotion < 2 {
			if !outcome.Applied {
				t.Fatalf("promotion %d was not applied: %+v", promotion+1, outcome)
			}
			proposed.Version = current.Version + 1
			current = proposed
		} else if outcome.Applied || outcome.Record.Stage != StageQuarantined {
			t.Fatalf("durable budget did not quarantine third promotion: %+v", outcome)
		}
	}
}

func TestSkillPackageBridgeRollbackBudgetAndFreezeSafetyBrake(t *testing.T) {
	Unfreeze()
	defer Unfreeze()
	root, base := newSkillPackageFixture(t)
	bridge := newSkillPackageBridge(root, base)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	if _, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err != nil {
		t.Fatal(err)
	}
	Freeze(FreezeBudgetExceeded)
	if _, err := (&SkillPackageBridge{Root: root}).Rollback(context.Background(), packageTestSkill); err != nil {
		t.Fatalf("frozen rollback safety brake: %v", err)
	}
	Unfreeze()
	if _, err := (&SkillPackageBridge{Root: root}).Rollback(context.Background(), packageTestSkill); err != nil {
		t.Fatalf("second restart rollback: %v", err)
	}
	if record, err := (&SkillPackageBridge{Root: root, DurableFreeze: thawedFreeze()}).Rollback(context.Background(), packageTestSkill); err != nil || record.ToVersion != 5 {
		t.Fatalf("over-budget rollback safety brake record=%+v err=%v", record, err)
	}
	opened, err := openEvolutionRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = opened.Close() }()
	budget, err := loadDurableBudget(opened, time.Now().UTC())
	if err != nil || budget.Window.RollbackChainDepth != 3 || budget.Window.FilesChanged != 14 || !IsFrozen() {
		t.Fatalf("durable rollback freeze state=%+v err=%v", budget.Window, err)
	}
}

func TestSkillPackageBridgeRollbackBudgetBreachRequiresDurableFreeze(t *testing.T) {
	Unfreeze()
	defer Unfreeze()
	root, base := newSkillPackageFixture(t)
	bridge := newSkillPackageBridge(root, base)
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	if _, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := (&SkillPackageBridge{Root: root}).Rollback(context.Background(), packageTestSkill); err != nil {
			t.Fatalf("rollback %d: %v", i+1, err)
		}
	}
	if _, err := (&SkillPackageBridge{Root: root}).Rollback(context.Background(), packageTestSkill); err == nil || !strings.Contains(err.Error(), "durable freeze store") {
		t.Fatalf("over-budget rollback without durable gate error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(versionSource(packageTestSkill, 5)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe rollback created v5 despite missing durable freeze: %v", err)
	}
}

func TestSkillPackageBridgeRejectsInvalidRepositoryBeforeDecision(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "duplicate catalog", mutate: func(t *testing.T, root string) {
			catalog := readPackageTestCatalog(t, root)
			catalog.Skills = append(catalog.Skills, catalog.Skills[0])
			writeTestYAML(t, root, packaging.SkillCatalogPath, catalog)
		}},
		{name: "non skill source", mutate: func(t *testing.T, root string) {
			catalog := readPackageTestCatalog(t, root)
			catalog.Skills[0].Source = "skills/code-reviewer-correctness/instructions.txt"
			writeTestFile(t, root, catalog.Skills[0].Source, []byte("bad\n"))
			writeTestYAML(t, root, packaging.SkillCatalogPath, catalog)
		}},
		{name: "not enabled", mutate: func(t *testing.T, root string) {
			enabled := packaging.Enablement{Version: 1, Profile: packaging.PersonalAutonomousVentureProfile, Agents: []string{"implementation", "reviewer", "extra-agent"}, Skills: []string{"extra-skill"}, DomainSkills: []string{}}
			writeTestYAML(t, root, "templates/product/.foundry/skills/enabled.yaml", enabled)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, base := newSkillPackageFixture(t)
			test.mutate(t, root)
			proposed := cloneSkillVersion(base)
			improvePrompt(&proposed)
			if _, err := newSkillPackageBridge(root, base).Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages()); err == nil {
				t.Fatal("invalid repository was accepted")
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(versionSource(packageTestSkill, 2)))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid repository created v2: %v", err)
			}
		})
	}
}

func TestSkillPackageBudgetRollingBoundaryAndCorruption(t *testing.T) {
	root, _ := newSkillPackageFixture(t)
	opened, err := openEvolutionRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = opened.Close() }()
	day0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	state, err := loadDurableBudget(opened, day0)
	if err != nil {
		t.Fatal(err)
	}
	if breach, err := reserveDurableBudget(opened, &state, DefaultChangeBudgetLimits(), budgetReservation{Timestamp: day0, Key: digestSkillBytes([]byte("day-zero")), Action: "promote", FilesChanged: 5}); err != nil || breach != "" {
		t.Fatalf("reserve: breach=%s err=%v", breach, err)
	}
	if _, err := reserveDurableBudget(opened, &state, DefaultChangeBudgetLimits(), budgetReservation{Timestamp: day0.Add(-time.Second), Key: digestSkillBytes([]byte("backdated")), Action: "promote", FilesChanged: 1}); err == nil {
		t.Fatal("backdated reservation was accepted")
	}
	day29, err := loadDurableBudget(opened, day0.AddDate(0, 0, 29))
	if err != nil || day29.Window.Promotions != 1 || day29.Window.FilesChanged != 5 {
		t.Fatalf("day29 window=%+v err=%v", day29.Window, err)
	}
	day30, err := loadDurableBudget(opened, day0.AddDate(0, 0, 30))
	if err != nil || day30.Window.Promotions != 0 || day30.Window.FilesChanged != 0 {
		t.Fatalf("day30 window=%+v err=%v", day30.Window, err)
	}

	for _, test := range []struct {
		name   string
		events []budgetReservation
	}{
		{name: "invalid key", events: []budgetReservation{{Sequence: 1, Timestamp: day0, Key: "a", Action: "promote", FilesChanged: 1}}},
		{name: "unknown action", events: []budgetReservation{{Sequence: 1, Timestamp: day0, Key: digestSkillBytes([]byte("unknown")), Action: "unknown", FilesChanged: 1}}},
		{name: "negative files", events: []budgetReservation{{Sequence: 1, Timestamp: day0, Key: digestSkillBytes([]byte("negative")), Action: "promote", FilesChanged: -1}}},
		{name: "zero files", events: []budgetReservation{{Sequence: 1, Timestamp: day0, Key: digestSkillBytes([]byte("zero")), Action: "promote", FilesChanged: 0}}},
		{name: "backwards timestamp", events: []budgetReservation{{Sequence: 1, Timestamp: day0, Key: digestSkillBytes([]byte("first")), Action: "promote", FilesChanged: 1}, {Sequence: 2, Timestamp: day0.Add(-time.Second), Key: digestSkillBytes([]byte("second")), Action: "rollback", FilesChanged: 1}}},
		{name: "duplicate key", events: []budgetReservation{{Sequence: 1, Timestamp: day0, Key: digestSkillBytes([]byte("same")), Action: "promote", FilesChanged: 1}, {Sequence: 2, Timestamp: day0, Key: digestSkillBytes([]byte("same")), Action: "rollback", FilesChanged: 1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			corruptRoot, _ := newSkillPackageFixture(t)
			corrupt, err := openEvolutionRoot(corruptRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = corrupt.Close() }()
			if err := ensureDirectories(corrupt, evolutionBudgetRecords); err != nil {
				t.Fatal(err)
			}
			for _, event := range test.events {
				raw, _ := json.Marshal(event)
				raw = append(raw, '\n')
				path := filepath.ToSlash(filepath.Join(evolutionBudgetRecords, fmt.Sprintf("%020d.json", event.Sequence)))
				if err := writeImmutable(corrupt, path, raw); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := loadDurableBudget(corrupt, day0); err == nil {
				t.Fatal("corrupt budget record was accepted")
			}
		})
	}
}

func TestSkillPackageBudgetMaxFilesFreezesWithoutWritingVersions(t *testing.T) {
	Unfreeze()
	defer Unfreeze()
	root, base := newSkillPackageFixture(t)
	gate := &recordingFreezeGate{}
	bridge := newSkillPackageBridge(root, base)
	bridge.DurableFreeze = gate
	bridge.Pipeline.Limits = DefaultChangeBudgetLimits()
	bridge.Pipeline.Limits.MaxFilesChanged = 4 // first promotion needs v1+v2 metadata/content + catalog = 5
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	outcome, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages())
	if err != nil || outcome.Applied || outcome.Record.Stage != StageQuarantined {
		t.Fatalf("max-files outcome=%+v err=%v", outcome, err)
	}
	if !gate.isFrozen(FreezeBudgetExceeded) || !IsFrozen() {
		t.Fatal("budget breach did not propagate durable and hot freeze")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(versionSource(packageTestSkill, 1)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("over-budget promotion wrote package versions: %v", err)
	}
}

func TestSkillPackageHotFreezeCannotSlipCatalogActivation(t *testing.T) {
	Unfreeze()
	defer Unfreeze()
	root, base := newSkillPackageFixture(t)
	bridge := newSkillPackageBridge(root, base)
	reached := make(chan struct{})
	release := make(chan struct{})
	bridge.beforeStep = func(step string) error {
		if step == "before-catalog" {
			close(reached)
			<-release
		}
		return nil
	}
	proposed := cloneSkillVersion(base)
	improvePrompt(&proposed)
	promoted := make(chan error, 1)
	go func() {
		outcome, err := bridge.Promote(context.Background(), L1Candidate{Base: base, Proposed: proposed, Profile: "personal"}, cleanL1Stages())
		if err == nil && !outcome.Applied {
			err = errors.New("promotion was not applied")
		}
		promoted <- err
	}()
	<-reached
	freezeStarted := make(chan struct{})
	freezeDone := make(chan struct{})
	go func() {
		close(freezeStarted)
		Freeze(FreezeQualityRegression)
		close(freezeDone)
	}()
	<-freezeStarted
	select {
	case <-freezeDone:
		t.Fatal("Freeze slipped through the active catalog read lock")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-promoted; err != nil {
		t.Fatalf("promotion: %v", err)
	}
	select {
	case <-freezeDone:
	case <-time.After(time.Second):
		t.Fatal("Freeze did not complete after activation released the lock")
	}
	assertPersonalSource(t, root, versionSource(packageTestSkill, 2))
}

func TestEvolutionReadRejectsDescriptorSwap(t *testing.T) {
	rootPath, _ := newSkillPackageFixture(t)
	root, err := openEvolutionRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	target := "skills/code-reviewer-correctness/SKILL.md"
	replacement := filepath.Join(rootPath, "replacement.md")
	if err := os.WriteFile(replacement, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = readRegularWithHook(root, target, func(stage, path string) error {
		if stage == "after-file-lstat" && path == target {
			return os.Rename(replacement, filepath.Join(rootPath, filepath.FromSlash(target)))
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "changed while opening") {
		t.Fatalf("descriptor swap error = %v", err)
	}
}

type recordingFreezeGate struct {
	mu     sync.Mutex
	frozen bool
	reason FreezeCondition
}

func (g *recordingFreezeGate) AcquirePromotionGuard(context.Context, string) (*PromotionGuard, bool, FreezeCondition, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.frozen {
		return nil, true, g.reason, nil
	}
	return &PromotionGuard{}, false, "", nil
}

func (g *recordingFreezeGate) Freeze(_ context.Context, _ string, reason FreezeCondition) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.frozen, g.reason = true, reason
	return nil
}

func (g *recordingFreezeGate) isFrozen(reason FreezeCondition) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.frozen && g.reason == reason
}

func newSkillPackageFixture(t *testing.T) (string, SkillVersion) {
	t.Helper()
	root := t.TempDir()
	source := filepath.ToSlash(filepath.Join("skills", packageTestSkill, "SKILL.md"))
	prompt := "Review the diff for correctness.\n"
	if err := os.MkdirAll(filepath.Join(root, "skills", packageTestSkill), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(source)), []byte(prompt), 0o600); err != nil {
		t.Fatal(err)
	}
	extraSkillSource := "skills/extra-skill/SKILL.md"
	writeTestFile(t, root, extraSkillSource, []byte("Extra bounded skill.\n"))
	skillCatalog := packaging.SkillCatalog{Version: 1, Skills: []packaging.Skill{
		{Name: packageTestSkill, Description: "test", Source: source, References: []string{}},
		{Name: "extra-skill", Description: "strict-subset fixture", Source: extraSkillSource, References: []string{}},
	}, DomainSkills: []packaging.Skill{}}
	writeTestYAML(t, root, packaging.SkillCatalogPath, skillCatalog)

	agents := []packaging.Agent{
		{Name: "implementation", Description: "bounded implementer", Source: "agents/implementation.md", Skills: []string{packageTestSkill}, Inputs: []string{"task"}, Outputs: []string{"diff"}},
		{Name: "reviewer", Description: "independent reviewer", Source: "agents/reviewer.md", Skills: []string{packageTestSkill}, Inputs: []string{"diff"}, Outputs: []string{"review"}},
		{Name: "extra-agent", Description: "strict-subset fixture", Source: "agents/extra-agent.md", Skills: []string{packageTestSkill}, Inputs: []string{"task"}, Outputs: []string{"note"}},
	}
	for _, agent := range agents {
		writeTestFile(t, root, agent.Source, []byte("# "+agent.Name+"\n"))
	}
	agentCatalog := packaging.AgentCatalog{Version: 1, Agents: agents, Bindings: []packaging.TaskBinding{{Name: "implementation-review", Implementer: "implementation", Reviewer: "reviewer"}}}
	writeTestYAML(t, root, packaging.AgentCatalogPath, agentCatalog)

	enabled := packaging.Enablement{Version: 1, Profile: packaging.PersonalAutonomousVentureProfile, Agents: []string{"implementation", "reviewer", "extra-agent"}, Skills: []string{packageTestSkill, "extra-skill"}, DomainSkills: []string{}}
	writeTestYAML(t, root, "templates/product/.foundry/skills/enabled.yaml", enabled)
	writeTestFile(t, root, "config/profiles/personal-autonomous-venture.yaml", []byte("agent_packages:\n  enabled: [implementation, reviewer, extra-agent]\nskill_packages:\n  enabled: [code-reviewer-correctness, extra-skill]\n  domain_enabled: []\n"))
	writeTestFile(t, root, "config/profiles/organization-10x.yaml", []byte("agent_packages:\n  enabled: [implementation, reviewer]\nskill_packages:\n  enabled: [code-reviewer-correctness]\n  domain_enabled: []\n"))
	return root, SkillVersion{SkillID: packageTestSkill, Version: 1, Prompt: prompt, Permissions: []string{"read"}, DataClasses: []string{"code"}, BudgetUSD: 1}
}

func writeTestYAML(t *testing.T, root, relative string, value any) {
	t.Helper()
	raw, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, relative, raw)
}

func writeTestFile(t *testing.T, root, relative string, content []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newSkillPackageBridge(root string, base SkillVersion) *SkillPackageBridge {
	registry := NewSkillRegistry()
	registry.Register(base)
	return &SkillPackageBridge{
		Root: root, DurableFreeze: thawedFreeze(),
		Pipeline: L1Pipeline{Registry: registry, Suite: GoldenSuite{Tasks: []GoldenTask{{Name: "non-empty", Check: func(version SkillVersion) bool { return version.Prompt != "" }}}}, Limits: DefaultChangeBudgetLimits()},
	}
}

func thawedFreeze() FreezeReaderFunc {
	return func(context.Context, string) (bool, FreezeCondition, error) { return false, "", nil }
}

func cleanL1Stages() L1Stages { return L1Stages{ShadowPass: true, CanaryPass: true} }

func improvePrompt(version *SkillVersion) { version.Prompt += "\nImproved review instructions.\n" }

func assertPackageBytes(t *testing.T, root string, version int, want []byte) {
	t.Helper()
	got := mustReadTestFile(t, filepath.Join(root, filepath.FromSlash(versionSource(packageTestSkill, version))))
	if !bytes.Equal(got, want) {
		t.Fatalf("v%d bytes differ", version)
	}
}

func assertPersonalSource(t *testing.T, root, want string) {
	t.Helper()
	catalog := readPackageTestCatalog(t, root)
	if catalog.Skills[0].Source != "skills/"+packageTestSkill+"/SKILL.md" {
		t.Fatalf("global source changed to %q", catalog.Skills[0].Source)
	}
	if catalog.Skills[0].ProfileSources == nil || catalog.Skills[0].ProfileSources.PersonalAutonomousVenture == nil || catalog.Skills[0].ProfileSources.PersonalAutonomousVenture.Source != want {
		t.Fatalf("personal source = %+v, want %q", catalog.Skills[0].ProfileSources, want)
	}
}

func assertNoPersonalSource(t *testing.T, root string) {
	t.Helper()
	if readPackageTestCatalog(t, root).Skills[0].ProfileSources != nil {
		t.Fatal("personal profile source was activated")
	}
}

func readPackageTestCatalog(t *testing.T, root string) packaging.SkillCatalog {
	t.Helper()
	var catalog packaging.SkillCatalog
	if err := yaml.Unmarshal(mustReadTestFile(t, filepath.Join(root, packaging.SkillCatalogPath)), &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog
}

func mustReadTestFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
