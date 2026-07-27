package observe_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

const repoQueueConfigPath = "../../config/queue-priority.yaml"

func TestLoadQueueConfig_RepoConfigIsValidAndOrdered(t *testing.T) {
	cfg, err := observe.LoadQueueConfig(repoQueueConfigPath)
	if err != nil {
		t.Fatalf("LoadQueueConfig(%s): %v", repoQueueConfigPath, err)
	}

	wantOrder := []string{"recovery", "delivery", "notification", "learning"}
	if len(cfg.Lanes) != len(wantOrder) {
		t.Fatalf("got %d lanes, want %d", len(cfg.Lanes), len(wantOrder))
	}
	for i, name := range wantOrder {
		if cfg.Lanes[i].Name != name {
			t.Errorf("lane %d: got %q, want %q (fixed priority order)", i, cfg.Lanes[i].Name, name)
		}
		if cfg.Lanes[i].Priority != i {
			t.Errorf("lane %q: priority = %d, want %d", name, cfg.Lanes[i].Priority, i)
		}
		if cfg.Lanes[i].TaskQueue == "" {
			t.Errorf("lane %q: task_queue must not be empty", name)
		}
		if cfg.Lanes[i].WorkerSlots <= 0 {
			t.Errorf("lane %q: worker_slots = %d, want > 0", name, cfg.Lanes[i].WorkerSlots)
		}
	}

	// Only learning is sheddable — recovery/delivery/notification must
	// always be admitted under brownout (docs/PLAN.md Task 33 Steps).
	for _, l := range cfg.Lanes {
		want := l.Name == "learning"
		if l.Sheddable != want {
			t.Errorf("lane %q: sheddable = %v, want %v", l.Name, l.Sheddable, want)
		}
	}
}

func TestQueueConfig_Lane_LooksUpByName(t *testing.T) {
	cfg, err := observe.LoadQueueConfig(repoQueueConfigPath)
	if err != nil {
		t.Fatalf("LoadQueueConfig: %v", err)
	}

	got, ok := cfg.Lane(observe.LaneDelivery)
	if !ok {
		t.Fatal("Lane(delivery) not found")
	}
	if got.Name != "delivery" {
		t.Errorf("Lane(delivery).Name = %q, want delivery", got.Name)
	}

	if _, ok := cfg.Lane(observe.Lane("nonexistent")); ok {
		t.Error("Lane(nonexistent) reported found, want not found")
	}
}

func TestLoadQueueConfig_RejectsWrongOrder(t *testing.T) {
	path := writeTempConfig(t, `
lanes:
  - name: delivery
    task_queue: foundry-delivery
    priority: 0
    worker_slots: 8
    sheddable: false
  - name: recovery
    task_queue: foundry-recovery
    priority: 1
    worker_slots: 4
    sheddable: false
  - name: notification
    task_queue: foundry-notification
    priority: 2
    worker_slots: 4
    sheddable: false
  - name: learning
    task_queue: foundry-learning
    priority: 3
    worker_slots: 2
    sheddable: true
`)
	if _, err := observe.LoadQueueConfig(path); err == nil {
		t.Fatal("expected an error for a config with recovery/delivery swapped, got nil")
	}
}

func TestLoadQueueConfig_RejectsMissingLane(t *testing.T) {
	path := writeTempConfig(t, `
lanes:
  - name: recovery
    task_queue: foundry-recovery
    priority: 0
    worker_slots: 4
    sheddable: false
`)
	if _, err := observe.LoadQueueConfig(path); err == nil {
		t.Fatal("expected an error for a config missing three lanes, got nil")
	}
}

func TestLoadQueueConfig_RejectsNonPositiveWorkerSlots(t *testing.T) {
	path := writeTempConfig(t, `
lanes:
  - name: recovery
    task_queue: foundry-recovery
    priority: 0
    worker_slots: 0
    sheddable: false
  - name: delivery
    task_queue: foundry-delivery
    priority: 1
    worker_slots: 8
    sheddable: false
  - name: notification
    task_queue: foundry-notification
    priority: 2
    worker_slots: 4
    sheddable: false
  - name: learning
    task_queue: foundry-learning
    priority: 3
    worker_slots: 2
    sheddable: true
`)
	if _, err := observe.LoadQueueConfig(path); err == nil {
		t.Fatal("expected an error for worker_slots=0, got nil")
	}
}

func TestLoadQueueConfig_MissingFile(t *testing.T) {
	if _, err := observe.LoadQueueConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "queue-priority.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
