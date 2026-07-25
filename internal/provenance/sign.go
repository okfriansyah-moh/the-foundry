package provenance

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// keyDirPerm and keyFilePerm are the fixed permissions for
// ~/.foundry/keys and the key files inside it (docs/PLAN.md Task 8 Step 2:
// "0600 perms"). Never widened, never taken from config.
const (
	keyDirPerm  os.FileMode = 0o700
	keyFilePerm os.FileMode = 0o600
)

// KeyPair is a local Ed25519 approver identity.
type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

// DefaultKeyDir returns ~/.foundry/keys, the default location foundry
// keygen writes to and plan approve/verify read from.
func DefaultKeyDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("provenance: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".foundry", "keys"), nil
}

// GenerateKeyPair creates a new random Ed25519 key pair.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("provenance: generate key pair: %w", err)
	}
	return &KeyPair{Public: pub, Private: priv}, nil
}

// WriteKeyPair writes kp to dir as approver.pub and approver.key (hex
// encoded), both 0600, dir itself 0700. It refuses to overwrite an
// existing key file unless force is true — keygen should not silently
// clobber an approver identity other artifacts may already be signed
// against.
func WriteKeyPair(dir string, kp *KeyPair, force bool) error {
	if err := os.MkdirAll(dir, keyDirPerm); err != nil {
		return fmt.Errorf("provenance: create key dir %s: %w", dir, err)
	}

	pubPath := filepath.Join(dir, "approver.pub")
	keyPath := filepath.Join(dir, "approver.key")

	if !force {
		for _, p := range []string{pubPath, keyPath} {
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("provenance: %s already exists, refusing to overwrite (use --force)", p)
			}
		}
	}

	if err := os.WriteFile(pubPath, []byte(hex.EncodeToString(kp.Public)), keyFilePerm); err != nil {
		return fmt.Errorf("provenance: write %s: %w", pubPath, err)
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(kp.Private)), keyFilePerm); err != nil {
		return fmt.Errorf("provenance: write %s: %w", keyPath, err)
	}
	return nil
}

// LoadKeyPair reads an approver key pair back from dir.
func LoadKeyPair(dir string) (*KeyPair, error) {
	pubHex, err := os.ReadFile(filepath.Join(dir, "approver.pub"))
	if err != nil {
		return nil, fmt.Errorf("provenance: read approver.pub: %w", err)
	}
	keyHex, err := os.ReadFile(filepath.Join(dir, "approver.key"))
	if err != nil {
		return nil, fmt.Errorf("provenance: read approver.key: %w", err)
	}
	pub, err := hex.DecodeString(string(pubHex))
	if err != nil {
		return nil, fmt.Errorf("provenance: decode approver.pub: %w", err)
	}
	priv, err := hex.DecodeString(string(keyHex))
	if err != nil {
		return nil, fmt.Errorf("provenance: decode approver.key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize || len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("provenance: key file size mismatch in %s", dir)
	}
	return &KeyPair{Public: ed25519.PublicKey(pub), Private: ed25519.PrivateKey(priv)}, nil
}

// LoadPublicKey reads just the public half from dir, for verify-only
// callers (e.g. the kernel-facing Store, which never holds the private
// key).
func LoadPublicKey(dir string) (ed25519.PublicKey, error) {
	pubHex, err := os.ReadFile(filepath.Join(dir, "approver.pub"))
	if err != nil {
		return nil, fmt.Errorf("provenance: read approver.pub: %w", err)
	}
	pub, err := hex.DecodeString(string(pubHex))
	if err != nil {
		return nil, fmt.Errorf("provenance: decode approver.pub: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("provenance: approver.pub size mismatch in %s", dir)
	}
	return ed25519.PublicKey(pub), nil
}

// SigningPayload returns the canonical bytes Sign and Verify operate on:
// the JSON encoding of every ApprovedPlan field except Signature itself
// (docs/PLAN.md Task 8 Step 2). encoding/json emits struct fields in fixed
// declaration order, and approvedPlanWire contains no maps, so the same
// field values always marshal to byte-identical output.
func SigningPayload(a *ApprovedPlan) ([]byte, error) {
	w := a.toWire()
	payload, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("provenance: marshal signing payload: %w", err)
	}
	return payload, nil
}

// Sign computes SigningPayload(a) and stores the Ed25519 signature on a.
// a must not already be signed with a different key expectation — callers
// sign once, at approval time.
func Sign(priv ed25519.PrivateKey, a *ApprovedPlan) error {
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("provenance: invalid private key size")
	}
	payload, err := SigningPayload(a)
	if err != nil {
		return err
	}
	a.signature = ed25519.Sign(priv, payload)
	return nil
}

// Verify recomputes SigningPayload(a) and checks it against a's stored
// signature under pub. A missing signature, a wrong-size signature, or a
// mismatched payload (i.e. any field of a was changed after signing) all
// fail closed with a non-nil error — this is the single choke point every
// tamper-detection path (file, DB row, forged insert) routes through.
func Verify(pub ed25519.PublicKey, a *ApprovedPlan) error {
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("provenance: invalid public key size")
	}
	if len(a.signature) == 0 {
		return fmt.Errorf("provenance: ApprovedPlan %s has no signature", a.planID)
	}
	payload, err := SigningPayload(a)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, a.signature) {
		return fmt.Errorf("provenance: signature verification failed for ApprovedPlan %s", a.planID)
	}
	return nil
}

func encodeSignature(sig []byte) string {
	return base64.StdEncoding.EncodeToString(sig)
}

func decodeSignature(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	sig, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("provenance: decode signature: %w", err)
	}
	return sig, nil
}
