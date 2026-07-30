package api

import (
	"net/http"

	"go.temporal.io/sdk/client"

	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

// docs/PLAN.md Task 107 (RTC-03): mission operational surface over HTTP —
// list, status, start and resume. Transport only: the kernel owns the workflow
// (Constitution C4). Each route is behind the same PDP middleware as every
// other internal/api route.

func (s *Server) missionStore() *mission.Store { return mission.NewStore(s.deps.DB) }

// handleListMissions implements GET /v1/missions.
func (s *Server) handleListMissions(w http.ResponseWriter, r *http.Request) {
	items, err := s.missionStore().ListMissions(r.Context(), mission.MissionFilter{
		Status:  r.URL.Query().Get("status"),
		Profile: r.URL.Query().Get("profile"),
	})
	if err != nil {
		s.logger().Error("api: list missions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list missions")
		return
	}
	writeJSON(w, http.StatusOK, items)
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
	resp := map[string]any{"mission": m}
	if st, err := store.LatestState(r.Context(), id); err == nil {
		resp["latest_state"] = st
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
