package retention

import "time"

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
