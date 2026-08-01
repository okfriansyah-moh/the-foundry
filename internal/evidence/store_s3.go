package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/encrypt"
)

// Backend names the evidence storage backend selected by compiled profile
// policy.
type Backend string

const (
	BackendFilesystem Backend = "filesystem"
	BackendS3         Backend = "s3"
)

// ErrStoreBackendRefused is returned when compiled profile policy requires a
// durable object store but the caller selected the filesystem backend.
var ErrStoreBackendRefused = errors.New("evidence: profile requires object store")

// StorePolicy is the evidence-store slice of compiled profile policy. It is
// intentionally minimal so callers can map their compiled policy into this
// package without changing the Store interface.
type StorePolicy struct {
	ProfileID          string
	Backend            Backend
	RequireObjectStore bool
}

// StoreForProfileOptions carries backend-specific constructor inputs. Backend
// selection comes from Policy, never from environment variables inside this
// package.
type StoreForProfileOptions struct {
	Policy         StorePolicy
	FilesystemRoot string
	S3             S3StoreOptions
}

// NewStoreForProfile constructs the evidence Store selected by compiled
// profile policy. Production profiles can set RequireObjectStore to fail closed
// if the filesystem backend is selected.
func NewStoreForProfile(ctx context.Context, opts StoreForProfileOptions) (Store, error) {
	backend := opts.Policy.Backend
	if backend == "" {
		backend = BackendFilesystem
	}
	if opts.Policy.RequireObjectStore && backend != BackendS3 {
		return nil, fmt.Errorf("%w: profile %s selected %s", ErrStoreBackendRefused, opts.Policy.ProfileID, backend)
	}

	switch backend {
	case BackendFilesystem:
		if opts.FilesystemRoot == "" {
			return nil, errors.New("evidence: filesystem root required")
		}
		return NewFSStore(opts.FilesystemRoot), nil
	case BackendS3:
		s3Opts := opts.S3
		if s3Opts.ProfileID == "" {
			s3Opts.ProfileID = opts.Policy.ProfileID
		}
		store, err := NewS3Store(ctx, s3Opts)
		if err != nil {
			return nil, err
		}
		return store, nil
	default:
		return nil, fmt.Errorf("evidence: unknown store backend %q", backend)
	}
}

// S3StoreOptions configures an S3/MinIO-compatible Store. ProfileID forms the
// per-profile object namespace: profiles/<profile>/evidence/...
type S3StoreOptions struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Secure          bool
	Bucket          string
	Region          string
	ProfileID       string
	CacheDir        string
}

// S3Store is a Store backed by an S3/MinIO-compatible bucket. Object keys are
// content-addressed under profiles/<profile>/evidence/<sha[0:2]>/<sha>/...
type S3Store struct {
	ctx      context.Context
	client   *minio.Client
	bucket   string
	region   string
	profile  string
	cacheDir string
}

