package retention

import (
	"testing"
	"time"
)

func TestBuildExport(t *testing.T) {
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	bundle := BuildExport("user-1", map[string][]string{"billing": {"invoice-1"}}, now)
	if bundle.Principal != "user-1" || len(bundle.Data["billing"]) != 1 || !bundle.GeneratedAt.Equal(now) {
		t.Fatalf("bundle=%+v", bundle)
	}
}
