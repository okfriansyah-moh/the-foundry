package inputroutere2e_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/inputrouter"
)

func TestRouteMatrix(t *testing.T) {
	d, err := inputrouter.DecideRoute(inputrouter.InputRequest{
		RequestID: "r", PrincipalID: "p", Mode: inputrouter.ModeOrganization,
		OrganizationID: "o", Kind: inputrouter.KindPlan, Origin: inputrouter.OriginCLI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Route != "organization.tenx" {
		t.Fatalf("%s", d.Route)
	}
}
