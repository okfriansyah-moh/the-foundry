package mission

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Observation struct {
	At               time.Time
	ActivationRate   float64
	ConversionRate   float64
	NetMRRUSD        float64
	CostToDateUSD    float64
	NoProgressCycles int
}

type Decide string

const (
	DecideContinue      Decide = "continue"
	DecideImprove       Decide = "improve"
	DecidePivot         Decide = "pivot"
	DecideKillCandidate Decide = "kill-candidate"
)

type DecidePolicy struct {
	NoProgressCyclesForKill int `yaml:"no_progress_cycles_for_kill"`
	DeclineStreakForPivot   int `yaml:"decline_streak_for_pivot"`
}

func LoadDecidePolicy(path string) (DecidePolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return DecidePolicy{}, fmt.Errorf("mission observe: read policy: %w", err)
	}
	var p DecidePolicy
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return DecidePolicy{}, fmt.Errorf("mission observe: decode policy: %w", err)
	}
	return p, nil
}

func DecideFromObservations(history []Observation, policy DecidePolicy) Decide {
	if len(history) == 0 {
		return DecideContinue
	}
	last := history[len(history)-1]
	if last.NoProgressCycles >= policy.NoProgressCyclesForKill {
		return DecideKillCandidate
	}

	declines := 0
	for i := len(history) - 1; i > 0; i-- {
		if history[i].NetMRRUSD < history[i-1].NetMRRUSD {
			declines++
		} else {
			break
		}
	}
	if declines >= policy.DeclineStreakForPivot {
		return DecidePivot
	}
	if last.ActivationRate < 0.10 || last.ConversionRate < 0.02 {
		return DecideImprove
	}
	return DecideContinue
}
