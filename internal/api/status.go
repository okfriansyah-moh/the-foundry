package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/okfriansyah-moh/the-foundry/internal/state"
)

// statusQueryTimeout bounds every DB/Temporal read this handler performs,
// matching cmd/foundry/status.go's own statusCmdTimeout.
const statusQueryTimeout = 30 * time.Second

// consistencyHeader is the response header docs/PLAN.md Task 38 requires:
// every status response states its consistency level ("projected" or
// "fresh") as a header, mirroring the same value the statusResponse JSON
// body's Consistency field already carries — one source of truth
// (statusProjected/statusFresh each set both from the same literal),
// never duplicated consistency-decision logic.
const consistencyHeader = "X-Foundry-Consistency"

// statusResponse is GET /v1/workflows/{id}/status's JSON body. Fields not
// meaningful for the requested consistency level are omitted
// (`omitempty`), mirroring the CLI's two distinct output shapes
// (formatProjected / formatFresh in cmd/foundry/status.go) in one type.
type statusResponse struct {
	WorkflowID     string  `json:"workflow_id"`
	Status         string  `json:"status"`
	Phase          string  `json:"phase"`
	Consistency    string  `json:"consistency"`
	LastSeq        int64   `json:"last_seq,omitempty"`
	LagSeconds     float64 `json:"lag_seconds,omitempty"`
	TemporalStatus string  `json:"temporal_status,omitempty"`
}

// handleWorkflowStatus implements GET /v1/workflows/{id}/status
// (docs/PLAN.md Task 36): ?consistency=projected (default) reads
// workflow_status_projection (Task 14); ?consistency=fresh reads
// Temporal's DescribeWorkflowExecution plus the latest
// workflow_transitions row directly, bypassing the projection —
// identical semantics to `foundry status --fresh`
// (cmd/foundry/status.go), reimplemented here as this task's dogfooded
// server-side source of truth.
func (s *Server) handleWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	workflowID := r.PathValue("id")
	if workflowID == "" {
		writeError(w, http.StatusBadRequest, "missing workflow id")
		return
	}

	switch consistency := r.URL.Query().Get("consistency"); consistency {
	case "", "projected":
		s.statusProjected(w, r, workflowID)
	case "fresh":
		s.statusFresh(w, r, workflowID)
	default:
		writeError(w, http.StatusBadRequest, `consistency must be "fresh" or "projected"`)
	}
}

func (s *Server) statusProjected(w http.ResponseWriter, r *http.Request, workflowID string) {
	ctx, cancel := context.WithTimeout(r.Context(), statusQueryTimeout)
	defer cancel()

	const q = `
SELECT workflow_id, status, phase, last_seq, updated_at
FROM workflow_status_projection
WHERE workflow_id = $1`

	var (
		rowWorkflowID, status, phase string
		lastSeq                      int64
		updatedAt                    time.Time
	)
	err := s.deps.DB.QueryRowContext(ctx, q, workflowID).Scan(&rowWorkflowID, &status, &phase, &lastSeq, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no projection row for this workflow")
		return
	}
	if err != nil {
		s.logger().Error("api: query projection failed", "workflow_id", workflowID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read workflow status")
		return
	}

	// docs/PLAN.md Task 38: every response carries the consistency level
	// it was served at as a header too, not just the JSON body field, so
	// a caller can branch on it (proxies, curl, non-JSON consumers)
	// without parsing the payload.
	w.Header().Set(consistencyHeader, "projected")
	writeJSON(w, http.StatusOK, statusResponse{
		WorkflowID:  rowWorkflowID,
		Status:      status,
		Phase:       phase,
		Consistency: "projected",
		LastSeq:     lastSeq,
		LagSeconds:  projectionLag(updatedAt, time.Now()).Seconds(),
	})
}

func (s *Server) statusFresh(w http.ResponseWriter, r *http.Request, workflowID string) {
	ctx, cancel := context.WithTimeout(r.Context(), statusQueryTimeout)
	defer cancel()

	last, err := queryLastTransition(ctx, s.deps.DB, workflowID)
	if err != nil {
		s.logger().Error("api: query last transition failed", "workflow_id", workflowID, "error", err)
		writeError(w, http.StatusNotFound, "no transitions recorded for this workflow")
		return
	}

	temporalStatus, err := describeTemporalWorkflow(ctx, s.deps.TemporalHostPort, s.deps.TemporalNamespace, workflowID)
	if err != nil {
		s.logger().Error("api: describe temporal workflow failed", "workflow_id", workflowID, "error", err)
		writeError(w, http.StatusBadGateway, "could not reach temporal")
		return
	}

	w.Header().Set(consistencyHeader, "fresh")
	writeJSON(w, http.StatusOK, statusResponse{
		WorkflowID:     workflowID,
		Status:         string(last.Status),
		Phase:          string(last.PhaseTo),
		Consistency:    "fresh",
		TemporalStatus: temporalStatus,
	})
}

// projectionLag mirrors cmd/foundry/status.go's function of the same
// name: how stale a projection row is, floored at zero so clock skew
// between this server and Postgres never reports a negative lag.
func projectionLag(updatedAt, now time.Time) time.Duration {
	if d := now.Sub(updatedAt); d > 0 {
		return d
	}
	return 0
}

// queryLastTransition reads the single latest workflow_transitions row
// for workflowID directly — deliberately not workflow_status_projection —
// mirroring cmd/foundry/status.go's function of the same name.
func queryLastTransition(ctx context.Context, db *sql.DB, workflowID string) (state.Transition, error) {
	const q = `
SELECT payload
FROM workflow_transitions
WHERE workflow_id = $1
ORDER BY seq DESC
LIMIT 1`

	var payload []byte
	err := db.QueryRowContext(ctx, q, workflowID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return state.Transition{}, fmt.Errorf("no transitions recorded for workflow %s", workflowID)
	}
	if err != nil {
		return state.Transition{}, err
	}

	var t state.Transition
	if err := json.Unmarshal(payload, &t); err != nil {
		return state.Transition{}, fmt.Errorf("decode transition: %w", err)
	}
	return t, nil
}

// describeTemporalWorkflow mirrors cmd/foundry/status.go's function of
// the same name: a raw workflowservice gRPC call for
// DescribeWorkflowExecution, returning the execution status string.
func describeTemporalWorkflow(ctx context.Context, hostPort, namespace, workflowID string) (string, error) {
	conn, err := grpc.NewClient(hostPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("dial temporal: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client := workflowservice.NewWorkflowServiceClient(conn)
	resp, err := client.DescribeWorkflowExecution(ctx, &workflowservice.DescribeWorkflowExecutionRequest{
		Namespace: namespace,
		Execution: &commonpb.WorkflowExecution{WorkflowId: workflowID},
	})
	if err != nil {
		return "", fmt.Errorf("DescribeWorkflowExecution: %w", err)
	}
	return resp.GetWorkflowExecutionInfo().GetStatus().String(), nil
}
