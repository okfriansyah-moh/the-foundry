package evolve

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/packaging"
	"gopkg.in/yaml.v3"
)

const (
	personalEvolutionProfile = "personal"
	evolutionDirectory       = "skills/.evolution"
	evolutionJournal         = evolutionDirectory + "/promotions.jsonl"
	evolutionLock            = evolutionDirectory + "/catalog.lock"
	evolutionRecords         = evolutionDirectory + "/records"
	evolutionBudgetRecords   = evolutionDirectory + "/budget-records"
)

var (
	skillPackageName          = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	auditReasonPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9:._-]{0,95}$`)
	skillPackageDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// DurableFreezeGate holds the Task-127 freeze serialization lock across the
// complete activation boundary. A read-only latch check is insufficient: a
// concurrent Freeze could otherwise win between the check and catalog rename.
type DurableFreezeGate interface {
	AcquirePromotionGuard(context.Context, string) (*PromotionGuard, bool, FreezeCondition, error)
	Freeze(context.Context, string, FreezeCondition) error
}

// FreezeReaderFunc adapts a function for tests and callers that already own a
// durable freeze implementation.
type FreezeReaderFunc func(context.Context, string) (bool, FreezeCondition, error)

// AcquirePromotionGuard implements DurableFreezeGate for deterministic tests.
// The returned zero-value guard is a stable no-op guard; production uses the
// transaction-backed FreezeStore.
func (f FreezeReaderFunc) AcquirePromotionGuard(ctx context.Context, scope string) (*PromotionGuard, bool, FreezeCondition, error) {
	frozen, reason, err := f(ctx, scope)
	if err != nil || frozen {
		return nil, frozen, reason, err
	}
	return &PromotionGuard{}, false, "", nil
}

// Freeze is a no-op for the deterministic test adapter. Tests that verify
// propagation use a stateful DurableFreezeGate instead.
func (f FreezeReaderFunc) Freeze(context.Context, string, FreezeCondition) error { return nil }

// SkillPackageBridge is the kernel-owned activation boundary between Task 77
// L1 decisions and Task 153/154 packages. It writes canonical package state;
// it never installs an executor projection or calls SCM/deploy code.
type SkillPackageBridge struct {
	Root          string
	Pipeline      L1Pipeline
	DurableFreeze DurableFreezeGate
	now           func() time.Time
	beforeStep    func(string) error
}

// SkillPackageRecord is a bounded, append-only audit row. Prompt contents are
// deliberately excluded because evolved instructions can contain secrets.
type SkillPackageRecord struct {
	Sequence                int            `json:"sequence"`
	Action                  string         `json:"action"`
	State                   string         `json:"state"`
	SkillID                 string         `json:"skill_id"`
	Profile                 string         `json:"profile"`
	Stage                   PromotionStage `json:"stage"`
	FromVersion             int            `json:"from_version"`
	ToVersion               int            `json:"to_version"`
	Source                  string         `json:"source,omitempty"`
	Digest                  string         `json:"digest,omitempty"`
	AuthorityDigest         string         `json:"authority_digest,omitempty"`
	ProposedDigest          string         `json:"proposed_digest,omitempty"`
	ProposedAuthorityDigest string         `json:"proposed_authority_digest,omitempty"`
	Reason                  string         `json:"reason,omitempty"`
	CreatedAt               time.Time      `json:"created_at"`
}

type skillVersionMetadata struct {
	SkillID      string   `json:"skill_id"`
	Version      int      `json:"version"`
	PromptSHA256 string   `json:"prompt_sha256"`
	Permissions  []string `json:"permissions"`
	DataClasses  []string `json:"data_classes"`
	BudgetUSD    float64  `json:"budget_usd"`
}

// Promote evaluates the unchanged Task 77 gates on a private registry clone,
// then activates only a successful personal promotion. Organization and
// rejected outcomes append an audit row but cannot mutate package versions or
// the catalog pointer.
func (b *SkillPackageBridge) Promote(ctx context.Context, cand L1Candidate, stages L1Stages) (result L1Outcome, returnErr error) {
	if b == nil || b.Pipeline.Registry == nil {
		return L1Outcome{}, fmt.Errorf("evolve: skill package bridge requires a registry")
	}
	if b.DurableFreeze == nil {
		return L1Outcome{}, fmt.Errorf("evolve: durable freeze reader is required")
	}
	if err := validateCandidateIdentity(cand); err != nil {
		return L1Outcome{}, err
	}
	root, err := openEvolutionRoot(b.Root)
	if err != nil {
		return L1Outcome{}, err
	}
	defer func() { _ = root.Close() }()
	unlock, err := lockEvolution(root)
	if err != nil {
		return L1Outcome{}, err
	}
	defer unlock()
	if err := reconcilePreparedRecords(root, b.record); err != nil {
		return L1Outcome{}, err
	}

	registry := b.Pipeline.Registry
	registry.mu.Lock()
	defer registry.mu.Unlock()
	currentHistory := registry.versions[cand.Base.SkillID]
	if len(currentHistory) == 0 || !reflect.DeepEqual(cloneSkillVersion(currentHistory[len(currentHistory)-1]), cloneSkillVersion(cand.Base)) {
		return L1Outcome{}, fmt.Errorf("evolve: candidate base is not the current registry version")
	}

	catalogs, enabled, personal, organization, skillIndex, originalCatalog, err := loadValidatedEvolutionRepository(root, cand.Base.SkillID)
	if err != nil {
		return L1Outcome{}, err
	}
	skill := catalogs.Skills.Skills[skillIndex]
	if len(skill.References) != 0 {
		return L1Outcome{}, fmt.Errorf("evolve: skill %s has references; versioned reference snapshots are required before promotion", skill.Name)
	}
	_, durableVersion, err := verifyActiveVersion(root, skill, cand.Base, cand.Profile == personalEvolutionProfile)
	if err != nil {
		return L1Outcome{}, err
	}

	budget, err := loadDurableBudget(root, b.recordTime())
	if err != nil {
		return L1Outcome{}, err
	}
	decisionRegistry := cloneRegistryLocked(registry)
	decisionPipeline := b.Pipeline
	decisionPipeline.Registry = decisionRegistry
	decisionPipeline.Window = mergeBudgetWindow(decisionPipeline.Window, budget)
	// The durable guard below is the authoritative cross-process freeze state.
	// Ignoring the legacy process latch here prevents an externally cleared
	// durable freeze from remaining stuck until this daemon restarts.
	outcome := decisionPipeline.evaluate(cand, stages, false)
	if !outcome.Applied {
		reason := string(outcome.ConditionFailure)
		if reason == "" {
			reason = string(outcome.Record.Stage)
		}
		if outcome.Record.Stage == StageQuarantined && stages.ShadowPass && stages.CanaryPass && cand.Profile == personalEvolutionProfile {
			projected := decisionPipeline.Window
			projected.Promotions++
			if breaches := projected.Breaches(decisionPipeline.Limits); len(breaches) != 0 {
				reason = "budget:" + string(breaches[0])
				if err := b.DurableFreeze.Freeze(ctx, FreezeScopeGlobal, breaches[0]); err != nil {
					return L1Outcome{}, fmt.Errorf("evolve: engage durable budget freeze: %w", err)
				}
				MirrorDurableFreeze(breaches[0])
			}
		}
		record := b.nonAppliedRecord(cand, outcome, reason)
		if err := appendAuthoritativeRecord(root, record); err != nil {
			return L1Outcome{}, err
		}
		return outcome, nil
	}

	guard, frozen, freezeReason, err := b.DurableFreeze.AcquirePromotionGuard(ctx, FreezeScopeGlobal)
	if err != nil {
		return L1Outcome{}, fmt.Errorf("evolve: acquire durable thawed guard: %w", err)
	}
	if frozen {
		MirrorDurableFreeze(freezeReason)
		outcome.Applied = false
		outcome.Record.Stage = StageQuarantined
		outcome.Record.PromotedValue = float64(cand.Base.Version)
		record := b.nonAppliedRecord(cand, outcome, "freeze:"+string(freezeReason))
		if err := appendAuthoritativeRecord(root, record); err != nil {
			return L1Outcome{}, err
		}
		return outcome, nil
	}
	if guard == nil {
		return L1Outcome{}, fmt.Errorf("evolve: durable thawed guard is nil")
	}
	guardCommitted := false
	defer func() {
		if !guardCommitted {
			if err := guard.Rollback(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("evolve: release thawed guard: %w", err))
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return L1Outcome{}, err
	}
	// The durable guard proves the store is thawed. Clear only a stale mirror;
	// a direct local Freeze remains authoritative for legacy in-process callers.
	clearDurableFreezeMirror()
	activationFreezeMu.RLock()
	activationLocked := true
	defer func() {
		if activationLocked {
			activationFreezeMu.RUnlock()
		}
	}()
	if IsFrozen() {
		outcome.Applied = false
		outcome.Record.Stage = StageQuarantined
		outcome.Record.PromotedValue = float64(cand.Base.Version)
		record := b.nonAppliedRecord(cand, outcome, "freeze:"+string(FreezeReason()))
		if err := appendAuthoritativeRecord(root, record); err != nil {
			return L1Outcome{}, err
		}
		return outcome, nil
	}
	nextVersion := durableVersion + 1
	if nextVersion != int(outcome.Record.PromotedValue) {
		return L1Outcome{}, fmt.Errorf("evolve: inconsistent promoted version")
	}
	newSource := versionSource(cand.Base.SkillID, nextVersion)
	newBytes := []byte(cand.Proposed.Prompt)
	if len(newBytes) == 0 {
		return L1Outcome{}, fmt.Errorf("evolve: proposed prompt must not be empty")
	}
	_, _, contentDigest, authorityDigest, err := encodeVersionObjects(cand.Proposed, nextVersion)
	if err != nil {
		return L1Outcome{}, err
	}
	reservationKey := operationKey("promote", cand.Base.SkillID, durableVersion, nextVersion, contentDigest, authorityDigest)
	filesChanged := 3
	if cand.Base.Version == 1 {
		filesChanged = 5
	}
	breach, err := reserveDurableBudget(root, &budget, b.effectiveLimits(), budgetReservation{Timestamp: b.recordTime(), Key: reservationKey, Action: "promote", FilesChanged: filesChanged})
	if err != nil {
		return L1Outcome{}, err
	}
	if breach != "" {
		activationFreezeMu.RUnlock()
		activationLocked = false
		if err := guard.Rollback(); err != nil {
			return L1Outcome{}, fmt.Errorf("evolve: release guard before budget freeze: %w", err)
		}
		guardCommitted = true
		if err := b.DurableFreeze.Freeze(ctx, FreezeScopeGlobal, breach); err != nil {
			return L1Outcome{}, fmt.Errorf("evolve: engage durable budget freeze: %w", err)
		}
		MirrorDurableFreeze(breach)
		outcome.Applied = false
		outcome.Record.Stage = StageQuarantined
		outcome.Record.PromotedValue = float64(cand.Base.Version)
		record := b.nonAppliedRecord(cand, outcome, "budget:"+string(breach))
		if err := appendAuthoritativeRecord(root, record); err != nil {
			return L1Outcome{}, err
		}
		return outcome, nil
	}
	if err := b.step("after-budget"); err != nil {
		return L1Outcome{}, err
	}
	if err := b.step("before-version"); err != nil {
		return L1Outcome{}, err
	}
	if cand.Base.Version == 1 {
		if _, _, err := ensureVersionObjects(root, cand.Base, 1); err != nil {
			return L1Outcome{}, err
		}
	}
	if _, _, err := ensureVersionObjects(root, cand.Proposed, nextVersion); err != nil {
		return L1Outcome{}, err
	}
	if err := b.step("after-version"); err != nil {
		return L1Outcome{}, err
	}
	pin := packaging.SkillProfileSource{Source: newSource, SHA256: contentDigest, AuthoritySHA256: authorityDigest}
	catalogs.Skills.Skills[skillIndex].ProfileSources = &packaging.SkillProfileSources{PersonalAutonomousVenture: &pin}
	prepared := b.record("promote", "prepared", cand.Base.SkillID, cand.Profile, StagePromoted, cand.Base.Version, nextVersion, newSource, newBytes)
	prepared.AuthorityDigest = authorityDigest
	if err := appendAuthoritativeRecord(root, prepared); err != nil {
		return L1Outcome{}, err
	}
	if err := b.step("before-catalog"); err != nil {
		return L1Outcome{}, err
	}
	if err := validateEvolutionRepository(root, catalogs, enabled, personal, organization); err != nil {
		return L1Outcome{}, fmt.Errorf("evolve: validate activated catalog: %w", err)
	}
	if err := writeEvolutionCatalog(root, catalogs.Skills, originalCatalog); err != nil {
		return L1Outcome{}, err
	}
	activationFreezeMu.RUnlock()
	activationLocked = false
	if err := b.step("after-catalog"); err != nil {
		return L1Outcome{}, fmt.Errorf("evolve: catalog activated; recovery required: %w", err)
	}

	promoted := cloneSkillVersion(cand.Proposed)
	promoted.SkillID = cand.Base.SkillID
	promoted.Version = nextVersion
	registry.versions[cand.Base.SkillID] = append(registry.versions[cand.Base.SkillID], promoted)
	b.Pipeline.Window.Promotions++
	committed := prepared
	committed.State = "committed"
	if err := appendAuthoritativeRecord(root, committed); err != nil {
		return outcome, fmt.Errorf("evolve: promotion activated but commit audit failed: %w", err)
	}
	if err := guard.Commit(); err != nil {
		return outcome, fmt.Errorf("evolve: promotion activated but thawed guard commit failed: %w", err)
	}
	guardCommitted = true
	return outcome, nil
}

// Rollback appends a new immutable version containing the immediately previous
// materialization bytes and atomically makes it the personal current source.
// It reconstructs its state from disk so the command works after restart.
func (b *SkillPackageBridge) Rollback(ctx context.Context, skillID string) (result SkillPackageRecord, returnErr error) {
	if !skillPackageName.MatchString(skillID) {
		return SkillPackageRecord{}, fmt.Errorf("evolve: invalid skill id %q", skillID)
	}
	root, err := openEvolutionRoot(b.Root)
	if err != nil {
		return SkillPackageRecord{}, err
	}
	defer func() { _ = root.Close() }()
	unlock, err := lockEvolution(root)
	if err != nil {
		return SkillPackageRecord{}, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return SkillPackageRecord{}, err
	}
	if err := reconcilePreparedRecords(root, b.record); err != nil {
		return SkillPackageRecord{}, err
	}
	catalogs, enabled, personal, organization, index, originalCatalog, err := loadValidatedEvolutionRepository(root, skillID)
	if err != nil {
		return SkillPackageRecord{}, err
	}
	skill := catalogs.Skills.Skills[index]
	if len(skill.References) != 0 {
		return SkillPackageRecord{}, fmt.Errorf("evolve: skill %s has references; rollback requires versioned reference snapshots", skillID)
	}
	if skill.ProfileSources == nil || skill.ProfileSources.PersonalAutonomousVenture == nil {
		return SkillPackageRecord{}, fmt.Errorf("evolve: skill %s has no personal promoted version to roll back", skillID)
	}
	activePin := *skill.ProfileSources.PersonalAutonomousVenture
	currentVersion, err := parseVersionSource(skillID, activePin.Source)
	if err != nil || currentVersion < 2 {
		return SkillPackageRecord{}, fmt.Errorf("evolve: skill %s has no previous promoted version to roll back to", skillID)
	}
	currentVersionValue, _, err := readPinnedVersion(root, skillID, currentVersion, activePin)
	if err != nil {
		return SkillPackageRecord{}, fmt.Errorf("evolve: verify current rollback source: %w", err)
	}
	previousVersion, previousAuthorityDigest, err := readVersionObjects(root, skillID, currentVersion-1)
	if err != nil {
		return SkillPackageRecord{}, fmt.Errorf("evolve: read rollback source: %w", err)
	}
	previousBytes := []byte(previousVersion.Prompt)
	var registryPrevious *SkillVersion
	if b.Pipeline.Registry != nil {
		registry := b.Pipeline.Registry
		registry.mu.Lock()
		defer registry.mu.Unlock()
		history := registry.versions[skillID]
		if len(history) < 2 || history[len(history)-1].Version != currentVersion || history[len(history)-2].Version != currentVersion-1 ||
			!reflect.DeepEqual(cloneSkillVersion(history[len(history)-1]), currentVersionValue) || !reflect.DeepEqual(cloneSkillVersion(history[len(history)-2]), previousVersion) {
			return SkillPackageRecord{}, fmt.Errorf("evolve: registry history is not coherent with durable rollback versions")
		}
		version := cloneSkillVersion(history[len(history)-2])
		registryPrevious = &version
	}
	nextVersion := currentVersion + 1
	nextSource := versionSource(skillID, nextVersion)
	_, _, contentDigest, authorityDigest, err := encodeVersionObjects(previousVersion, nextVersion)
	if err != nil {
		return SkillPackageRecord{}, err
	}
	budget, err := loadDurableBudget(root, b.recordTime())
	if err != nil {
		return SkillPackageRecord{}, err
	}
	reservationKey := operationKey("rollback", skillID, currentVersion, nextVersion, contentDigest, authorityDigest)
	breach, err := reserveDurableBudget(root, &budget, b.effectiveLimits(), budgetReservation{Timestamp: b.recordTime(), Key: reservationKey, Action: "rollback", FilesChanged: 3})
	if err != nil {
		return SkillPackageRecord{}, err
	}
	if breach != "" {
		// Rollback is the safety brake: retain and audit the over-budget rollback,
		// while durably freezing subsequent promotion lanes. A rollback caller
		// without a durable gate cannot safely cross this boundary: fail before
		// writing the rollback version rather than leaving only a process-local
		// mirror behind.
		if b.DurableFreeze == nil {
			return SkillPackageRecord{}, fmt.Errorf("evolve: durable freeze store is required for over-budget rollback")
		}
		if err := b.DurableFreeze.Freeze(ctx, FreezeScopeGlobal, breach); err != nil {
			return SkillPackageRecord{}, fmt.Errorf("evolve: freeze promotions after rollback budget breach: %w", err)
		}
		MirrorDurableFreeze(breach)
	}
	if err := b.step("after-budget"); err != nil {
		return SkillPackageRecord{}, err
	}
	if err := b.step("before-version"); err != nil {
		return SkillPackageRecord{}, err
	}
	if _, _, err := ensureVersionObjects(root, previousVersion, nextVersion); err != nil {
		return SkillPackageRecord{}, err
	}
	if err := b.step("after-version"); err != nil {
		return SkillPackageRecord{}, err
	}
	pin := packaging.SkillProfileSource{Source: nextSource, SHA256: contentDigest, AuthoritySHA256: authorityDigest}
	catalogs.Skills.Skills[index].ProfileSources.PersonalAutonomousVenture = &pin
	prepared := b.record("rollback", "prepared", skillID, personalEvolutionProfile, StageReverted, currentVersion, nextVersion, nextSource, previousBytes)
	prepared.AuthorityDigest = authorityDigest
	if previousAuthorityDigest == "" {
		return SkillPackageRecord{}, fmt.Errorf("evolve: previous authority digest is empty")
	}
	if err := appendAuthoritativeRecord(root, prepared); err != nil {
		return SkillPackageRecord{}, err
	}
	if err := b.step("before-catalog"); err != nil {
		return SkillPackageRecord{}, err
	}
	if err := validateEvolutionRepository(root, catalogs, enabled, personal, organization); err != nil {
		return SkillPackageRecord{}, fmt.Errorf("evolve: validate rollback catalog: %w", err)
	}
	if err := writeEvolutionCatalog(root, catalogs.Skills, originalCatalog); err != nil {
		return SkillPackageRecord{}, err
	}
	if err := b.step("after-catalog"); err != nil {
		return SkillPackageRecord{}, fmt.Errorf("evolve: rollback catalog activated; recovery required: %w", err)
	}
	if registryPrevious != nil {
		registryPrevious.Version = nextVersion
		b.Pipeline.Registry.versions[skillID] = append(b.Pipeline.Registry.versions[skillID], cloneSkillVersion(*registryPrevious))
	}
	prepared.State = "committed"
	if err := appendAuthoritativeRecord(root, prepared); err != nil {
		return prepared, fmt.Errorf("evolve: rollback activated but commit audit failed: %w", err)
	}
	return prepared, nil
}

func validateCandidateIdentity(cand L1Candidate) error {
	if !skillPackageName.MatchString(cand.Base.SkillID) || cand.Base.SkillID != cand.Proposed.SkillID {
		return fmt.Errorf("evolve: base and proposed skill ids must match one valid catalog package")
	}
	if cand.Base.Version < 1 {
		return fmt.Errorf("evolve: candidate base version must be positive")
	}
	if cand.Profile != personalEvolutionProfile && cand.Profile != "organization" {
		return fmt.Errorf("evolve: candidate profile must be personal or organization")
	}
	for name, version := range map[string]SkillVersion{"base": cand.Base, "proposed": cand.Proposed} {
		if version.Permissions == nil || version.DataClasses == nil || version.BudgetUSD < 0 || math.IsNaN(version.BudgetUSD) || math.IsInf(version.BudgetUSD, 0) {
			return fmt.Errorf("evolve: candidate %s authority metadata is invalid", name)
		}
	}
	return nil
}

func cloneRegistryLocked(registry *SkillRegistry) *SkillRegistry {
	copyRegistry := NewSkillRegistry()
	for skillID, history := range registry.versions {
		copyHistory := make([]SkillVersion, len(history))
		for i := range history {
			copyHistory[i] = cloneSkillVersion(history[i])
		}
		copyRegistry.versions[skillID] = copyHistory
	}
	return copyRegistry
}

func (b *SkillPackageBridge) record(action, state, skillID, profile string, stage PromotionStage, from, to int, source string, content []byte) SkillPackageRecord {
	createdAt := b.recordTime()
	record := SkillPackageRecord{Action: action, State: state, SkillID: skillID, Profile: profile, Stage: stage, FromVersion: from, ToVersion: to, Source: source, CreatedAt: createdAt}
	if content != nil {
		record.Digest = digestSkillBytes(content)
	}
	return record
}

func (b *SkillPackageBridge) nonAppliedRecord(candidate L1Candidate, outcome L1Outcome, reason string) SkillPackageRecord {
	record := b.record("evaluate", "recorded", candidate.Base.SkillID, candidate.Profile, outcome.Record.Stage, candidate.Base.Version, candidate.Base.Version, "", nil)
	record.ProposedDigest = digestSkillBytes([]byte(candidate.Proposed.Prompt))
	_, _, _, authorityDigest, err := encodeVersionObjects(candidate.Proposed, candidate.Base.Version+1)
	if err == nil {
		record.ProposedAuthorityDigest = authorityDigest
	}
	record.Reason = boundedAuditReason(reason)
	return record
}

func boundedAuditReason(reason string) string {
	if auditReasonPattern.MatchString(reason) {
		return reason
	}
	return "unclassified"
}

func (b *SkillPackageBridge) recordTime() time.Time {
	if b != nil && b.now != nil {
		return b.now().UTC()
	}
	if b != nil && b.Pipeline.Now != nil {
		return b.Pipeline.Now().UTC()
	}
	return time.Now().UTC()
}

func (b *SkillPackageBridge) effectiveLimits() ChangeBudgetLimits {
	limits := b.Pipeline.Limits
	if limits.MaxPromotions == 0 && limits.MaxFilesChanged == 0 && limits.MaxRollbackDepth == 0 {
		return DefaultChangeBudgetLimits()
	}
	return limits
}

func (b *SkillPackageBridge) step(name string) error {
	if b.beforeStep == nil {
		return nil
	}
	if err := b.beforeStep(name); err != nil {
		return fmt.Errorf("evolve: %s: %w", name, err)
	}
	return nil
}

func openEvolutionRoot(path string) (*os.Root, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("evolve: resolve root: %w", err)
	}
	before, err := os.Lstat(abs)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("evolve: root must be a real directory")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("evolve: open root: %w", err)
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = root.Close()
		return nil, fmt.Errorf("evolve: root changed while opening")
	}
	if err := ensureDirectories(root, evolutionDirectory); err != nil {
		_ = root.Close()
		return nil, err
	}
	return root, nil
}

func lockEvolution(root *os.Root) (func(), error) {
	if info, err := root.Lstat(evolutionLock); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("evolve: catalog lock is a symlink or non-regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("evolve: inspect catalog lock: %w", err)
	}
	lock, err := root.OpenFile(evolutionLock, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("evolve: open catalog lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("evolve: lock catalog: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
}

func loadValidatedEvolutionRepository(root *os.Root, skillID string) (packaging.Catalogs, packaging.Enablement, packaging.ProfileEnablement, packaging.ProfileEnablement, int, []byte, error) {
	catalogs, err := packaging.LoadCatalogsFromRoot(root)
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, -1, nil, fmt.Errorf("evolve: load catalogs: %w", err)
	}
	enabled, err := packaging.LoadEnablementFromRoot(root, "templates/product/.foundry/skills/enabled.yaml")
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, -1, nil, err
	}
	personal, err := packaging.LoadProfileEnablementFromRoot(root, "config/profiles/personal-autonomous-venture.yaml")
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, -1, nil, err
	}
	organization, err := packaging.LoadProfileEnablementFromRoot(root, "config/profiles/organization-10x.yaml")
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, -1, nil, err
	}
	if err := validateEvolutionRepository(root, catalogs, enabled, personal, organization); err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, -1, nil, err
	}
	raw, err := readRegular(root, packaging.SkillCatalogPath)
	if err != nil {
		return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, -1, nil, err
	}
	for i := range catalogs.Skills.Skills {
		if catalogs.Skills.Skills[i].Name == skillID {
			if !containsString(enabled.Skills, skillID) || !containsString(personal.SkillPackages.Enabled, skillID) {
				return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, -1, nil, fmt.Errorf("evolve: core skill %s is not enabled for personal evolution", skillID)
			}
			return catalogs, enabled, personal, organization, i, raw, nil
		}
	}
	return packaging.Catalogs{}, packaging.Enablement{}, packaging.ProfileEnablement{}, packaging.ProfileEnablement{}, -1, nil, fmt.Errorf("evolve: core skill %s is not cataloged", skillID)
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateEvolutionRepository(root *os.Root, catalogs packaging.Catalogs, enabled packaging.Enablement, personal, organization packaging.ProfileEnablement) error {
	if err := packaging.ValidateCatalogsFromRoot(root, catalogs); err != nil {
		return err
	}
	if err := packaging.ValidateEnablement(catalogs, enabled); err != nil {
		return err
	}
	if err := packaging.ValidateProfiles(catalogs, personal, organization); err != nil {
		return err
	}
	return nil
}

func writeEvolutionCatalog(root *os.Root, catalog packaging.SkillCatalog, expectedCurrent []byte) error {
	raw, err := yaml.Marshal(catalog)
	if err != nil {
		return fmt.Errorf("evolve: encode skill catalog: %w", err)
	}
	file, temp, err := createEvolutionTemp(root)
	if err != nil {
		return fmt.Errorf("evolve: create catalog temp: %w", err)
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = root.Remove(temp)
		}
	}()
	if _, err := file.Write(raw); err != nil {
		return fmt.Errorf("evolve: write catalog temp: %w", err)
	}
	info, err := root.Lstat(packaging.SkillCatalogPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("evolve: skill catalog destination is not a regular file")
	}
	current, err := readRegular(root, packaging.SkillCatalogPath)
	if err != nil || !bytes.Equal(current, expectedCurrent) {
		return fmt.Errorf("evolve: skill catalog changed before activation")
	}
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("evolve: preserve skill catalog mode: %w", err)
	}
	// Sync after chmod so both catalog contents and preserved metadata are
	// durable before the atomic rename.
	if err := file.Sync(); err != nil {
		return fmt.Errorf("evolve: sync catalog temp: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("evolve: close catalog temp: %w", err)
	}
	if err := root.Rename(temp, packaging.SkillCatalogPath); err != nil {
		return fmt.Errorf("evolve: activate skill catalog: %w", err)
	}
	cleanup = false
	directory, err := root.Open("skills")
	if err != nil {
		return fmt.Errorf("evolve: open catalog directory: %w", err)
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("evolve: sync catalog directory: %w", err)
	}
	return nil
}

func createEvolutionTemp(root *os.Root) (*os.File, string, error) {
	for range 100 {
		var suffix [16]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return nil, "", fmt.Errorf("evolve: random catalog temp: %w", err)
		}
		name := evolutionDirectory + "/catalog-" + hex.EncodeToString(suffix[:]) + ".tmp"
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
		if err == nil {
			return file, name, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", fmt.Errorf("evolve: create catalog temp: %w", err)
		}
	}
	return nil, "", fmt.Errorf("evolve: catalog temp collisions exhausted")
}

func operationKey(action, skillID string, from, to int, digest, authorityDigest string) string {
	raw := strings.Join([]string{action, skillID, strconv.Itoa(from), strconv.Itoa(to), digest, authorityDigest}, "\x00")
	return digestSkillBytes([]byte(raw))
}

func ensureDirectories(root *os.Root, relative string) error {
	if err := validateRelativePath(relative); err != nil {
		return err
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(relative)), "/")
	for i := range parts {
		current := strings.Join(parts[:i+1], "/")
		info, err := root.Lstat(current)
		created := false
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("evolve: create directory %s: %w", current, err)
			}
			created = true
			info, err = root.Lstat(current)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("evolve: directory %s is not a real directory", current)
		}
		if created {
			parent := filepath.ToSlash(filepath.Dir(current))
			if err := syncDirectory(root, parent); err != nil {
				return fmt.Errorf("evolve: persist directory %s: %w", current, err)
			}
		}
	}
	return nil
}

func openRealDirectory(root *os.Root, relative string) (*os.File, error) {
	before, err := root.Lstat(relative)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("evolve: directory %s is not a real directory", relative)
	}
	directory, err := root.Open(relative)
	if err != nil {
		return nil, fmt.Errorf("evolve: open directory %s: %w", relative, err)
	}
	after, err := directory.Stat()
	if err != nil || !os.SameFile(before, after) {
		_ = directory.Close()
		return nil, fmt.Errorf("evolve: directory %s changed while opening", relative)
	}
	return directory, nil
}

func readRegular(root *os.Root, relative string) ([]byte, error) {
	return readRegularWithHook(root, relative, nil)
}

type evolutionReadHook func(stage, path string) error

func readRegularWithHook(root *os.Root, relative string, hook evolutionReadHook) ([]byte, error) {
	if err := validateRelativePath(relative); err != nil {
		return nil, err
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(relative)), "/")
	current := root
	owned := false
	defer func() {
		if owned {
			_ = current.Close()
		}
	}()
	for index, part := range parts[:len(parts)-1] {
		path := strings.Join(parts[:index+1], "/")
		before, err := current.Lstat(part)
		if err != nil {
			return nil, err
		}
		if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			return nil, fmt.Errorf("evolve: path %s contains a symlink or non-directory", relative)
		}
		if hook != nil {
			if err := hook("after-parent-lstat", path); err != nil {
				return nil, err
			}
		}
		next, err := current.OpenRoot(part)
		if err != nil {
			return nil, err
		}
		after, err := next.Stat(".")
		if err != nil || !os.SameFile(before, after) {
			_ = next.Close()
			return nil, fmt.Errorf("evolve: path parent %s changed while opening", path)
		}
		if owned {
			_ = current.Close()
		}
		current, owned = next, true
	}
	name := parts[len(parts)-1]
	before, err := current.Lstat(name)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("evolve: path %s is a symlink or non-regular file", relative)
	}
	if hook != nil {
		if err := hook("after-file-lstat", relative); err != nil {
			return nil, err
		}
	}
	file, err := current.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	after, statErr := file.Stat()
	if statErr != nil || !os.SameFile(before, after) {
		_ = file.Close()
		return nil, fmt.Errorf("evolve: path %s changed while opening", relative)
	}
	content, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	return content, nil
}

func validateRelativePath(path string) error {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(path))))
	if clean == "." || clean != path || filepath.IsAbs(filepath.FromSlash(path)) || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("evolve: unsafe noncanonical path %q", path)
	}
	return nil
}

func versionSource(skillID string, version int) string {
	return "skills/" + skillID + "/versions/v" + strconv.Itoa(version) + "/SKILL.md"
}

func parseVersionSource(skillID, source string) (int, error) {
	prefix := "skills/" + skillID + "/versions/v"
	if !strings.HasPrefix(source, prefix) || !strings.HasSuffix(source, "/SKILL.md") {
		return 0, fmt.Errorf("noncanonical version source")
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(source, prefix), "/SKILL.md")
	if raw == "" || (len(raw) > 1 && raw[0] == '0') {
		return 0, fmt.Errorf("noncanonical version")
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version < 1 || versionSource(skillID, version) != source {
		return 0, fmt.Errorf("noncanonical version source")
	}
	return version, nil
}

func digestSkillBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ReadSkillPackageRecords returns the durable audit rows in append order.
func ReadSkillPackageRecords(rootPath string) ([]SkillPackageRecord, error) {
	root, err := openEvolutionRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return readAuthoritativeRecords(root)
}
