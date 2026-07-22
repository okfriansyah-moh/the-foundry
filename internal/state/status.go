package state

// Status is the canonical workflow status. Constitution C1 fixes this set at
// exactly six members; all richer meaning lives in Phase, Reason, and
// ResultCode, never in a new Status value.
type Status string

// The six canonical statuses (state-model.md §1).
const (
	StatusPending   Status = "PENDING"
	StatusRunning   Status = "RUNNING"
	StatusWaiting   Status = "WAITING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"
)

// String returns the canonical wire representation.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether s is a terminal status (SUCCEEDED, FAILED, or
// CANCELLED) that absorbs all further transitions.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// Phase is a registry-controlled field describing what work is happening
// while Status is RUNNING (state-model.md §2).
type Phase string

// Reason is a registry-controlled field describing why a workflow is WAITING
// or FAILED (state-model.md §2).
type Reason string

// ResultCode is a registry-controlled field carrying terminal outcome detail
// (state-model.md §2).
type ResultCode string

// String returns the canonical wire representation.
func (p Phase) String() string { return string(p) }

// String returns the canonical wire representation.
func (r Reason) String() string { return string(r) }

// String returns the canonical wire representation. The deprecated alias
// TEN_X_BRANCHES_READY is never emitted here — see alias.go.
func (c ResultCode) String() string { return string(c) }
