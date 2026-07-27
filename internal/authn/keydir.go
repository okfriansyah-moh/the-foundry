package authn

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

const (
	sessionKeyDirPerm  os.FileMode = 0o700
	sessionKeyFilePerm os.FileMode = 0o600
)

// DefaultSessionKeyDir returns ~/.foundry/keys — the same directory
// internal/provenance's approver key lives in (docs/PLAN.md Task 8).
// `foundry login` and whatever process runs ApproveHandler both need to
// agree on where the session-signing key pair lives.
func DefaultSessionKeyDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("authn: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".foundry", "keys"), nil
}

// LoadOrGenerateSessionKey reads dir/session.key (a PEM-encoded EC
// private key); if it doesn't exist yet, it generates a new P-256 key
// pair and writes it there (0600, dir itself 0700) before returning it.
func LoadOrGenerateSessionKey(dir string) (*ecdsa.PrivateKey, error) {
	path := filepath.Join(dir, "session.key")

	if buf, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(buf)
		if block == nil {
			return nil, fmt.Errorf("authn: %s is not valid PEM", path)
		}
		priv, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("authn: parse session key %s: %w", path, err)
		}
		return priv, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("authn: read session key %s: %w", path, err)
	}

	priv, err := GenerateSessionKey()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, sessionKeyDirPerm); err != nil {
		return nil, fmt.Errorf("authn: create key dir %s: %w", dir, err)
	}
	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("authn: marshal session key: %w", err)
	}
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), sessionKeyFilePerm); err != nil {
		return nil, fmt.Errorf("authn: write session key %s: %w", path, err)
	}
	return priv, nil
}
