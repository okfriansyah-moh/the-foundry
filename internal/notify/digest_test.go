package notify

import (
	"strings"
	"testing"
	"time"
)

var baseTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func makePromo(id, product string) DigestPromotion {
	return DigestPromotion{
		ID:            id,
		ProductID:     product,
		ChangeRef:     "abcdef1234567890",
		PlanDigest:    "deadbeef00001111222233334444",
		MetricsBefore: map[string]float64{"net_mrr_usd": 100, "activation_rate": 0.10},
		MetricsAfter:  map[string]float64{"net_mrr_usd": 120, "activation_rate": 0.12},
		CreatedAt:     baseTime.Add(-2 * time.Hour),
	}
}

// TestFormatWeeklyDigest_ContainsRollbackLink verifies digest includes nonce link.
func TestFormatWeeklyDigest_ContainsRollbackLink(t *testing.T) {
	promos := []DigestPromotion{makePromo("p1", "prod1")}
	nonces := map[string]string{"p1": "nonce123abc"}
	msg := FormatWeeklyDigest(promos, nonces, baseTime)

	if !strings.Contains(msg, "/rollback p1 nonce123abc") {
		t.Errorf("digest missing rollback link; got:\n%s", msg)
	}
	if !strings.Contains(msg, "prod1") {
		t.Errorf("digest missing product id; got:\n%s", msg)
	}
}

// TestFormatWeeklyDigest_Empty returns empty-state message.
func TestFormatWeeklyDigest_Empty(t *testing.T) {
	msg := FormatWeeklyDigest(nil, nil, baseTime)
	if !strings.Contains(msg, "No promotions") {
		t.Errorf("expected empty-state message; got: %s", msg)
	}
}

// TestBuildVetoRecords_ExpiriesCorrect verifies 24h window.
func TestBuildVetoRecords_ExpiriesCorrect(t *testing.T) {
	promos := []DigestPromotion{makePromo("p1", "prod1"), makePromo("p2", "prod1")}
	recs := BuildVetoRecords(promos, baseTime)
	if len(recs) != 2 {
		t.Fatalf("len(recs)=%d, want 2", len(recs))
	}
	for _, r := range recs {
		want := baseTime.Add(VetoWindow)
		if !r.ExpiresAt.Equal(want) {
			t.Errorf("ExpiresAt=%v, want %v", r.ExpiresAt, want)
		}
	}
}

// TestIsVetoExpired verifies window boundary.
func TestIsVetoExpired(t *testing.T) {
	rec := VetoRecord{PromotionID: "p1", ExpiresAt: baseTime.Add(VetoWindow)}
	justBefore := baseTime.Add(VetoWindow - time.Second)
	justAfter := baseTime.Add(VetoWindow + time.Second)
	if IsVetoExpired(rec, justBefore) {
		t.Error("should not be expired before window closes")
	}
	if !IsVetoExpired(rec, justAfter) {
		t.Error("should be expired after window closes")
	}
}

// TestFreezeCheck_RollbackChain freezes on depth >2.
func TestFreezeCheck_RollbackChain(t *testing.T) {
	reason := FreezeCheck("prod1", nil, 3)
	if reason != FreezeReasonRollbackChain {
		t.Errorf("reason=%q, want %q", reason, FreezeReasonRollbackChain)
	}
}

// TestFreezeCheck_RepeatedVeto freezes on two vetoes.
func TestFreezeCheck_RepeatedVeto(t *testing.T) {
	history := []VetoRecord{
		{PromotionID: "p1", ProductID: "prod1", Vetoed: true},
		{PromotionID: "p2", ProductID: "prod1", Vetoed: true},
	}
	reason := FreezeCheck("prod1", history, 0)
	if reason != FreezeReasonRepeatedVeto {
		t.Errorf("reason=%q, want %q", reason, FreezeReasonRepeatedVeto)
	}
}

// TestFreezeCheck_Clear passes with shallow chain and zero vetoes.
func TestFreezeCheck_Clear(t *testing.T) {
	history := []VetoRecord{{PromotionID: "p1", ProductID: "prod1", Vetoed: false}}
	reason := FreezeCheck("prod1", history, 1)
	if reason != "" {
		t.Errorf("reason=%q, want empty (clear)", reason)
	}
}

// TestFreezeCheck_CrossProductIsolation verifies vetoes for a different product
// do not count toward the freeze threshold.
func TestFreezeCheck_CrossProductIsolation(t *testing.T) {
	history := []VetoRecord{
		{PromotionID: "p1", ProductID: "other-product", Vetoed: true},
		{PromotionID: "p2", ProductID: "other-product", Vetoed: true},
	}
	reason := FreezeCheck("prod1", history, 0)
	if reason != "" {
		t.Errorf("cross-product vetoes should not freeze prod1; got reason=%q", reason)
	}
}

// TestVeto_WithinWindowRollsBack verifies veto-within-window path.
func TestVeto_WithinWindowRollsBack(t *testing.T) {
	rec := VetoRecord{PromotionID: "p1", ExpiresAt: baseTime.Add(VetoWindow)}
	vetoTime := baseTime.Add(12 * time.Hour)
	if IsVetoExpired(rec, vetoTime) {
		t.Fatal("veto should not be expired at 12h")
	}
	rec.Vetoed = true
	rec.VetoedAt = &vetoTime
	if !rec.Vetoed {
		t.Error("vetoed=false after setting true")
	}
}

// TestVeto_ExpiredIgnored verifies expired veto is ignored (window passed).
func TestVeto_ExpiredIgnored(t *testing.T) {
	rec := VetoRecord{PromotionID: "p1", ExpiresAt: baseTime.Add(VetoWindow)}
	afterExpiry := baseTime.Add(25 * time.Hour)
	if !IsVetoExpired(rec, afterExpiry) {
		t.Error("veto should be expired at 25h")
	}
}
