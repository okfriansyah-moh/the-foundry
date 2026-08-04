package evolve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/packaging"
)

func appendEvolutionRecord(root *os.Root, record SkillPackageRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("evolve: encode promotion record: %w", err)
	}
	raw = append(raw, '\n')
	if info, err := root.Lstat(evolutionJournal); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("evolve: promotion journal is a symlink or non-regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("evolve: inspect promotion journal: %w", err)
	}
	file, err := root.OpenFile(evolutionJournal, os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("evolve: open promotion journal: %w", err)
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return fmt.Errorf("evolve: append promotion journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("evolve: sync promotion journal: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("evolve: close promotion journal: %w", err)
	}
	return nil
}

func appendAuthoritativeRecord(root *os.Root, record SkillPackageRecord) error {
	if err := ensureDirectories(root, evolutionRecords); err != nil {
		return err
	}
	records, err := readAuthoritativeRecords(root)
	if err != nil {
		return err
	}
	record.Sequence = 1
	if len(records) > 0 {
		record.Sequence = records[len(records)-1].Sequence + 1
	}
	if err := validateSkillPackageRecord(record); err != nil {
		return fmt.Errorf("evolve: invalid authoritative record: %w", err)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("evolve: encode authoritative record: %w", err)
	}
	raw = append(raw, '\n')
	path := filepath.ToSlash(filepath.Join(evolutionRecords, fmt.Sprintf("%020d.json", record.Sequence)))
	if err := writeImmutable(root, path, raw); err != nil {
		return fmt.Errorf("evolve: persist authoritative record: %w", err)
	}
	// JSONL is a derived human/evidence convenience. The exclusive, fsynced
	// per-record files above are authoritative, so a torn JSONL tail cannot
	// erase or fabricate promotion state.
	_ = appendEvolutionRecord(root, record)
	return nil
}

func readAuthoritativeRecords(root *os.Root) ([]SkillPackageRecord, error) {
	if info, err := root.Lstat(evolutionRecords); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("evolve: authoritative records path is a symlink or non-directory")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("evolve: inspect authoritative records: %w", err)
	}
	entries, err := openRealDirectory(root, evolutionRecords)
	if err != nil {
		return nil, fmt.Errorf("evolve: open authoritative records: %w", err)
	}
	defer func() { _ = entries.Close() }()
	names, err := entries.Readdirnames(-1)
	if err != nil {
		return nil, fmt.Errorf("evolve: list authoritative records: %w", err)
	}
	sort.Strings(names)
	records := make([]SkillPackageRecord, 0, len(names))
	for index, name := range names {
		want := fmt.Sprintf("%020d.json", index+1)
		if name != want {
			return nil, fmt.Errorf("evolve: non-contiguous authoritative record %q, want %q", name, want)
		}
		raw, err := readImmutableObject(root, filepath.ToSlash(filepath.Join(evolutionRecords, name)))
		if err != nil {
			return nil, err
		}
		var record SkillPackageRecord
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("evolve: decode authoritative record %s: %w", name, err)
		}
		if record.Sequence != index+1 {
			return nil, fmt.Errorf("evolve: authoritative record sequence mismatch")
		}
		if err := validateSkillPackageRecord(record); err != nil {
			return nil, fmt.Errorf("evolve: invalid authoritative record %s: %w", name, err)
		}
		canonical, _ := json.Marshal(record)
		canonical = append(canonical, '\n')
		if !bytes.Equal(canonical, raw) {
			return nil, fmt.Errorf("evolve: authoritative record %s is noncanonical", name)
		}
		records = append(records, record)
	}
	return records, nil
}

