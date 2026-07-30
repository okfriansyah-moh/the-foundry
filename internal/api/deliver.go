package api

import (
	"errors"
	"net/http"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
)

// handleDeliverPlan implements POST /v1/plans/{id}/deliver (docs/PLAN.md Task
// 105 / RTC-01): the single production edge from an ApprovedPlan to a running
// DeliverPlan execution. It is transport only — the kernel (kernel.StartDelivery)
// resolves the lane, the executor allowlist and the deterministic workflow ID;
// this handler never passes an executor name, a task queue or a workflow ID of
// its own (Constitution C4).
//
//	202 + workflow id  — started (or idempotently re-observed)
//	403                — PDP denied (enforced by authorize() before this runs)
//	409                — a delivery for this plan+attempt already exists
//	422                — the plan is revoked/expired, or the lane/allowlist is invalid
func (s *Server) handleDeliverPlan(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	if planID == "" {
		writeError(w, http.StatusBadRequest, "plan id is required")
		return
	}
	lane := r.URL.Query().Get("lane")

	tc, err := client.Dial(client.Options{
		HostPort:  s.deps.TemporalHostPort,
		Namespace: s.deps.TemporalNamespace,
	})
	if err != nil {
		s.logger().Error("api: dial temporal for deliver failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not reach the workflow service")
		return
	}
	defer tc.Close()

	out, err := kernel.StartDelivery(r.Context(), kernel.StartDeps{
		Starter:           tc,
		Provenance:        s.deps.Provenance,
		QueueConfig:       s.deps.QueueConfig,
		LaneSelector:      kernel.LaneSelector{},
		ExecutorAllowlist: s.deps.DeliverExecutorAllowlist,
		Transitions:       kernel.NewPGTransitionStore(s.deps.DB),
	}, kernel.StartDeliveryInput{
		PlanID: planID,
		Lane:   lane,
	})
	switch {
	case errors.Is(err, kernel.ErrStartDuplicate):
		writeError(w, http.StatusConflict, "a delivery for this plan already exists")
		return
	case errors.Is(err, kernel.ErrStartRefused):
		writeError(w, http.StatusUnprocessableEntity, "delivery refused: "+err.Error())
		return
	case err != nil:
		s.logger().Error("api: start delivery failed", "plan", planID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not start delivery")
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}
