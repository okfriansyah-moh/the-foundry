package integrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// IntegrationItem is one item in the per-branch FIFO push queue.
type IntegrationItem struct {
	ID             string
	Branch         string
	GroupID        string
	ManifestDigest string
	Commits        []string
	ExpectedBase   string // SHA the caller expects to be the current branch head
	EnqueuedAt     time.Time
}

// Receipt is the proof of a successful push: issued after the CAS push
// completes and recorded to the external-operations ledger.
type Receipt struct {
	Branch         string
	BeforeSHA      string
	AfterSHA       string
	GroupID        string
	ManifestDigest string
	IssuedAt       time.Time
}

// ErrStaleFencingToken is returned when the fencing token held by the
// current push operation has been superseded by a newer one. This prevents
// stale writers from making progress.
var ErrStaleFencingToken = errors.New("integrator: stale fencing token — lease superseded")

// ErrDriftDetected is returned when the remote branch head does not match
// expectedBase. The caller should requeue with a fresh expectedBase after
// rebasing (Task 59).
var ErrDriftDetected = errors.New("integrator: drift detected — remote head moved")

// ErrForcePushAttempted is returned if any code path attempts a force push.
// This should never happen; the check is a safety net.
var ErrForcePushAttempted = errors.New("integrator: force push is prohibited (Constitution C4)")

// RemoteReader reads the current head SHA of a branch.
type RemoteReader interface {
	ReadHead(ctx context.Context, branch string) (sha string, err error)
}

// CASPusher pushes commits fast-forward-only with a fencing token.
// ForcePush is intentionally absent from this interface.
type CASPusher interface {
	CASPush(ctx context.Context, branch, expectedBase string, commits []string, fencingToken string) (afterSHA string, err error)
}

// LeaseAcquirer acquires a branch-scoped lease returning a fencing token.
type LeaseAcquirer interface {
	AcquireLease(ctx context.Context, branch, holder string, ttl time.Duration) (token string, err error)
	ReleaseLease(ctx context.Context, branch, token string) error
}

// ReceiptStore persists receipts to the external-operations ledger.
type ReceiptStore interface {
	RecordReceipt(ctx context.Context, receipt Receipt) error
}

// Queue serializes integration items per branch using an in-memory mutex.
// A production implementation replaces this with PG advisory locks.
type Queue struct {
	mu    sync.Mutex
	items map[string][]IntegrationItem // branch → FIFO queue
}

// NewQueue creates an empty Queue.
func NewQueue() *Queue {
	return &Queue{items: make(map[string][]IntegrationItem)}
}

// Enqueue adds an item to the per-branch FIFO queue.
func (q *Queue) Enqueue(item IntegrationItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items[item.Branch] = append(q.items[item.Branch], item)
}

// Dequeue removes and returns the next item for the branch, or (zero, false).
func (q *Queue) Dequeue(branch string) (IntegrationItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := q.items[branch]
	if len(items) == 0 {
		return IntegrationItem{}, false
	}
	next := items[0]
	q.items[branch] = items[1:]
	return next, true
}

// Integrator serializes branch pushes with lease+fencing.
type Integrator struct {
	Leases   LeaseAcquirer
	Reader   RemoteReader
	Pusher   CASPusher
	Receipts ReceiptStore
	LeaseTTL time.Duration
}

// ProcessItem executes one integration item: lease → drift-check → CAS push → receipt.
// The fencing token prevents stale writers; force push is impossible (no API surface).
func (g *Integrator) ProcessItem(ctx context.Context, item IntegrationItem) (Receipt, error) {
	// Step 1: acquire branch lease.
	token, err := g.Leases.AcquireLease(ctx, item.Branch, item.ID, g.LeaseTTL)
	if err != nil {
		return Receipt{}, fmt.Errorf("integrator: acquire lease for branch %q: %w", item.Branch, err)
	}
	defer func() {
		_ = g.Leases.ReleaseLease(ctx, item.Branch, token)
	}()

	// Step 2: fetch remote head.
	remoteHead, err := g.Reader.ReadHead(ctx, item.Branch)
	if err != nil {
		return Receipt{}, fmt.Errorf("integrator: read remote head for branch %q: %w", item.Branch, err)
	}

	// Step 3: drift check.
	if remoteHead != item.ExpectedBase {
		return Receipt{}, fmt.Errorf("%w: branch %q head %q != expected %q",
			ErrDriftDetected, item.Branch, remoteHead, item.ExpectedBase)
	}

	// Step 4+5: CAS push (fast-forward only; force push impossible — no force=true path).
	afterSHA, err := g.Pusher.CASPush(ctx, item.Branch, item.ExpectedBase, item.Commits, token)
	if err != nil {
		if errors.Is(err, ErrStaleFencingToken) {
			return Receipt{}, err
		}
		return Receipt{}, fmt.Errorf("integrator: CAS push to branch %q: %w", item.Branch, err)
	}

	// Step 6: record receipt.
	rec := Receipt{
		Branch:         item.Branch,
		BeforeSHA:      item.ExpectedBase,
		AfterSHA:       afterSHA,
		GroupID:        item.GroupID,
		ManifestDigest: item.ManifestDigest,
		IssuedAt:       time.Now().UTC(),
	}
	if err := g.Receipts.RecordReceipt(ctx, rec); err != nil {
		return rec, fmt.Errorf("integrator: record receipt for branch %q: %w", item.Branch, err)
	}
	return rec, nil
}