func validateSkillPackageRecord(record SkillPackageRecord) error {
	if record.Sequence < 1 || !skillPackageName.MatchString(record.SkillID) {
		return fmt.Errorf("invalid sequence or skill id")
	}
	if record.Profile != personalEvolutionProfile && record.Profile != "organization" {
		return fmt.Errorf("invalid profile")
	}
	if record.CreatedAt.IsZero() || record.CreatedAt.Location() != time.UTC {
		return fmt.Errorf("invalid creation timestamp")
	}
	if record.FromVersion < 1 || record.ToVersion < 1 {
		return fmt.Errorf("invalid version range")
	}
	if record.Reason != "" && !auditReasonPattern.MatchString(record.Reason) {
		return fmt.Errorf("invalid bounded reason")
	}
	validStage := map[PromotionStage]bool{
		StageRejected: true, StageQuarantined: true, StageReverted: true,
		StageProposed: true, StagePromoted: true,
	}
	if !validStage[record.Stage] {
		return fmt.Errorf("invalid stage")
	}
	validDigest := func(value string) bool { return skillPackageDigestPattern.MatchString(value) }
	switch record.Action {
	case "evaluate":
		if record.State != "recorded" || record.FromVersion != record.ToVersion || record.Source != "" || record.Digest != "" || record.AuthorityDigest != "" || record.Reason == "" || !validDigest(record.ProposedDigest) || !validDigest(record.ProposedAuthorityDigest) {
			return fmt.Errorf("invalid evaluation record")
		}
	case "promote", "rollback":
		if record.State != "prepared" && record.State != "committed" && record.State != "recovered-committed" && record.State != "aborted" {
			return fmt.Errorf("invalid activation state")
		}
		if record.ToVersion != record.FromVersion+1 || record.Source == "" || !validDigest(record.Digest) || !validDigest(record.AuthorityDigest) || record.ProposedDigest != "" || record.ProposedAuthorityDigest != "" || record.Reason != "" {
			return fmt.Errorf("invalid activation record")
		}
		if record.Action == "promote" && record.Stage != StagePromoted {
			return fmt.Errorf("invalid promotion stage")
		}
		if record.Action == "rollback" && (record.Stage != StageReverted || record.Profile != personalEvolutionProfile) {
			return fmt.Errorf("invalid rollback record")
		}
		version, err := parseVersionSource(record.SkillID, record.Source)
		if err != nil || version != record.ToVersion {
			return fmt.Errorf("invalid activation source")
		}
	default:
		return fmt.Errorf("invalid action")
	}
	return nil
}

type packageRecordFactory func(string, string, string, string, PromotionStage, int, int, string, []byte) SkillPackageRecord

func reconcilePreparedRecords(root *os.Root, factory packageRecordFactory) error {
	records, err := readAuthoritativeRecords(root)
	if err != nil {
		return err
	}
	terminal := make(map[string]struct{})
	for _, record := range records {
		if record.State != "prepared" {
			terminal[recordIdentity(record)] = struct{}{}
		}
	}
	for _, prepared := range records {
		if prepared.State != "prepared" {
			continue
		}
		if _, ok := terminal[recordIdentity(prepared)]; ok {
			continue
		}
		state := "aborted"
		catalogs, err := packaging.LoadCatalogsFromRoot(root)
		if err == nil {
			for _, skill := range catalogs.Skills.Skills {
				if skill.Name != prepared.SkillID || skill.ProfileSources == nil || skill.ProfileSources.PersonalAutonomousVenture == nil {
					continue
				}
				pin := skill.ProfileSources.PersonalAutonomousVenture
				if pin.Source == prepared.Source && pin.SHA256 == prepared.Digest && pin.AuthoritySHA256 == prepared.AuthorityDigest {
					state = "recovered-committed"
				}
			}
		}
		recovered := factory(prepared.Action, state, prepared.SkillID, prepared.Profile, prepared.Stage, prepared.FromVersion, prepared.ToVersion, prepared.Source, nil)
		recovered.Digest = prepared.Digest
		recovered.AuthorityDigest = prepared.AuthorityDigest
		if err := appendAuthoritativeRecord(root, recovered); err != nil {
			return err
		}
	}
	return nil
}

func recordIdentity(record SkillPackageRecord) string {
	return record.Action + "\x00" + record.SkillID + "\x00" + strconv.Itoa(record.ToVersion)
}
