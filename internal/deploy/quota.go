package deploy

import (
	"fmt"
	"os"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

type ProfileQuota struct {
	MaxWorkflows  int `yaml:"max_workflows"`
	MaxRunners    int `yaml:"max_runners"`
	MaxAdmissions int `yaml:"max_admissions"`
	// MaxActiveMissions caps how many missions a profile may run concurrently
	// in a portfolio (docs/PLAN.md Task 81 / EVO-08 — the per-profile
	// extension of Task 65's quotas that bounds multi-mission fairness at the
	// control-plane level, complementing each mission contract's own
	// maximum_active_products). 0 means unlimited.
	MaxActiveMissions int `yaml:"max_active_missions"`
}

type Usage struct {
	Workflows  int
	Runners    int
	Admissions int
	Missions   int
}

type QuotaFile struct {
	Profiles map[string]ProfileQuota `yaml:"profiles"`
}

type QuotaEnforcer struct {
	mu     sync.Mutex
	quotas map[string]ProfileQuota
	usage  map[string]Usage
}

func LoadQuotas(path string) (map[string]ProfileQuota, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("deploy: read quotas %s: %w", path, err)
	}
	return ParseQuotasYAML(raw, path)
}

// ParseQuotasYAML strictly decodes quotas YAML payload bytes.
func ParseQuotasYAML(raw []byte, source string) (map[string]ProfileQuota, error) {
	if source == "" {
		source = "<memory>"
	}
	var file QuotaFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("deploy: parse quotas %s: %w", source, err)
	}
	return file.Profiles, nil
}

func NewQuotaEnforcer(quotas map[string]ProfileQuota) *QuotaEnforcer {
	return &QuotaEnforcer{quotas: quotas, usage: map[string]Usage{}}
}

func (q *QuotaEnforcer) profileQuota(profile string) (ProfileQuota, error) {
	quota, ok := q.quotas[profile]
	if !ok {
		return ProfileQuota{}, fmt.Errorf("deploy: unknown profile %q", profile)
	}
	return quota, nil
}

func (q *QuotaEnforcer) CanAcquire(profile string, delta Usage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	quota, err := q.profileQuota(profile)
	if err != nil {
		return err
	}
	used := q.usage[profile]
	if quota.MaxWorkflows > 0 && used.Workflows+delta.Workflows > quota.MaxWorkflows {
		return fmt.Errorf("deploy: profile %s exceeds workflow quota", profile)
	}
	if quota.MaxRunners > 0 && used.Runners+delta.Runners > quota.MaxRunners {
		return fmt.Errorf("deploy: profile %s exceeds runner quota", profile)
	}
	if quota.MaxAdmissions > 0 && used.Admissions+delta.Admissions > quota.MaxAdmissions {
		return fmt.Errorf("deploy: profile %s exceeds admission quota", profile)
	}
	if quota.MaxActiveMissions > 0 && used.Missions+delta.Missions > quota.MaxActiveMissions {
		return fmt.Errorf("deploy: profile %s exceeds active-mission quota", profile)
	}
	return nil
}

func (q *QuotaEnforcer) Acquire(profile string, delta Usage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	quota, err := q.profileQuota(profile)
	if err != nil {
		return err
	}
	used := q.usage[profile]
	if quota.MaxWorkflows > 0 && used.Workflows+delta.Workflows > quota.MaxWorkflows {
		return fmt.Errorf("deploy: profile %s exceeds workflow quota", profile)
	}
	if quota.MaxRunners > 0 && used.Runners+delta.Runners > quota.MaxRunners {
		return fmt.Errorf("deploy: profile %s exceeds runner quota", profile)
	}
	if quota.MaxAdmissions > 0 && used.Admissions+delta.Admissions > quota.MaxAdmissions {
		return fmt.Errorf("deploy: profile %s exceeds admission quota", profile)
	}
	if quota.MaxActiveMissions > 0 && used.Missions+delta.Missions > quota.MaxActiveMissions {
		return fmt.Errorf("deploy: profile %s exceeds active-mission quota", profile)
	}
	used.Workflows += delta.Workflows
	used.Runners += delta.Runners
	used.Admissions += delta.Admissions
	used.Missions += delta.Missions
	q.usage[profile] = used
	return nil
}

func (q *QuotaEnforcer) Release(profile string, delta Usage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	used := q.usage[profile]
	used.Workflows -= delta.Workflows
	used.Runners -= delta.Runners
	used.Admissions -= delta.Admissions
	used.Missions -= delta.Missions
	if used.Missions < 0 {
		used.Missions = 0
	}
	if used.Workflows < 0 {
		used.Workflows = 0
	}
	if used.Runners < 0 {
		used.Runners = 0
	}
	if used.Admissions < 0 {
		used.Admissions = 0
	}
	q.usage[profile] = used
}

func (q *QuotaEnforcer) Snapshot() map[string]Usage {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make(map[string]Usage, len(q.usage))
	for k, v := range q.usage {
		out[k] = v
	}
	return out
}

func SortedProfiles(quotas map[string]ProfileQuota) []string {
	profiles := make([]string, 0, len(quotas))
	for profile := range quotas {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)
	return profiles
}
