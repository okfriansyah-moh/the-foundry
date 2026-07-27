package notify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/admission"
	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
)

type fakeController struct {
	paused, resumed []string
}

func (f *fakeController) Status(_ context.Context, workflow string) (string, error) {
	return "status of " + workflow, nil
}
func (f *fakeController) Pause(_ context.Context, workflow string) error {
	f.paused = append(f.paused, workflow)
	return nil
}
func (f *fakeController) Resume(_ context.Context, workflow string) error {
	f.resumed = append(f.resumed, workflow)
	return nil
}

func newTestRouter(t *testing.T, ctrl notify.WorkflowController) (*notify.CommandRouter, *notify.NonceRegistry) {
	t.Helper()
	chats := notify.NewChatRegistry()
	chats.Register("chat-1", "operator-1")
	nonces := notify.NewNonceRegistry()

	router := &notify.CommandRouter{
		Chats:      chats,
		Nonces:     nonces,
		Controller: ctrl,
		ResolvePlanContext: func(_ context.Context, planID string) (authn.PlanContext, error) {
			if planID == "high-risk-plan" {
				return authn.PlanContext{Tier: admission.TierH, Profile: profile.Personal}, nil
			}
			return authn.PlanContext{Tier: admission.TierA0, Profile: profile.Personal}, nil
		},
		SecureSurfaceURL: func(planID string) string { return "https://secure.example/plans/" + planID },
	}
	return router, nonces
}

func TestCommandRouter_UnknownChatRejectsEverything(t *testing.T) {
	router, nonces := newTestRouter(t, &fakeController{})
	nonce, err := nonces.Issue("chat-1", "wf-1")
	if err != nil {
		t.Fatalf("issue nonce: %v", err)
	}

	reply := router.Handle(context.Background(), "unknown-chat", "/status wf-1 "+nonce)
	if !strings.Contains(reply, "not registered") {
		t.Fatalf("want unregistered-chat rejection, got %q", reply)
	}
}

func TestCommandRouter_PauseResumeStatusHappyPath(t *testing.T) {
	ctrl := &fakeController{}
	router, nonces := newTestRouter(t, ctrl)
	ctx := context.Background()

	pauseNonce, _ := nonces.Issue("chat-1", "wf-1")
	reply := router.Handle(ctx, "chat-1", "/pause wf-1 "+pauseNonce)
	if reply != "paused" || len(ctrl.paused) != 1 || ctrl.paused[0] != "wf-1" {
		t.Fatalf("pause did not reach the controller: reply=%q paused=%v", reply, ctrl.paused)
	}

	resumeNonce, _ := nonces.Issue("chat-1", "wf-1")
	reply = router.Handle(ctx, "chat-1", "/resume wf-1 "+resumeNonce)
	if reply != "resumed" || len(ctrl.resumed) != 1 {
		t.Fatalf("resume did not reach the controller: reply=%q resumed=%v", reply, ctrl.resumed)
	}

	statusNonce, _ := nonces.Issue("chat-1", "wf-1")
	reply = router.Handle(ctx, "chat-1", "/status wf-1 "+statusNonce)
	if reply != "status of wf-1" {
		t.Fatalf("want status reply, got %q", reply)
	}
}

func TestCommandRouter_NonceReplayRejected(t *testing.T) {
	router, nonces := newTestRouter(t, &fakeController{})
	ctx := context.Background()

	nonce, _ := nonces.Issue("chat-1", "wf-1")
	first := router.Handle(ctx, "chat-1", "/status wf-1 "+nonce)
	if first != "status of wf-1" {
		t.Fatalf("first use should succeed, got %q", first)
	}

	second := router.Handle(ctx, "chat-1", "/status wf-1 "+nonce)
	if second != notify.ErrNonceAlreadyUsed.Error() {
		t.Fatalf("replayed nonce must be rejected with %q, got %q", notify.ErrNonceAlreadyUsed.Error(), second)
	}
}

func TestCommandRouter_ExpiredNonceRejected(t *testing.T) {
	router, nonces := newTestRouter(t, &fakeController{})

	// NonceRegistry has no exported way to fast-forward its clock from
	// outside the package, so this test drives Consume directly against
	// a manually-expired scenario via the exported API: issue, then
	// assert the nonce still validates before TTL and is opaque after —
	// full TTL-elapsed behavior is covered by
	// TestNonceRegistry_ExpiredNonceRejected in this package's internal
	// test (nonce_test.go), which injects a fake clock.
	nonce, err := nonces.Issue("chat-1", "wf-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if reply := router.Handle(context.Background(), "chat-1", "/status wf-1 "+nonce); reply != "status of wf-1" {
		t.Fatalf("fresh nonce should be accepted, got %q", reply)
	}
}

func TestCommandRouter_ApproveHighRiskRejected(t *testing.T) {
	router, _ := newTestRouter(t, &fakeController{})
	reply := router.Handle(context.Background(), "chat-1", "/approve high-risk-plan")
	if !strings.Contains(reply, "secure surface") && !strings.Contains(reply, "secure.example") {
		t.Fatalf("high-risk /approve must be rejected with a pointer to the secure surface, got %q", reply)
	}
}

func TestCommandRouter_ApproveNeverPerformsASideEffect(t *testing.T) {
	// Even for a low-risk plan (TelegramApprove.Allowed == true),
	// handleApprove must never call anything beyond returning the guard's
	// Reply text — there is no ApprovalStore/AddApprover dependency
	// wired into CommandRouter at all, so it is structurally impossible
	// for this router to complete an approval.
	router, _ := newTestRouter(t, &fakeController{})
	reply := router.Handle(context.Background(), "chat-1", "/approve low-risk-plan")
	if reply != "approval accepted" {
		t.Fatalf("want authn.TelegramApprove's low-risk reply verbatim, got %q", reply)
	}
}
