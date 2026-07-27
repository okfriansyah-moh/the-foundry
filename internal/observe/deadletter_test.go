package observe_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

type fakeAlerter struct {
	alerts []observe.DeadLetterAlert
	err    error
}

func (f *fakeAlerter) Alert(_ context.Context, a observe.DeadLetterAlert) error {
	f.alerts = append(f.alerts, a)
	return f.err
}

func TestMemoryDeadLetterStore_RecordAndList(t *testing.T) {
	store := observe.NewMemoryDeadLetterStore()
	ctx := context.Background()

	item, err := store.Record(ctx, "learning", []byte(`{"task":"eval-42"}`), "schema validation failed")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if item.ID == "" {
		t.Fatal("Record did not populate an ID")
	}
	if item.Queue != "learning" || item.Reason != "schema validation failed" {
		t.Fatalf("Record returned %+v, unexpected fields", item)
	}

	items, err := store.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("List() = %+v, want exactly the recorded item", items)
	}
}

func TestRecordAndAlert_IncrementsMetricAndSendsAlert(t *testing.T) {
	store := observe.NewMemoryDeadLetterStore()
	alerter := &fakeAlerter{}
	ctx := context.Background()

	before := scrape(t)
	item, err := observe.RecordAndAlert(ctx, store, alerter, "learning", []byte("poison"), "poisoned task")
	if err != nil {
		t.Fatalf("RecordAndAlert: %v", err)
	}
	after := scrape(t)

	if len(alerter.alerts) != 1 {
		t.Fatalf("alerter received %d alerts, want 1", len(alerter.alerts))
	}
	got := alerter.alerts[0]
	if got.ItemID != item.ID || got.Queue != "learning" || got.Reason != "poisoned task" {
		t.Fatalf("alert = %+v, want ItemID=%s Queue=learning Reason=%q", got, item.ID, "poisoned task")
	}
	if before == after {
		t.Fatal("expected foundry_dead_letter_items_total to change after RecordAndAlert")
	}
	if !strings.Contains(after, `foundry_dead_letter_items_total{queue="learning"}`) {
		t.Fatalf("expected a learning-labeled dead_letter_items_total series, got:\n%s", after)
	}
}

func TestRecordAndAlert_NilAlerterIsOptional(t *testing.T) {
	store := observe.NewMemoryDeadLetterStore()
	if _, err := observe.RecordAndAlert(context.Background(), store, nil, "learning", nil, "no alerter configured"); err != nil {
		t.Fatalf("RecordAndAlert with nil alerter: %v", err)
	}
}

func TestRecordAndAlert_AlertFailureIsReturnedButRecordIsNotLost(t *testing.T) {
	store := observe.NewMemoryDeadLetterStore()
	alerter := &fakeAlerter{err: errors.New("telegram unreachable")}
	ctx := context.Background()

	item, err := observe.RecordAndAlert(ctx, store, alerter, "learning", []byte("poison"), "poisoned task")
	if err == nil {
		t.Fatal("expected RecordAndAlert to return the alert error")
	}

	items, listErr := store.List(ctx, 10)
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	found := false
	for _, i := range items {
		if i.ID == item.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the dead-letter record must persist even when the alert send fails")
	}
}
