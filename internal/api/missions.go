package api

import (
	"net/http"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

// docs/PLAN.md Task 107 (RTC-03): mission operational surface over HTTP —
// list, status, start and resume. Transport only: the kernel owns the workflow
// (Constitution C4). Each route is behind the same PDP middleware as every
// other internal/api route.

// missionResponse is the wire shape for a mission.Mission. The domain struct
// carries no json tags (it would marshal CamelCase, e.g. "ID"/"PrincipalID"),
// so the API maps it to explicit snake_case here, matching every other route's
// convention (see status.go's statusResponse). Contract already has its own
// snake_case json tags, so it is embedded directly.
type missionResponse struct {
	ID          string           `json:"id"`
	PrincipalID string           `json:"principal_id"`
	WorkflowID  string           `json:"workflow_id"`
	Contract    mission.Contract `json:"contract"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

func newMissionResponse(m mission.Mission) missionResponse {
	return missionResponse{
		ID:          m.ID,
		PrincipalID: m.PrincipalID,
		WorkflowID:  m.WorkflowID,
		Contract:    m.Contract,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

// missionStateResponse is the wire shape for a mission.StateSnapshot (latest
// loop state), snake_case to match the rest of the API.
type missionStateResponse struct {
	MissionID        string     `json:"mission_id"`
	Cycle            int        `json:"cycle"`
	NetMRRUSD        float64    `json:"net_mrr_usd"`
	NoProgressCycles int        `json:"no_progress_cycles"`
	Confirming       bool       `json:"confirming"`
	ConfirmedSince   *time.Time `json:"confirmed_since,omitempty"`
	Status           string     `json:"status"`
	Reason           string     `json:"reason"`
	ResultCode       string     `json:"result_code"`
	ObservedAt       time.Time  `json:"observed_at"`
}

func newMissionStateResponse(s mission.StateSnapshot) missionStateResponse {
	return missionStateResponse{
		MissionID:        s.MissionID,
		Cycle:            s.Cycle,
		NetMRRUSD:        s.NetMRRUSD,
		NoProgressCycles: s.NoProgressCycles,
		Confirming:       s.Confirming,
		ConfirmedSince:   s.ConfirmedSince,
		Status:           s.Status,
		Reason:           s.Reason,
		ResultCode:       s.ResultCode,
		ObservedAt:       s.ObservedAt,
	}
}

// missionListItemResponse is a mission plus its latest loop state, snake_case.
// missionResponse's tagged fields are promoted into the object, mirroring the
// domain MissionListItem's own embedding of Mission.
type missionListItemResponse struct {
	missionResponse
	Status     string     `json:"status"`
	Reason     string     `json:"reason"`
	Cycle      int        `json:"cycle"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

func newMissionListItemResponse(item mission.MissionListItem) missionListItemResponse {
	return missionListItemResponse{
		missionResponse: newMissionResponse(item.Mission),
		Status:          item.Status,
		Reason:          item.Reason,
		Cycle:           item.Cycle,
		ObservedAt:      item.ObservedAt,
	}
}

func (s *Server) missionStore() *mission.Store { return mission.NewStore(s.deps.DB) }

// handleListMissions implements GET /v1/missions.
func (s *Server) handleListMissions(w http.ResponseWriter, r *http.Request) {
	// Task 118 (SEC-04) tenancy: a principal lists only its own missions.
	items, err := s.missionStore().ListMissions(r.Context(), mission.MissionFilter{
		Status:      r.URL.Query().Get("status"),
		Profile:     r.URL.Query().Get("profile"),
		PrincipalID: principalFromContext(r.Context()),
	})
	if err != nil {
		s.logger().Error("api: list missions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list missions")
		return
	}
	resp := make([]missionListItemResponse, len(items))
	for i, item := range items {
		resp[i] = newMissionListItemResponse(item)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleMissionStatus implements GET /v1/missions/{id}.
func (s *Server) handleMissionStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store := s.missionStore()
	m, err := store.GetMission(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mission not found")
		return
	}
	resp := map[string]any{"mission": newMissionResponse(m)}
	if st, err := store.LatestState(r.Context(), id); err == nil {
		resp["latest_state"] = newMissionStateResponse(st)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleStartMission implements POST /v1/missions/{id}/start.
func (s *Server) handleStartMission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store := s.missionStore()
	m, err := store.GetMission(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mission not found")
		return
	}
	taskQueue, err := kernel.LaneSelector{}.Select(r.URL.Query().Get("lane"), s.deps.QueueConfig)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid lane: "+err.Error())
		return
	}
	tc, err := client.Dial(client.Options{HostPort: s.deps.TemporalHostPort, Namespace: s.deps.TemporalNamespace})
	if err != nil {
		s.logger().Error("api: dial temporal for mission start failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not reach the workflow service")
		return
	}
	defer tc.Close()

	run, err := tc.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID:        m.WorkflowID,
		TaskQueue: taskQueue,
	}, mission.MissionLoop, mission.MissionLoopInput{
		MissionID:         m.ID,
		Contract:          m.Contract,
		DeliveryTaskQueue: taskQueue,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "could not start mission: "+err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"mission_id": m.ID, "workflow_id": m.WorkflowID, "run_id": run.GetRunID(), "task_queue": taskQueue,
	})
}

// handleResumeMission implements POST /v1/missions/{id}/resume. A mission that
// is not currently WAITING is refused.
func (s *Server) handleResumeMission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store := s.missionStore()
	m, err := store.GetMission(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mission not found")
		return
	}
	if st, err := store.LatestState(r.Context(), id); err == nil && st.Status != "WAITING" {
		writeError(w, http.StatusConflict, "mission is not WAITING — nothing to resume")
		return
	}
	tc, err := client.Dial(client.Options{HostPort: s.deps.TemporalHostPort, Namespace: s.deps.TemporalNamespace})
	if err != nil {
		s.logger().Error("api: dial temporal for mission resume failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not reach the workflow service")
		return
	}
	defer tc.Close()
	if err := tc.SignalWorkflow(r.Context(), m.WorkflowID, "", mission.SignalResumeMission, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "could not resume mission")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"mission_id": m.ID, "workflow_id": m.WorkflowID})
}
