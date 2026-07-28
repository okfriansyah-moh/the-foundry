package tenx_test

import "testing"

type callRecorder struct {
	calls []string
}

func (r *callRecorder) record(name string) { r.calls = append(r.calls, name) }
func (r *callRecorder) Fetch()             { r.record("fetch") }
func (r *callRecorder) Push()              { r.record("push") }
func (r *callRecorder) Notify()            { r.record("notify") }
func (r *callRecorder) Ledger()            { r.record("ledger") }
func (r *callRecorder) Evidence()          { r.record("evidence") }

func runTenXFlow(r *callRecorder) {
	r.Fetch()
	r.Ledger()
	r.Evidence()
	r.Push()
	r.Notify()
}

func TestTenXWorkflow_AllowedCallSetOnly(t *testing.T) {
	allowed := map[string]struct{}{
		"fetch":    {},
		"push":     {},
		"notify":   {},
		"ledger":   {},
		"evidence": {},
	}
	recorder := &callRecorder{}
	runTenXFlow(recorder)
	if len(recorder.calls) == 0 {
		t.Fatal("expected tenx flow to emit calls")
	}
	seen := map[string]struct{}{}
	for _, call := range recorder.calls {
		if _, ok := allowed[call]; !ok {
			t.Fatalf("unexpected call %q", call)
		}
		seen[call] = struct{}{}
	}
	if len(seen) != len(allowed) {
		t.Fatalf("seen %d allowed call kinds, want %d (%v)", len(seen), len(allowed), recorder.calls)
	}
}
