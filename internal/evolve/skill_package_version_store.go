package evolve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"

	"github.com/okfriansyah-moh/the-foundry/internal/packaging"
)

func metadataSource(skillID string, version int) string {
	return "skills/" + skillID + "/versions/v" + strconv.Itoa(version) + "/metadata.json"
}

func ensureVersionObjects(root *os.Root, version SkillVersion, number int) (string, string, error) {
	content, metadataBytes, contentDigest, authorityDigest, err := encodeVersionObjects(version, number)
	if err != nil {
		return "", "", err
	}
	if err := writeImmutable(root, versionSource(version.SkillID, number), content); err != nil {
		return "", "", err
	}
	if err := writeImmutable(root, metadataSource(version.SkillID, number), metadataBytes); err != nil {
		return "", "", err
	}
	versionDirectory := filepath.ToSlash(filepath.Dir(versionSource(version.SkillID, number)))
	if err := syncDirectory(root, versionDirectory); err != nil {
		return "", "", err
	}
	if err := syncDirectory(root, filepath.ToSlash(filepath.Dir(versionDirectory))); err != nil {
		return "", "", err
	}
	return contentDigest, authorityDigest, nil
}

func encodeVersionObjects(version SkillVersion, number int) ([]byte, []byte, string, string, error) {
	version = cloneSkillVersion(version)
	version.Version = number
	content := []byte(version.Prompt)
	if len(content) == 0 {
		return nil, nil, "", "", fmt.Errorf("evolve: version prompt must not be empty")
	}
	contentDigest := digestSkillBytes(content)
	metadata := skillVersionMetadata{
		SkillID: version.SkillID, Version: number, PromptSHA256: contentDigest,
		Permissions: nonNilStrings(version.Permissions), DataClasses: nonNilStrings(version.DataClasses), BudgetUSD: version.BudgetUSD,
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("evolve: encode version metadata: %w", err)
	}
	metadataBytes = append(metadataBytes, '\n')
	authorityDigest := digestSkillBytes(metadataBytes)
	return content, metadataBytes, contentDigest, authorityDigest, nil
}

