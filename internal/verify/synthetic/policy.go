package synthetic

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type CanarySignalPolicy struct {
	MinSessions     int `yaml:"min_sessions"`
	MinTransactions int `yaml:"min_transactions"`
}

type TrafficSample struct {
	Sessions     int
	Transactions int
}

type VerificationMode string

const (
	ModeRealCanary          VerificationMode = "real-canary"
	ModeSyntheticSubstitute VerificationMode = "synthetic-substitute"
	ModeHybrid              VerificationMode = "hybrid"
)

func LoadPolicy(path string) (CanarySignalPolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return CanarySignalPolicy{}, fmt.Errorf("synthetic verify: read policy: %w", err)
	}
	var p CanarySignalPolicy
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return CanarySignalPolicy{}, fmt.Errorf("synthetic verify: decode policy: %w", err)
	}
	return p, nil
}

func DecideVerificationMode(p CanarySignalPolicy, sample TrafficSample) VerificationMode {
	hasSessions := sample.Sessions >= p.MinSessions
	hasTransactions := sample.Transactions >= p.MinTransactions
	switch {
	case hasSessions && hasTransactions:
		return ModeRealCanary
	case hasSessions || hasTransactions:
		return ModeHybrid
	default:
		return ModeSyntheticSubstitute
	}
}

func ModeNotification(mode VerificationMode) string {
	if mode == ModeSyntheticSubstitute || mode == ModeHybrid {
		return string(mode) + " (synthetic — not real user validation)"
	}
	return string(mode)
}