// NewS3Store creates an S3Store, creates the bucket if needed, and applies a
// private/deny-public bucket policy. Every S3 request uses ctx.
func NewS3Store(ctx context.Context, opts S3StoreOptions) (*S3Store, error) {
	if ctx == nil {
		return nil, errors.New("evidence: context required")
	}
	if opts.Endpoint == "" {
		return nil, errors.New("evidence: s3 endpoint required")
	}
	if opts.Bucket == "" {
		return nil, errors.New("evidence: s3 bucket required")
	}
	if opts.AccessKeyID == "" || opts.SecretAccessKey == "" {
		return nil, errors.New("evidence: s3 static credentials required")
	}
	if opts.CacheDir == "" {
		return nil, errors.New("evidence: s3 cache dir required")
	}
	if err := validateProfileID(opts.ProfileID); err != nil {
		return nil, err
	}

	client, err := minio.New(opts.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKeyID, opts.SecretAccessKey, opts.SessionToken),
		Secure: opts.Secure,
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("evidence: create s3 client: %w", err)
	}

	store := &S3Store{
		ctx:      ctx,
		client:   client,
		bucket:   opts.Bucket,
		region:   opts.Region,
		profile:  opts.ProfileID,
		cacheDir: opts.CacheDir,
	}
	if err := store.ensureBucket(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *S3Store) ensureBucket() error {
	exists, err := s.client.BucketExists(s.ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("evidence: check s3 bucket %s: %w", s.bucket, err)
	}
	if !exists {
		if err := s.client.MakeBucket(s.ctx, s.bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
			return fmt.Errorf("evidence: create s3 bucket %s: %w", s.bucket, err)
		}
	}
	policy, err := S3DenyPublicAccessPolicy(s.bucket)
	if err != nil {
		return err
	}
	if err := s.client.SetBucketPolicy(s.ctx, s.bucket, policy); err != nil {
		if !strings.Contains(err.Error(), "invalid condition key 'aws:PrincipalType'") {
			return fmt.Errorf("evidence: set private s3 bucket policy for %s: %w", s.bucket, err)
		}
		minioPolicy, policyErr := minioDenyPublicAccessPolicy(s.bucket)
		if policyErr != nil {
			return policyErr
		}
		if err := s.client.SetBucketPolicy(s.ctx, s.bucket, minioPolicy); err != nil {
			return fmt.Errorf("evidence: set private minio bucket policy for %s: %w", s.bucket, err)
		}
	}
	return nil
}

// Put computes bundle.Manifest's digest and stores manifest/artifacts under the
// profile namespace. It errors with ErrBundleExists when the manifest key
// already exists.
func (s *S3Store) Put(bundle Bundle) (string, error) {
	id, err := bundle.Manifest.DigestHex()
	if err != nil {
		return "", fmt.Errorf("evidence: compute bundle id: %w", err)
	}
	if _, err := s.client.StatObject(s.ctx, s.bucket, s.objectKey(s.bundleRelKey(id, "manifest.json")), minio.StatObjectOptions{}); err == nil {
		return "", fmt.Errorf("%w: %s", ErrBundleExists, id)
	} else if !isS3NotFound(err) {
		return "", fmt.Errorf("evidence: stat s3 manifest for %s: %w", id, err)
	}

	for _, ref := range bundle.Manifest.Artifacts {
		srcPath, err := safeJoin(bundle.Dir, ref.Path)
		if err != nil {
			return "", fmt.Errorf("evidence: artifact %s: %w", ref.Path, err)
		}
		if err := validateObjectRelKey(ref.Path); err != nil {
			return "", fmt.Errorf("evidence: artifact %s: %w", ref.Path, err)
		}
		if err := s.putFile(s.bundleRelKey(id, path.Join("artifacts", path.Clean(ref.Path))), srcPath, "application/octet-stream"); err != nil {
			return "", fmt.Errorf("evidence: upload artifact %s: %w", ref.Path, err)
		}
	}

	canon, err := bundle.Manifest.canonicalJSON()
	if err != nil {
		return "", fmt.Errorf("evidence: encode manifest: %w", err)
	}
	if err := s.putBytes(s.bundleRelKey(id, "manifest.json"), canon, "application/json"); err != nil {
		return "", fmt.Errorf("evidence: upload manifest: %w", err)
	}
	return id, nil
}

