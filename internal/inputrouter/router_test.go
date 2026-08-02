package inputrouter

import (
	"context"
	"testing"
	"time"
)

func TestDecideRoute_PersonalIdea(t *testing.T) {
	d, err := DecideRoute(InputRequest{
		RequestID: "r1", PrincipalID: "p", Mode: ModePersonal, Kind: KindIdea, Origin: OriginCLI,
		SubmittedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Route != "personal.intake" {
		t.Fatalf("route=%s", d.Route)
	}
}

func TestDecideRoute_OrgIdeaRefused(t *testing.T) {
	d, err := DecideRoute(InputRequest{
		RequestID: "r1", PrincipalID: "p", OrganizationID: "o", Mode: ModeOrganization,
		Kind: KindIdea, Origin: OriginAPI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.RefuseReason == "" {
		t.Fatal("expected refuse")
	}
}

func TestDecideRoute_RejectsExecutorMeta(t *testing.T) {
	_, err := DecideRoute(InputRequest{
		RequestID: "r1", PrincipalID: "p", Mode: ModePersonal, Kind: KindPlan, Origin: OriginCLI,
		ClientMeta: map[string]string{"executor": "claude"},
	})
	if err == nil {
		t.Fatal("expected executor refusal")
	}
}

func TestRouter_Idempotent(t *testing.T) {
	r := &Router{Store: NewMemoryStore()}
	in := InputRequest{
		RequestID: "r1", IdempotencyKey: "idem-1", PrincipalID: "p",
		Mode: ModePersonal, Kind: KindIdea, Origin: OriginCLI,
	}
	d1, err := r.Route(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.RequestID = "r2"
	d2, err := r.Route(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if d1.RequestDigest != d2.RequestDigest {
		t.Fatalf("idempotency failed")
	}
}
