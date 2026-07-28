package retention

import "sync"

type HoldRegistry struct {
	mu    sync.RWMutex
	holds map[string]string
}

func NewHoldRegistry() *HoldRegistry { return &HoldRegistry{holds: map[string]string{}} }
func (h *HoldRegistry) Hold(key, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.holds[key] = reason
}
func (h *HoldRegistry) Release(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.holds, key)
}
func (h *HoldRegistry) Active(key string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.holds[key]
	return ok
}
