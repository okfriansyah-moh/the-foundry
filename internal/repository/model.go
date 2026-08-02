package repository

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Closed provider vocabulary (aligned with kernel envelope providers).
const (
	ProviderGitHub    = "github"
	ProviderBitbucket = "bitbucket"
	ProviderLocal     = "local"
)

var (
	ErrNotFound      = errors.New("repository: not found")
	ErrInvalid       = errors.New("repository: invalid")
	ErrWrongOwner    = errors.New("repository: wrong owner")
	ErrPathRefused   = errors.New("repository: local path refused")
	ErrStaleRevision = errors.New("repository: stale or missing revision")
)

// Record is one owned repository declaration.
type Record struct {
	ID                   string
	Provider             string
	CanonicalURL         string
	Alias                string
	ProfileID            string
	OrganizationID       string
	PinnedBaseRevision   string
	DefaultTargetBranch  string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// Validate checks closed vocabularies and required identity fields.
func (r Record) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("%w: missing id", ErrInvalid)
	}
	if !validProvider(r.Provider) {
		return fmt.Errorf("%w: unknown provider %q", ErrInvalid, r.Provider)
	}
	if r.CanonicalURL == "" {
		return fmt.Errorf("%w: missing canonical_url", ErrInvalid)
	}
	if _, err := NormalizeCanonicalURL(r.CanonicalURL); err != nil {
		return err
	}
	if r.ProfileID == "" {
		return fmt.Errorf("%w: missing profile_id", ErrInvalid)
	}
	if r.OrganizationID != "" && r.ProfileID == "" {
		return fmt.Errorf("%w: organization without profile", ErrInvalid)
	}
	return nil
}

// NormalizeCanonicalURL lowercases scheme/host and strips trailing slashes and
// .git suffix for stable identity comparison.
func NormalizeCanonicalURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		// Allow file:// and path-like local roots only when explicitly local.
		if strings.HasPrefix(raw, "file://") || strings.HasPrefix(raw, "/") {
			return strings.TrimRight(raw, "/"), nil
		}
		return "", fmt.Errorf("%w: canonical_url %q", ErrInvalid, raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), ".git")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func validProvider(p string) bool {
	switch p {
	case ProviderGitHub, ProviderBitbucket, ProviderLocal:
		return true
	default:
		return false
	}
}
