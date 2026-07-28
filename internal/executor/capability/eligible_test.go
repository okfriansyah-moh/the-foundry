package capability

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "executor-capabilities.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, `executors:
  - provider: claude-code
    execution_class: cli-agentic
    features: [reasoning.adaptive, tools.strict]
    availability: supported
    last_verified_at: 2026-07-28T00:00:00Z
  - provider: kimi
    execution_class: cli-agentic
    features: []
    availability: unsupported
    last_verified_at: 2026-07-28T00:00:00Z
`)
	reg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.Executors) != 2 {
		t.Fatalf("want 2 records, got %d", len(reg.Executors))
	}
	if _, ok := reg.Lookup("claude-code"); !ok {
		t.Fatal("claude-code not found")
	}
}

func TestLoadRejectsUnknownTopLevelKey(t *testing.T) {
	p := writeTemp(t, `executors: []
bogus_top_level: true
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected load error for unknown top-level key, got nil")
	}
}

func TestLoadRejectsUnknownRecordKey(t *testing.T) {
	p := writeTemp(t, `executors:
  - provider: claude-code
    execution_class: cli-agentic
    availability: supported
    last_verified_at: 2026-07-28T00:00:00Z
    surprise: 1
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected load error for unknown record key, got nil")
	}
}

func TestLoadRejectsBadAvailability(t *testing.T) {
	p := writeTemp(t, `executors:
  - provider: claude-code
    execution_class: cli-agentic
    availability: maybe
    last_verified_at: 2026-07-28T00:00:00Z
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected load error for bad availability, got nil")
	}
}

func TestLoadRejectsDuplicateProvider(t *testing.T) {
	p := writeTemp(t, `executors:
  - provider: claude-code
    execution_class: cli-agentic
    availability: supported
    last_verified_at: 2026-07-28T00:00:00Z
  - provider: claude-code
    execution_class: api
    availability: supported
    last_verified_at: 2026-07-28T00:00:00Z
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected load error for duplicate provider, got nil")
	}
}

func TestLoadRejectsMissingLastVerified(t *testing.T) {
	p := writeTemp(t, `executors:
  - provider: claude-code
    execution_class: cli-agentic
    availability: supported
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected load error for missing last_verified_at, got nil")
	}
}

func TestStale(t *testing.T) {
	reg := Registry{Executors: []Record{
		{Provider: "fresh", LastVerifiedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{Provider: "old", LastVerifiedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
	}}
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	got := reg.Stale(now)
	if !reflect.DeepEqual(got, []string{"old"}) {
		t.Fatalf("Stale = %v, want [old]", got)
	}
}

func TestEligibleFiltersAndSorts(t *testing.T) {
	reg := Registry{Executors: []Record{
		{Provider: "zzz", ExecutionClass: "cli-agentic", Features: []string{"a", "b"}, Availability: AvailabilitySupported},
		{Provider: "aaa", ExecutionClass: "cli-agentic", Features: []string{"a", "b"}, Availability: AvailabilitySupported},
		{Provider: "unsupported-one", Features: []string{"a", "b"}, Availability: AvailabilityUnsupported},
		{Provider: "missing-feature", Features: []string{"a"}, Availability: AvailabilitySupported},
	}}
	got := reg.Eligible("personal", []string{"a", "b"})
	if len(got) != 2 {
		t.Fatalf("want 2 eligible, got %d: %+v", len(got), got)
	}
	if got[0].Provider != "aaa" || got[1].Provider != "zzz" {
		t.Fatalf("Eligible not sorted deterministically: %v", []string{got[0].Provider, got[1].Provider})
	}
}

func TestEligibleProfileDenyAndAllow(t *testing.T) {
	reg := Registry{Executors: []Record{
		{Provider: "deny-me", Availability: AvailabilitySupported, ProfileDeny: []string{"org"}},
		{Provider: "allow-only", Availability: AvailabilitySupported, ProfileAllow: []string{"personal"}},
	}}
	// org profile: deny-me excluded, allow-only excluded (not in allow list).
	if got := reg.Eligible("org", nil); len(got) != 0 {
		t.Fatalf("org: want 0 eligible, got %+v", got)
	}
	// personal profile: deny-me eligible (no deny), allow-only eligible.
	if got := reg.Eligible("personal", nil); len(got) != 2 {
		t.Fatalf("personal: want 2 eligible, got %+v", got)
	}
}

func TestEligibleEmptyRequiredMatchesAll(t *testing.T) {
	reg := Registry{Executors: []Record{
		{Provider: "x", Availability: AvailabilitySupported},
	}}
	if got := reg.Eligible("p", nil); len(got) != 1 {
		t.Fatalf("empty required should match; got %+v", got)
	}
}

// TestRealRegistryLoads guards the shipped config against schema drift.
func TestRealRegistryLoads(t *testing.T) {
	reg, err := Load(filepath.Join("..", "..", "..", "config", "executor-capabilities.yaml"))
	if err != nil {
		t.Fatalf("shipped config/executor-capabilities.yaml failed to load: %v", err)
	}
	if _, ok := reg.Lookup("claude-code"); !ok {
		t.Fatal("shipped registry missing claude-code")
	}
}
