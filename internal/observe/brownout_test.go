package observe_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

func loadRepoQueueConfig(t *testing.T) observe.QueueConfig {
	t.Helper()
	cfg, err := observe.LoadQueueConfig(repoQueueConfigPath)
	if err != nil {
		t.Fatalf("LoadQueueConfig: %v", err)
	}
	return cfg
}

func TestBrownoutController_DisabledAdmitsEveryLane(t *testing.T) {
	b := observe.NewBrownoutController(loadRepoQueueConfig(t))
	for _, lane := range []observe.Lane{observe.LaneRecovery, observe.LaneDelivery, observe.LaneNotification, observe.LaneLearning} {
		if !b.Admit(lane) {
			t.Errorf("lane %q: Admit() = false while brownout disabled, want true", lane)
		}
	}
}

func TestBrownoutController_EnabledShedsOnlyLearning(t *testing.T) {
	b := observe.NewBrownoutController(loadRepoQueueConfig(t))
	b.SetEnabled(true)

	for _, lane := range []observe.Lane{observe.LaneRecovery, observe.LaneDelivery, observe.LaneNotification} {
		if !b.Admit(lane) {
			t.Errorf("lane %q: Admit() = false under brownout, want true (Steps: keeps delivery+recovery; notification carries this task's own P1 alert)", lane)
		}
	}
	if b.Admit(observe.LaneLearning) {
		t.Error("lane learning: Admit() = true under brownout, want false (Steps: sheds learning first)")
	}
}

func TestBrownoutController_DisablingResumesLearning(t *testing.T) {
	b := observe.NewBrownoutController(loadRepoQueueConfig(t))
	b.SetEnabled(true)
	if b.Admit(observe.LaneLearning) {
		t.Fatal("precondition: learning should be shed while enabled")
	}
	b.SetEnabled(false)
	if !b.Admit(observe.LaneLearning) {
		t.Error("learning should resume once brownout is disabled")
	}
}

func TestBrownoutController_UnknownLaneDefaultsToAdmitted(t *testing.T) {
	b := observe.NewBrownoutController(loadRepoQueueConfig(t))
	b.SetEnabled(true)
	if !b.Admit(observe.Lane("unknown")) {
		t.Error("an unconfigured lane must fail safe to admitted, not silently dropped")
	}
}

func TestBrownoutController_SetEnabled_UpdatesGauge(t *testing.T) {
	b := observe.NewBrownoutController(loadRepoQueueConfig(t))

	b.SetEnabled(true)
	if body := scrape(t); !strings.Contains(body, "foundry_brownout_mode 1") {
		t.Fatalf("expected foundry_brownout_mode 1 after SetEnabled(true), got:\n%s", body)
	}

	b.SetEnabled(false)
	if body := scrape(t); !strings.Contains(body, "foundry_brownout_mode 0") {
		t.Fatalf("expected foundry_brownout_mode 0 after SetEnabled(false), got:\n%s", body)
	}
}

// TestBrownoutController_ConcurrentAccess proves Admit/SetEnabled are
// race-safe under concurrent callers (this task's Validation includes
// `go test -race`).
func TestBrownoutController_ConcurrentAccess(t *testing.T) {
	b := observe.NewBrownoutController(loadRepoQueueConfig(t))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			b.SetEnabled(true)
		}()
		go func() {
			defer wg.Done()
			_ = b.Admit(observe.LaneLearning)
		}()
	}
	wg.Wait()
}
