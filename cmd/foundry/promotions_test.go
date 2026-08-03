package main

import "testing"

func TestPromotionFreezeScope(t *testing.T) {
	if got := promotionFreezeScope("product-1", ""); got != "product-1" {
		t.Fatalf("default scope = %q", got)
	}
	if got := promotionFreezeScope("product-1", "global"); got != "global" {
		t.Fatalf("explicit skill-evolution scope = %q", got)
	}
}