// Get loads the manifest and downloads artifacts into CacheDir before returning
// a Bundle whose Dir points at that cache.
func (s *S3Store) Get(id string) (Bundle, error) {
	raw, err := s.readObject(s.bundleRelKey(id, "manifest.json"))
	if err != nil {
		return Bundle{}, fmt.Errorf("evidence: read s3 manifest for %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Bundle{}, fmt.Errorf("evidence: decode manifest for %s: %w", id, err)
	}

	artifactsDir := filepath.Join(s.cacheDir, s.profile, id[:2], id, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return Bundle{}, fmt.Errorf("evidence: create s3 cache for %s: %w", id, err)
	}
	for _, ref := range m.Artifacts {
		if err := validateObjectRelKey(ref.Path); err != nil {
			return Bundle{}, fmt.Errorf("evidence: artifact %s: %w", ref.Path, err)
		}
		raw, err := s.readObject(s.bundleRelKey(id, path.Join("artifacts", path.Clean(ref.Path))))
		if err != nil {
			return Bundle{}, fmt.Errorf("evidence: read s3 artifact %s: %w", ref.Path, err)
		}
		dstPath, err := safeJoin(artifactsDir, ref.Path)
		if err != nil {
			return Bundle{}, fmt.Errorf("evidence: artifact %s: %w", ref.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return Bundle{}, fmt.Errorf("evidence: create cache dir for %s: %w", ref.Path, err)
		}
		if err := os.WriteFile(dstPath, raw, 0o644); err != nil {
			return Bundle{}, fmt.Errorf("evidence: write cached artifact %s: %w", ref.Path, err)
		}
	}
	return Bundle{Manifest: m, Dir: artifactsDir}, nil
}

// Verify re-derives the manifest digest and every artifact hash from bytes read
// back from S3.
func (s *S3Store) Verify(id string) error {
	raw, err := s.readObject(s.bundleRelKey(id, "manifest.json"))
	if err != nil {
		return fmt.Errorf("evidence: read s3 manifest for %s: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("evidence: decode manifest for %s: %w", id, err)
	}
	gotDigest, err := m.DigestHex()
	if err != nil {
		return fmt.Errorf("evidence: recompute manifest digest for %s: %w", id, err)
	}
	if gotDigest != id {
		return fmt.Errorf("%w: manifest.json digest %s does not match bundle id %s", ErrVerifyFailed, gotDigest, id)
	}

	for _, ref := range m.Artifacts {
		if err := validateObjectRelKey(ref.Path); err != nil {
			return fmt.Errorf("%w: artifact %s: %v", ErrVerifyFailed, ref.Path, err)
		}
		sum, size, err := s.hashObject(s.bundleRelKey(id, path.Join("artifacts", path.Clean(ref.Path))))
		if err != nil {
			return fmt.Errorf("%w: artifact %s: %v", ErrVerifyFailed, ref.Path, err)
		}
		if sum != ref.SHA256 {
			return fmt.Errorf("%w: artifact %s: sha256 mismatch (stored %s, actual %s)", ErrVerifyFailed, ref.Path, ref.SHA256, sum)
		}
		if size != ref.Bytes {
			return fmt.Errorf("%w: artifact %s: size mismatch (stored %d, actual %d)", ErrVerifyFailed, ref.Path, ref.Bytes, size)
		}
	}
	return nil
}

// DeleteKey deletes one object key relative to this store's profile namespace.
// It is the retention sweeper hook for S3-backed evidence.
func (s *S3Store) DeleteKey(ctx context.Context, key string) error {
	if ctx == nil {
		return errors.New("evidence: context required")
	}
	if err := validateObjectRelKey(key); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, s.objectKey(path.Clean(key)), minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("evidence: delete s3 object %s: %w", key, err)
	}
	return nil
}

func (s *S3Store) putFile(relKey, srcPath, contentType string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", srcPath, err)
	}
	_, err = s.client.PutObject(s.ctx, s.bucket, s.objectKey(relKey), f, info.Size(), s.putObjectOptions(contentType))
	return err
}

func (s *S3Store) putBytes(relKey string, raw []byte, contentType string) error {
	_, err := s.client.PutObject(s.ctx, s.bucket, s.objectKey(relKey), bytes.NewReader(raw), int64(len(raw)), s.putObjectOptions(contentType))
	return err
}

func (s *S3Store) readObject(relKey string) ([]byte, error) {
	obj, err := s.client.GetObject(s.ctx, s.bucket, s.objectKey(relKey), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = obj.Close() }()
	raw, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relKey, err)
	}
	return raw, nil
}

