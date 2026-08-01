package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type storeHarness struct {
	store          Store
	tamperArtifact func(t *testing.T, id string)
	tamperManifest func(t *testing.T, id string)
}

func TestStoreConformance(t *testing.T) {
	for _, tc := range []struct {
		name    string
		harness func(t *testing.T) storeHarness
	}{
		{name: "filesystem", harness: filesystemHarness},
		{name: "s3", harness: s3Harness},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runStoreConformance(t, tc.harness)
		})
	}
}

func runStoreConformance(t *testing.T, harness func(t *testing.T) storeHarness) {
	t.Run("put get verify", func(t *testing.T) {
		h := harness(t)
		bundle := buildStagedBundle(t, []byte("hello"))
		id, err := h.store.Put(bundle)
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		got, err := h.store.Get(id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Manifest.TaskID != bundle.Manifest.TaskID {
			t.Fatalf("TaskID = %q, want %q", got.Manifest.TaskID, bundle.Manifest.TaskID)
		}
		if err := h.store.Verify(id); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	})

	t.Run("duplicate put errors", func(t *testing.T) {
		h := harness(t)
		bundle := buildStagedBundle(t, []byte("hello"))
		id, err := h.store.Put(bundle)
		if err != nil {
			t.Fatalf("first Put: %v", err)
		}
		if _, err := h.store.Put(buildStagedBundle(t, []byte("hello"))); !errors.Is(err, ErrBundleExists) {
			t.Fatalf("second Put with id %s error = %v, want ErrBundleExists", id, err)
		}
	})

	t.Run("artifact tamper detected", func(t *testing.T) {
		h := harness(t)
		id, err := h.store.Put(buildStagedBundle(t, []byte("hello")))
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		h.tamperArtifact(t, id)
		err = h.store.Verify(id)
		if err == nil {
			t.Fatalf("Verify succeeded after artifact tamper")
		}
		if !containsAll(err.Error(), "output.txt", "sha256 mismatch") {
			t.Fatalf("Verify error = %v, want output.txt sha256 mismatch", err)
		}
	})

	t.Run("manifest tamper detected", func(t *testing.T) {
		h := harness(t)
		id, err := h.store.Put(buildStagedBundle(t, []byte("hello")))
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		h.tamperManifest(t, id)
		if err := h.store.Verify(id); err == nil {
			t.Fatalf("Verify succeeded after manifest tamper")
		}
	})
}

func filesystemHarness(t *testing.T) storeHarness {
	t.Helper()
	root := t.TempDir()
	store := NewFSStore(root)
	return storeHarness{
		store: store,
		tamperArtifact: func(t *testing.T, id string) {
			t.Helper()
			artifactPath := filepath.Join(root, id[:2], id, "artifacts", "output.txt")
			raw, err := os.ReadFile(artifactPath)
			if err != nil {
				t.Fatalf("read stored artifact: %v", err)
			}
			raw[0] ^= 0xFF
			if err := os.WriteFile(artifactPath, raw, 0o644); err != nil {
				t.Fatalf("rewrite tampered artifact: %v", err)
			}
		},
		tamperManifest: func(t *testing.T, id string) {
			t.Helper()
			manifestPath := filepath.Join(root, id[:2], id, "manifest.json")
			raw, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			tampered := append(raw[:len(raw)-1], []byte(`"x"}`)...)
			if err := os.WriteFile(manifestPath, tampered, 0o644); err != nil {
				t.Fatalf("rewrite tampered manifest: %v", err)
			}
		},
	}
}

func s3Harness(t *testing.T) storeHarness {
	t.Helper()
	endpoint := os.Getenv("MINIO_TEST_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("S3_TEST_ENDPOINT")
	}
	if endpoint == "" {
		t.Skip("MINIO_TEST_ENDPOINT/S3_TEST_ENDPOINT not set")
	}

	accessKey := envDefault("MINIO_TEST_ACCESS_KEY", envDefault("S3_TEST_ACCESS_KEY", "minioadmin"))
	secretKey := envDefault("MINIO_TEST_SECRET_KEY", envDefault("S3_TEST_SECRET_KEY", "minioadmin"))
	bucket := envDefault("MINIO_TEST_BUCKET", envDefault("S3_TEST_BUCKET", "foundry-evidence-test"))
	secure, err := strconv.ParseBool(envDefault("MINIO_TEST_SECURE", envDefault("S3_TEST_SECURE", "false")))
	if err != nil {
		t.Fatalf("parse MINIO_TEST_SECURE/S3_TEST_SECURE: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	profileID := fmt.Sprintf("test-%d", time.Now().UnixNano())
	store, err := NewS3Store(ctx, S3StoreOptions{
		Endpoint:        endpoint,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		Secure:          secure,
		Bucket:          bucket,
		ProfileID:       profileID,
		CacheDir:        t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}

	return storeHarness{
		store: store,
		tamperArtifact: func(t *testing.T, id string) {
			t.Helper()
			raw := []byte("jello")
			_, err := store.client.PutObject(ctx, store.bucket, store.objectKey(store.bundleRelKey(id, "artifacts/output.txt")), bytes.NewReader(raw), int64(len(raw)), store.putObjectOptions("application/octet-stream"))
			if err != nil {
				t.Fatalf("overwrite s3 artifact: %v", err)
			}
		},
		tamperManifest: func(t *testing.T, id string) {
			t.Helper()
			raw, err := store.readObject(store.bundleRelKey(id, "manifest.json"))
			if err != nil {
				t.Fatalf("read s3 manifest: %v", err)
			}
			tampered := append(raw[:len(raw)-1], []byte(`"x"}`)...)
			if err := store.putBytes(store.bundleRelKey(id, "manifest.json"), tampered, "application/json"); err != nil {
				t.Fatalf("overwrite s3 manifest: %v", err)
			}
		},
	}
}

func TestNewStoreForProfileRequiresObjectStore(t *testing.T) {
	_, err := NewStoreForProfile(context.Background(), StoreForProfileOptions{
		Policy: StorePolicy{
			ProfileID:          "production",
			Backend:            BackendFilesystem,
			RequireObjectStore: true,
		},
		FilesystemRoot: t.TempDir(),
	})
	if !errors.Is(err, ErrStoreBackendRefused) {
		t.Fatalf("NewStoreForProfile error = %v, want ErrStoreBackendRefused", err)
	}
}

func TestNewStoreForProfileFilesystemAllowed(t *testing.T) {
	store, err := NewStoreForProfile(context.Background(), StoreForProfileOptions{
		Policy:         StorePolicy{ProfileID: "personal", Backend: BackendFilesystem},
		FilesystemRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewStoreForProfile: %v", err)
	}
	if _, ok := store.(*FSStore); !ok {
		t.Fatalf("store type = %T, want *FSStore", store)
	}
}

func TestS3DenyPublicAccessPolicy(t *testing.T) {
	policy, err := S3DenyPublicAccessPolicy("foundry-evidence")
	if err != nil {
		t.Fatalf("S3DenyPublicAccessPolicy: %v", err)
	}
	var decoded struct {
		Statement []struct {
			Effect    string
			Principal string
			Action    []string
			Resource  []string
			Condition map[string]map[string]string
		}
	}
	if err := json.Unmarshal([]byte(policy), &decoded); err != nil {
		t.Fatalf("policy is not valid json: %v", err)
	}
	if len(decoded.Statement) != 1 {
		t.Fatalf("statement count = %d, want 1", len(decoded.Statement))
	}
	stmt := decoded.Statement[0]
	if stmt.Effect != "Deny" || stmt.Principal != "*" {
		t.Fatalf("policy statement = %+v, want Deny for public principal", stmt)
	}
	if !containsAll(strings.Join(stmt.Action, ","), "s3:*") {
		t.Fatalf("policy actions = %v, want s3:* denied for anonymous public access", stmt.Action)
	}
	if stmt.Condition["StringEquals"]["aws:PrincipalType"] != "Anonymous" {
		t.Fatalf("policy condition = %v, want anonymous-only deny", stmt.Condition)
	}
}

func TestS3PutObjectOptionsRequestServerSideEncryption(t *testing.T) {
	store := &S3Store{profile: "personal"}
	opts := store.putObjectOptions("application/json")
	if opts.ServerSideEncryption == nil {
		t.Fatalf("ServerSideEncryption is nil")
	}
	if opts.UserMetadata["foundry-profile"] != "personal" {
		t.Fatalf("foundry-profile metadata = %q, want personal", opts.UserMetadata["foundry-profile"])
	}
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
