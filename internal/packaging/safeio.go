package packaging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type canonicalReadHook func(stage, path string) error

func readCanonicalFileWithHook(rootPath, reference string, hook canonicalReadHook) ([]byte, error) {
	root, err := openCanonicalRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	content, _, err := readCanonicalFileInfoFromRoot(root, reference, hook)
	return content, err
}

func readCanonicalFileFromRoot(root *os.Root, reference string) ([]byte, error) {
	content, _, err := readCanonicalFileInfoFromRoot(root, reference, nil)
	return content, err
}

func readCanonicalFileInfoFromRoot(root *os.Root, reference string, hook canonicalReadHook) ([]byte, os.FileInfo, error) {
	if root == nil {
		return nil, nil, fmt.Errorf("catalog root is required")
	}
	parts, err := canonicalReferenceParts(reference)
	if err != nil {
		return nil, nil, err
	}
	return readCanonicalFromRoot(root, parts, hook)
}

func openCanonicalRoot(path string) (*os.Root, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve catalog root: %w", err)
	}
	before, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("inspect catalog root: %w", err)
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("catalog root must be a real directory")
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("open catalog root: %w", err)
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = root.Close()
		return nil, fmt.Errorf("catalog root changed while opening")
	}
	return root, nil
}

func canonicalReferenceParts(reference string) ([]string, error) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(reference))))
	if clean == "." || clean != reference || filepath.IsAbs(filepath.FromSlash(reference)) || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("reference %q escapes catalog root or is noncanonical", reference)
	}
	return strings.Split(clean, "/"), nil
}

func readCanonicalFromRoot(root *os.Root, parts []string, hook canonicalReadHook) ([]byte, os.FileInfo, error) {
	current := root
	currentOwned := false
	closeCurrent := func() {
		if currentOwned {
			_ = current.Close()
		}
	}
	defer closeCurrent()

	for index, part := range parts[:len(parts)-1] {
		path := strings.Join(parts[:index+1], "/")
		before, err := current.Lstat(part)
		if err != nil {
			return nil, nil, fmt.Errorf("inspect reference parent %q: %w", path, err)
		}
		if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			return nil, nil, fmt.Errorf("reference %q contains a symlink or non-directory", strings.Join(parts, "/"))
		}
		if hook != nil {
			if err := hook("after-parent-lstat", path); err != nil {
				return nil, nil, err
			}
		}
		next, err := current.OpenRoot(part)
		if err != nil {
			return nil, nil, fmt.Errorf("open reference parent %q: %w", path, err)
		}
		after, err := next.Stat(".")
		if err != nil || !os.SameFile(before, after) {
			_ = next.Close()
			return nil, nil, fmt.Errorf("reference parent %q changed while opening", path)
		}
		if currentOwned {
			_ = current.Close()
		}
		current, currentOwned = next, true
	}

	name := parts[len(parts)-1]
	reference := strings.Join(parts, "/")
	before, err := current.Lstat(name)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect reference %q: %w", reference, err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("reference %q is a symlink or non-regular file", reference)
	}
	if hook != nil {
		if err := hook("after-file-lstat", reference); err != nil {
			return nil, nil, err
		}
	}
	file, err := current.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open reference %q: %w", reference, err)
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, nil, fmt.Errorf("reference %q changed while opening", reference)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, fmt.Errorf("read reference %q: %w", reference, err)
	}
	return content, after, nil
}
