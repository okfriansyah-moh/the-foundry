package billing

import "sync"

type EventStore struct {
	mu    sync.Mutex
	seen  map[string]struct{}
	total int
}

func NewEventStore() *EventStore {
	return &EventStore{seen: map[string]struct{}{}}
}

func (s *EventStore) Handle(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[eventID]; ok {
		return false
	}
	s.seen[eventID] = struct{}{}
	s.total++
	return true
}

func (s *EventStore) Total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}
