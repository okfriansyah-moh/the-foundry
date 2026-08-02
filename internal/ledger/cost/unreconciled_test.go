package cost

import "testing"

func TestMissingUsageIsNeverZero(t *testing.T) {
	if MissingUsageIsNeverZero(nil) != KindUnreconciled {
		t.Fatal("nil must be unreconciled")
	}
	v := 1.0
	if MissingUsageIsNeverZero(&v) != KindObserved {
		t.Fatal("observed")
	}
}

func TestFreezeGate_Threshold(t *testing.T) {
	g := NewFreezeGate(2)
	_ = g.RecordMissingUsage("p1", "e1", "missing")
	if err := g.AllowUnattendedReserve("p1"); err != nil {
		t.Fatal("should not freeze yet")
	}
	e := g.RecordMissingUsage("p1", "e2", "missing")
	if !e.FreezeTriggered {
		t.Fatal("expected freeze")
	}
	if err := g.AllowUnattendedReserve("p1"); err == nil {
		t.Fatal("expected freeze refusal")
	}
	g.ClearAfterReconcile("p1")
	if err := g.AllowUnattendedReserve("p1"); err != nil {
		t.Fatal(err)
	}
}
