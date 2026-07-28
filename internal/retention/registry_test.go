package retention

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRegistry(t *testing.T) {
	registry, err := LoadRegistry(filepath.Join("..", "..", "config", "retention.yaml"))
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	ttl, err := registry.TTL("customer_data")
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl != 720*time.Hour {
		t.Fatalf("ttl=%s", ttl)
	}
}
