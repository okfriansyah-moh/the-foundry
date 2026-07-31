package cost

import "testing"

func TestPriceUsage_ProviderReportedWins(t *testing.T) {
	rt := NewRateTable()
	usd, err := rt.PriceUsage("anything", 100, 50, 0, 0.99)
	if err != nil || usd != 0.99 {
		t.Fatalf("provider-reported dollars are authoritative: usd=%v err=%v", usd, err)
	}
}

func TestPriceUsage_UnknownModelRefuses(t *testing.T) {
	rt := NewRateTable()
	_, err := rt.PriceUsage("mystery", 100, 50, 0, 0)
	var unk PriceUnknownError
	if err == nil {
		t.Fatal("an unrated model must refuse to estimate")
	}
	if !asPriceUnknown(err, &unk) {
		t.Fatalf("want PriceUnknownError, got %T", err)
	}
}

func TestPriceUsage_NoSignalIsUnknown(t *testing.T) {
	rt := NewRateTable(ModelRate{Model: "m", InputPer1KUSD: 1})
	_, err := rt.PriceUsage("m", 0, 0, 0, 0)
	if err == nil {
		t.Fatal("no tokens and no dollars must be unknown, not zero")
	}
}

func TestAmortizeSubscription(t *testing.T) {
	if got := AmortizeSubscription(100, 10); got != 10 {
		t.Fatalf("want 10 per task, got %v", got)
	}
	if got := AmortizeSubscription(100, 0); got != 100 {
		t.Fatalf("zero tasks attributes the whole fee, got %v", got)
	}
}

func TestShadowLedger_CeilingHalts(t *testing.T) {
	s := ShadowLedger{CeilingUSD: 10}
	if s.Add(6); s.Breached() {
		t.Fatal("6 of 10 must not breach")
	}
	if !s.Add(5) {
		t.Fatal("11 of 10 must breach and halt")
	}
	if s.Remaining() != 0 {
		t.Fatalf("breached ledger has 0 remaining, got %v", s.Remaining())
	}
}

func asPriceUnknown(err error, target *PriceUnknownError) bool {
	if e, ok := err.(PriceUnknownError); ok {
		*target = e
		return true
	}
	return false
}
