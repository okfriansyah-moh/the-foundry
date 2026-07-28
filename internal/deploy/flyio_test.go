package deploy

import (
	"context"
	"testing"
)

func TestFlyAdapterAndRehearsal(t *testing.T) {
	adapter := FlyAdapter{Token: "token"}
	rec, err := adapter.DeployPreview(context.Background(), "Demo", "ref-1")
	if err != nil {
		t.Fatalf("DeployPreview: %v", err)
	}
	if rec.Product != "foundry-demo" {
		t.Fatalf("Product=%q, want foundry-demo", rec.Product)
	}
	if err := RehearseRollback(context.Background(), adapter, "Demo", "ref-new", "ref-old"); err != nil {
		t.Fatalf("RehearseRollback: %v", err)
	}
}
