package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"filippo.io/age"

	"github.com/okfriansyah-moh/the-foundry/internal/secrets"
)

// DefaultPath returns "~/.foundry/secrets.age", expanded via
// os.UserHomeDir (there is no literal "~" expansion in Go's os package).
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("filestore: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".foundry", "secrets.age"), nil
}

// payload is secrets.age's plaintext shape once decrypted: scope -> name
// -> value. The profile-bound scope model (internal/secrets's doc.go)
// means the same name can hold a different value per scope.
type payload map[string]map[string]string

// Store is the age-encrypted file implementation of secrets.Store.
// Construct with New; the zero value is not usable (Path/Keys unset).
type Store struct {
	// Path is the secrets.age file location. Use DefaultPath() unless a
	// test or CI override is needed.
	Path string
	// Keys resolves the identity/recipient used to decrypt/encrypt Path.
	Keys KeySource
	// Logger receives one audit record per Get call. Defaults to
	// slog.Default() when nil.
	Logger *slog.Logger

	mu sync.Mutex
}

var _ secrets.Store = (*Store)(nil)

// New constructs a Store at path using keys for encryption/decryption.
func New(path string, keys KeySource) *Store {
	return &Store{Path: path, Keys: keys}
}

func (s *Store) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// load reads and decrypts Path, returning an empty payload (not an error)
// when Path does not exist yet — a Store nothing has ever written to
// behaves as an empty secret set.
func (s *Store) load(ctx context.Context) (payload, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return payload{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("filestore: open %s: %w", s.Path, err)
	}
	defer f.Close()

	id, err := s.Keys.Identity(ctx)
	if err != nil {
		return nil, fmt.Errorf("filestore: resolve decryption key: %w", err)
	}

	r, err := age.Decrypt(f, id)
	if err != nil {
		return nil, fmt.Errorf("filestore: decrypt %s: %w", s.Path, err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("filestore: read decrypted %s: %w", s.Path, err)
	}

	var p payload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("filestore: parse %s: %w", s.Path, err)
	}
	if p == nil {
		p = payload{}
	}
	return p, nil
}

// save encrypts and atomically writes p to Path (write-temp-then-rename,
// so a crash mid-write never leaves a truncated or half-encrypted file in
// Path's place).
func (s *Store) save(ctx context.Context, p payload) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("filestore: marshal secrets: %w", err)
	}

	recipient, err := s.Keys.Recipient(ctx)
	if err != nil {
		return fmt.Errorf("filestore: resolve encryption key: %w", err)
	}

	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("filestore: create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".secrets-*.age.tmp")
	if err != nil {
		return fmt.Errorf("filestore: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("filestore: chmod temp file: %w", err)
	}

	w, err := age.Encrypt(tmp, recipient)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("filestore: start encryption: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("filestore: write encrypted secrets: %w", err)
	}
	if err := w.Close(); err != nil {
		tmp.Close()
		return fmt.Errorf("filestore: finalize encryption: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("filestore: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.Path); err != nil {
		return fmt.Errorf("filestore: replace %s: %w", s.Path, err)
	}
	return nil
}

// Get implements secrets.Store. Every call is audited (scope, name,
// found) via slog before returning — the value itself is never logged.
func (s *Store) Get(ctx context.Context, scope, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.load(ctx)
	if err != nil {
		s.audit(scope, name, false, err)
		return "", err
	}

	names, ok := p[scope]
	if ok {
		if v, ok := names[name]; ok {
			s.audit(scope, name, true, nil)
			return v, nil
		}
	}
	notFound := fmt.Errorf("filestore: scope %q name %q: %w", scope, name, secrets.ErrNotFound)
	s.audit(scope, name, false, notFound)
	return "", notFound
}

// Set provisions (or overwrites) one secret. It is deliberately not part
// of the secrets.Store interface consuming packages depend on — writing a
// plaintext value into the store is a trusted provisioning action (an
// operator CLI, a bootstrap script, a test setup), not something every
// Get-only caller should be able to trigger.
func (s *Store) Set(ctx context.Context, scope, name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.load(ctx)
	if err != nil {
		return err
	}
	if p[scope] == nil {
		p[scope] = map[string]string{}
	}
	p[scope][name] = value
	return s.save(ctx, p)
}

// audit logs one read attempt. err is included only when it is not
// secrets.ErrNotFound (a routine, expected outcome, not a fault worth
// surfacing at the same level as a real backend error) — and even then
// only via err.Error(), which no code path in this package ever derives
// from a secret's plaintext value.
func (s *Store) audit(scope, name string, found bool, err error) {
	attrs := []any{"scope", scope, "name", name, "found", found}
	if err != nil && !errors.Is(err, secrets.ErrNotFound) {
		attrs = append(attrs, "error", err.Error())
	}
	s.logger().Info("secrets: read", attrs...)
}