func readVersionObjects(root *os.Root, skillID string, version int) (SkillVersion, string, error) {
	content, err := readImmutableObject(root, versionSource(skillID, version))
	if err != nil {
		return SkillVersion{}, "", err
	}
	metadataBytes, err := readImmutableObject(root, metadataSource(skillID, version))
	if err != nil {
		return SkillVersion{}, "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(metadataBytes))
	decoder.DisallowUnknownFields()
	var metadata skillVersionMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return SkillVersion{}, "", fmt.Errorf("evolve: decode version metadata: %w", err)
	}
	if decoder.More() {
		return SkillVersion{}, "", fmt.Errorf("evolve: trailing version metadata")
	}
	canonical, err := json.Marshal(metadata)
	if err != nil {
		return SkillVersion{}, "", err
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(canonical, metadataBytes) {
		return SkillVersion{}, "", fmt.Errorf("evolve: version metadata is not canonical")
	}
	if metadata.SkillID != skillID || metadata.Version != version || metadata.PromptSHA256 != digestSkillBytes(content) {
		return SkillVersion{}, "", fmt.Errorf("evolve: version metadata does not bind content")
	}
	return SkillVersion{
		SkillID: skillID, Version: version, Prompt: string(content),
		Permissions: cloneStrings(metadata.Permissions), DataClasses: cloneStrings(metadata.DataClasses), BudgetUSD: metadata.BudgetUSD,
	}, digestSkillBytes(metadataBytes), nil
}

func readPinnedVersion(root *os.Root, skillID string, version int, pin packaging.SkillProfileSource) (SkillVersion, string, error) {
	if pin.Source != versionSource(skillID, version) {
		return SkillVersion{}, "", fmt.Errorf("evolve: catalog source pin is noncanonical")
	}
	value, authorityDigest, err := readVersionObjects(root, skillID, version)
	if err != nil {
		return SkillVersion{}, "", err
	}
	if pin.SHA256 != digestSkillBytes([]byte(value.Prompt)) || pin.AuthoritySHA256 != authorityDigest {
		return SkillVersion{}, "", fmt.Errorf("evolve: catalog content or authority pin mismatch")
	}
	return value, authorityDigest, nil
}

func verifyActiveVersion(root *os.Root, skill packaging.Skill, base SkillVersion, personal bool) ([]byte, int, error) {
	if personal && skill.ProfileSources != nil && skill.ProfileSources.PersonalAutonomousVenture != nil {
		pin := *skill.ProfileSources.PersonalAutonomousVenture
		version, err := parseVersionSource(base.SkillID, pin.Source)
		if err != nil {
			return nil, 0, fmt.Errorf("evolve: invalid personal profile source: %w", err)
		}
		active, _, err := readPinnedVersion(root, base.SkillID, version, pin)
		if err != nil {
			return nil, 0, err
		}
		if !reflect.DeepEqual(active, cloneSkillVersion(base)) {
			return nil, 0, fmt.Errorf("evolve: candidate base authority does not match active metadata")
		}
		return []byte(active.Prompt), version, nil
	}
	if base.Version != 1 {
		return nil, 0, fmt.Errorf("evolve: baseline catalog source only represents v1")
	}
	content, err := readRegular(root, skill.Source)
	if err != nil {
		return nil, 0, fmt.Errorf("evolve: read baseline skill: %w", err)
	}
	if !bytes.Equal(content, []byte(base.Prompt)) {
		return nil, 0, fmt.Errorf("evolve: baseline prompt does not match candidate base")
	}
	return content, 1, nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return cloneStrings(values)
}

func cloneStrings(values []string) []string { return append([]string(nil), values...) }

func writeImmutable(root *os.Root, relative string, content []byte) error {
	if err := validateRelativePath(relative); err != nil {
		return err
	}
	if existing, err := readImmutableObject(root, relative); err == nil {
		if bytes.Equal(existing, content) {
			return nil
		}
		return fmt.Errorf("evolve: immutable version collision at %s", relative)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := ensureDirectories(root, filepath.ToSlash(filepath.Dir(relative))); err != nil {
		return err
	}
	file, err := root.OpenFile(relative, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("evolve: create immutable version %s: %w", relative, err)
	}
	created, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return statErr
	}
	removeOnError := func() {
		if current, err := root.Lstat(relative); err == nil && os.SameFile(current, created) {
			_ = root.Remove(relative)
		}
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		removeOnError()
		return fmt.Errorf("evolve: write immutable version: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		removeOnError()
		return fmt.Errorf("evolve: sync immutable version: %w", err)
	}
	if err := file.Chmod(0o400); err != nil {
		_ = file.Close()
		removeOnError()
		return fmt.Errorf("evolve: make immutable version read-only: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		removeOnError()
		return fmt.Errorf("evolve: sync immutable mode: %w", err)
	}
	if err := file.Close(); err != nil {
		removeOnError()
		return fmt.Errorf("evolve: close immutable version: %w", err)
	}
	return syncDirectory(root, filepath.ToSlash(filepath.Dir(relative)))
}

func readImmutableObject(root *os.Root, relative string) ([]byte, error) {
	content, err := readRegular(root, relative)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(relative)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return nil, fmt.Errorf("evolve: immutable object %s must have exactly one hard link", relative)
	}
	if info.Mode().Perm()&0o222 != 0 {
		return nil, fmt.Errorf("evolve: immutable object %s is writable", relative)
	}
	return content, nil
}

func syncDirectory(root *os.Root, relative string) error {
	directory, err := openRealDirectory(root, relative)
	if err != nil {
		return fmt.Errorf("evolve: open directory %s for sync: %w", relative, err)
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("evolve: sync directory %s: %w", relative, err)
	}
	return nil
}
