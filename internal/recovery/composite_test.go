package recovery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

type fakePG struct {
	snaps []recovery.WorkflowSnapshot
	err   error
}

func (f *fakePG) ListNonterminal(context.Context) ([]recovery.WorkflowSnapshot, error) {
	return f.snaps, f.err
}

type fakeHeartbeats struct {
	calls map[string]int
	hb    time.Time
	err   error
}

func (f *fakeHeartbeats) Heartbeat(_ context.Context, workflowID string) (time.Time, error) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[workflowID]++
	return f.hb, f.err
}

func TestCompositeProjectionSource_FillsHeartbeatOnlyForRunning(t *testing.T) {
	now := time.Now()
	pg := &fakePG{snaps: []recovery.WorkflowSnapshot{
		{WorkflowID: "wf-running", Status: state.StatusRunning},
		{WorkflowID: "wf-waiting", Status: state.StatusWaiting},
	}}
	hb := &fakeHeartbeats{hb: now}
	src := &recovery.CompositeProjectionSource{PG: pg, Heartbeats: hb}

	got, err := src.ListNonterminal(context.Background())
	if err != nil {
		t.Fatalf("ListNonterminal: %v", err)
	}
	if hb.calls["wf-running"] != 1 {
		t.Fatalf("Heartbeat called %d times for wf-running, want 1", hb.calls["wf-running"])
	}
	if hb.calls["wf-waiting"] != 0 {
		t.Fatalf("Heartbeat called %d times for wf-waiting, want 0 (WAITING never heartbeats)", hb.calls["wf-waiting"])
	}

	byID := make(map[string]recovery.WorkflowSnapshot, len(got))
	for _, s := range got {
		byID[s.WorkflowID] = s
	}
	if !byID["wf-running"].LastHeartbeat.Equal(now) {
		t.Fatalf("wf-running LastHeartbeat = %v, want %v", byID["wf-running"].LastHeartbeat, now)
	}
	if !byID["wf-waiting"].LastHeartbeat.IsZero() {
		t.Fatalf("wf-waiting LastHeartbeat = %v, want zero", byID["wf-waiting"].LastHeartbeat)
	}
}

func TestCompositeProjectionSource_PropagatesPGError(t *testing.T) {
	wantErr := errors.New("pg down")
	src := &recovery.CompositeProjectionSource{PG: &fakePG{err: wantErr}, Heartbeats: &fakeHeartbeats{}}

	if _, err := src.ListNonterminal(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("ListNonterminal error = %v, want wrapping %v", err, wantErr)
	}
}

func TestCompositeProjectionSource_PropagatesHeartbeatError(t *testing.T) {
	wantErr := errors.New("temporal down")
	pg := &fakePG{snaps: []recovery.WorkflowSnapshot{{WorkflowID: "wf-running", Status: state.StatusRunning}}}
	src := &recovery.CompositeProjectionSource{PG: pg, Heartbeats: &fakeHeartbeats{err: wantErr}}

	if _, err := src.ListNonterminal(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("ListNonterminal error = %v, want wrapping %v", err, wantErr)
	}
}
