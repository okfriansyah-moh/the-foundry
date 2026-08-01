// docs/PLAN.md Task 125 (VEN-15) gated live proof. Gated by RUN_FLY_LIVE=1 +
// FLY_API_TOKEN, so a bare `go test ./...` never touches the network. When
// enabled it deploys the product template to a real personal Fly app,
// health-checks the real reachable URL, and rolls it back — the same code path
// the kernel's DeployProduct activity drives.
package deploy_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/deploy"
)

func TestFlyDeployLive(t *testing.T) {
	if os.Getenv("RUN_FLY_LIVE") != "1" {
		t.Skip("set RUN_FLY_LIVE=1 (with FLY_API_TOKEN and FLY_LIVE_PRODUCT) to run the live Fly deploy proof")
	}
	token := os.Getenv("FLY_API_TOKEN")
	product := os.Getenv("FLY_LIVE_PRODUCT")
	artifact := os.Getenv("FLY_LIVE_IMAGE")
	if token == "" || product == "" || artifact == "" {
		t.Skip("FLY_API_TOKEN, FLY_LIVE_PRODUCT and FLY_LIVE_IMAGE must all be set")
	}
	adapter := deploy.FlyAdapter{Token: token}
	if base := os.Getenv("FLY_API_BASE_URL"); base != "" {
		adapter.BaseURL = base
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	receipt, err := deploy.Execute(ctx, adapter, deploy.DeployRequest{
		Product:     product,
		Environment: "preview",
		Artifact:    artifact,
		PreviousRef: os.Getenv("FLY_LIVE_PREVIOUS_IMAGE"),
	})
	if err != nil {
		t.Fatalf("live deploy: %v", err)
	}
	if receipt.Record.URL == "" {
		t.Fatal("live deploy must return a real reachable URL")
	}
	t.Logf("live deploy result: code=%s healthy=%v url=%s", receipt.ResultCode, receipt.Healthy, receipt.Record.URL)

	// Roll back so the live test leaves nothing running.
	if _, err := adapter.Rollback(ctx, product, os.Getenv("FLY_LIVE_PREVIOUS_IMAGE")); err != nil {
		t.Fatalf("live rollback: %v", err)
	}
}