func (s *S3Store) hashObject(relKey string) (string, int64, error) {
	obj, err := s.client.GetObject(s.ctx, s.bucket, s.objectKey(relKey), minio.GetObjectOptions{})
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = obj.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, obj)
	if err != nil {
		return "", 0, fmt.Errorf("read %s: %w", relKey, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func (s *S3Store) putObjectOptions(contentType string) minio.PutObjectOptions {
	return minio.PutObjectOptions{
		ContentType:          contentType,
		ServerSideEncryption: encrypt.NewSSE(),
		UserMetadata: map[string]string{
			"foundry-profile": s.profile,
		},
	}
}

func (s *S3Store) objectKey(relKey string) string {
	return path.Join("profiles", s.profile, "evidence", relKey)
}

func (s *S3Store) bundleRelKey(id, rel string) string {
	return path.Join(id[:2], id, rel)
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket"
}

func validateProfileID(profileID string) error {
	if profileID == "" {
		return errors.New("evidence: profile id required for s3 store")
	}
	for _, r := range profileID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("evidence: invalid profile id %q", profileID)
	}
	return nil
}

func validateObjectRelKey(key string) error {
	if key == "" {
		return errors.New("object key required")
	}
	if strings.HasPrefix(key, "/") {
		return fmt.Errorf("object key must be relative: %s", key)
	}
	clean := path.Clean(key)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("object key escapes profile namespace: %s", key)
	}
	return nil
}

// S3DenyPublicAccessPolicy returns the bucket policy this package applies to
// evidence buckets: no public list/read access to the bucket or its objects.
func S3DenyPublicAccessPolicy(bucket string) (string, error) {
	if bucket == "" {
		return "", errors.New("evidence: bucket required")
	}
	policy := struct {
		Version   string `json:"Version"`
		Statement []struct {
			Sid       string   `json:"Sid"`
			Effect    string   `json:"Effect"`
			Principal string   `json:"Principal"`
			Action    []string `json:"Action"`
			Resource  []string `json:"Resource"`
			Condition map[string]struct {
				PrincipalType string `json:"aws:PrincipalType"`
			} `json:"Condition"`
		} `json:"Statement"`
	}{
		Version: "2012-10-17",
		Statement: []struct {
			Sid       string   `json:"Sid"`
			Effect    string   `json:"Effect"`
			Principal string   `json:"Principal"`
			Action    []string `json:"Action"`
			Resource  []string `json:"Resource"`
			Condition map[string]struct {
				PrincipalType string `json:"aws:PrincipalType"`
			} `json:"Condition"`
		}{
			{
				Sid:       "DenyPublicEvidenceAccess",
				Effect:    "Deny",
				Principal: "*",
				Action:    []string{"s3:*"},
				Resource:  []string{"arn:aws:s3:::" + bucket, "arn:aws:s3:::" + bucket + "/*"},
				Condition: map[string]struct {
					PrincipalType string `json:"aws:PrincipalType"`
				}{
					"StringEquals": {PrincipalType: "Anonymous"},
				},
			},
		},
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("evidence: marshal s3 bucket policy: %w", err)
	}
	return string(raw), nil
}

func minioDenyPublicAccessPolicy(bucket string) (string, error) {
	if bucket == "" {
		return "", errors.New("evidence: bucket required")
	}
	policy := struct {
		Version   string `json:"Version"`
		Statement []struct {
			Sid       string   `json:"Sid"`
			Effect    string   `json:"Effect"`
			Principal string   `json:"Principal"`
			Action    []string `json:"Action"`
			Resource  []string `json:"Resource"`
		} `json:"Statement"`
	}{
		Version: "2012-10-17",
		Statement: []struct {
			Sid       string   `json:"Sid"`
			Effect    string   `json:"Effect"`
			Principal string   `json:"Principal"`
			Action    []string `json:"Action"`
			Resource  []string `json:"Resource"`
		}{
			{
				Sid:       "DenyPublicEvidenceAccess",
				Effect:    "Deny",
				Principal: "*",
				Action:    []string{"s3:*"},
				Resource:  []string{"arn:aws:s3:::" + bucket, "arn:aws:s3:::" + bucket + "/*"},
			},
		},
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("evidence: marshal minio bucket policy: %w", err)
	}
	return string(raw), nil
}
