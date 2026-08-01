package retention

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type SweepCandidate struct {
	Class     string
	Key       string
	CreatedAt time.Time
}

type Sweeper struct {
	Registry Registry
	Holds    *HoldRegistry
	Now      func() time.Time
}

type KeyDeleter interface {
	DeleteKey(ctx context.Context, key string) error
}

func (s Sweeper) Sweep(candidates []SweepCandidate) ([]string, error) {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	var expired []string
	for _, candidate := range candidates {
		if s.Holds != nil && s.Holds.Active(candidate.Key) {
			continue
		}
		ttl, err := s.Registry.TTL(candidate.Class)
		if err != nil {
			return nil, err
		}
		if candidate.CreatedAt.Add(ttl).Before(now) || candidate.CreatedAt.Add(ttl).Equal(now) {
			expired = append(expired, candidate.Key)
		}
	}
	return expired, nil
}

func (s Sweeper) SweepAndDelete(ctx context.Context, candidates []SweepCandidate, deleter KeyDeleter) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("retention: context required")
	}
	if deleter == nil {
		return nil, errors.New("retention: key deleter required")
	}
	expired, err := s.Sweep(candidates)
	if err != nil {
		return nil, err
	}
	for _, key := range expired {
		if err := deleter.DeleteKey(ctx, key); err != nil {
			return nil, fmt.Errorf("retention: delete %s: %w", key, err)
		}
	}
	return expired, nil
}
