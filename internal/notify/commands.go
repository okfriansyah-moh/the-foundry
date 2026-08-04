package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/evolve"
)

// NonceTTL is the fixed lifetime of a command nonce (docs/PLAN.md Task 30
// Steps: "per-command nonce ... TTL 10m").
const NonceTTL = 10 * time.Minute

var (
	// ErrUnknownNonce is returned when a nonce was never issued (or has
	// already been garbage-collected).
	ErrUnknownNonce = errors.New("notify: unknown nonce")
	// ErrNonceAlreadyUsed is returned on nonce replay — a nonce is
	// single-use.
	ErrNonceAlreadyUsed = errors.New("notify: nonce already used")
	// ErrNonceExpired is returned once NonceTTL has elapsed since issue.
	ErrNonceExpired = errors.New("notify: nonce expired")
	// ErrNonceMismatch is returned when a valid, unused, unexpired nonce
	// is presented for a different chat or workflow than it was issued
	// for.
	ErrNonceMismatch = errors.New("notify: nonce does not match chat/workflow")
	// ErrUnknownChat is returned when a command arrives from a chat id
	// that is not registered to any principal — Task 30's Acceptance
	// criterion "unknown chat rejected".
	ErrUnknownChat = errors.New("notify: unrecognized chat — not registered to a principal")
)

type nonceEntry struct {
	chatID    string
	workflow  string
	expiresAt time.Time
	used      bool
}

// NonceRegistry issues and single-use-validates per-command nonces
// (docs/PLAN.md Task 30 Steps). Nonces are issued embedded in an
// outbound message and must be echoed back in the operator's command.
type NonceRegistry struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]*nonceEntry
}

// NewNonceRegistry constructs a NonceRegistry using NonceTTL.
func NewNonceRegistry() *NonceRegistry {
	return &NonceRegistry{ttl: NonceTTL, now: time.Now, entries: make(map[string]*nonceEntry)}
}

// Issue mints a new random nonce scoped to chatID+workflow, valid for
// ttl from now.
func (r *NonceRegistry) Issue(chatID, workflow string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("notify: generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(buf)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[nonce] = &nonceEntry{
		chatID:    chatID,
		workflow:  workflow,
		expiresAt: r.now().Add(r.ttl),
	}
	return nonce, nil
}

// Consume validates nonce for chatID+workflow and marks it used. A
// second Consume call for the same nonce (replay) returns
// ErrNonceAlreadyUsed; a call after NonceTTL has elapsed returns
// ErrNonceExpired.
func (r *NonceRegistry) Consume(nonce, chatID, workflow string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.entries[nonce]
	if !ok {
		return ErrUnknownNonce
	}
	if e.used {
		return ErrNonceAlreadyUsed
	}
	if r.now().After(e.expiresAt) {
		return ErrNonceExpired
	}
	if e.chatID != chatID || e.workflow != workflow {
		return ErrNonceMismatch
	}
	e.used = true
	return nil
}

// Validate checks whether nonce is valid for chatID+workflow without consuming
// it. Returns the same sentinel errors as Consume. Use Validate before
// performing the side effect, then Consume after success to preserve the retry
// path on side-effect failure.
func (r *NonceRegistry) Validate(nonce, chatID, workflow string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, ok := r.entries[nonce]
	if !ok {
		return ErrUnknownNonce
	}
	if e.used {
		return ErrNonceAlreadyUsed
	}
	if r.now().After(e.expiresAt) {
		return ErrNonceExpired
	}
	if e.chatID != chatID || e.workflow != workflow {
		return ErrNonceMismatch
	}
	return nil
}

// ChatRegistry binds chat ids to principals. An unregistered chat id
// cannot issue any command (Task 30's Acceptance: "unknown chat
// rejected").
type ChatRegistry struct {
	mu   sync.RWMutex
	byID map[string]string
}

// NewChatRegistry constructs an empty ChatRegistry.
func NewChatRegistry() *ChatRegistry {
	return &ChatRegistry{byID: make(map[string]string)}
}

// Register binds chatID to principal.
func (c *ChatRegistry) Register(chatID, principal string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID[chatID] = principal
}

// Principal returns chatID's bound principal, or ok=false if chatID is
// not registered.
func (c *ChatRegistry) Principal(chatID string) (principal string, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	principal, ok = c.byID[chatID]
	return principal, ok
}

// WorkflowController is the injected seam through which the command
// router forwards a validated /status, /pause, or /resume command.
// Constitution C4: this package never performs the side effect itself —
// whatever kernel-side code constructs a CommandRouter supplies the
// controller that actually signals the workflow.
type WorkflowController interface {
	Status(ctx context.Context, workflow string) (string, error)
	Pause(ctx context.Context, workflow string) error
	Resume(ctx context.Context, workflow string) error
}

// CommandRouter parses and dispatches the Telegram commands this engine
// supports.
type CommandRouter struct {
	Chats      *ChatRegistry
	Nonces     *NonceRegistry
	Controller WorkflowController
	// Veto handles /rollback <promo-id> <nonce> commands (Task 52 / VEN-13).
	Veto VetoExecutor
	// FreezeEvolution durably freezes autonomous skill promotion before the
	// process-local hot-path latch is mirrored. Production must provide this
	// callback; keeping the side effect injected preserves Constitution C4.
	FreezeEvolution func(context.Context, evolve.FreezeCondition) error

	// ResolvePlanContext and SecureSurfaceURL wire /approve to Task 25's
	// existing C11 guard (internal/authn.TelegramApprove) rather than
	// reimplementing it.
	ResolvePlanContext authn.PlanContextResolver
	SecureSurfaceURL   authn.SecureSurfaceURLFunc

	// Idea wires the /idea and /confirm low-risk intake commands (Task 113 /
	// INT-05). Optional: nil disables intake-by-message.
	Idea *IdeaCommand
}

// Handle parses and dispatches one command line (e.g. "/pause flow-123
// <nonce>") received from chatID, returning the reply text to send back.
func (r *CommandRouter) Handle(ctx context.Context, chatID, text string) string {
	if _, ok := r.Chats.Principal(chatID); !ok {
		return ErrUnknownChat.Error()
	}

	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "empty command"
	}
	name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	args := fields[1:]

	switch name {
	case "status":
		return r.handleNonced(ctx, chatID, args, r.Controller.Status)
	case "pause":
		return r.handleNonced(ctx, chatID, args, func(ctx context.Context, wf string) (string, error) {
			return "paused", r.Controller.Pause(ctx, wf)
		})
	case "resume":
		return r.handleNonced(ctx, chatID, args, func(ctx context.Context, wf string) (string, error) {
			return "resumed", r.Controller.Resume(ctx, wf)
		})
	case "freeze":
		return r.handleFreeze(ctx, args)
	case "approve":
		return r.handleApprove(ctx, args)
	case "rollback":
		return r.handleRollback(ctx, chatID, args)
	case "idea":
		// Task 113: a free-text idea becomes a mission draft. The text is data,
		// never an instruction.
		if r.Idea == nil {
			return "idea intake is not enabled"
		}
		principal, _ := r.Chats.Principal(chatID)
		return r.Idea.HandleIdea(ctx, chatID, principal, strings.Join(args, " "))
	case "confirm":
		if r.Idea == nil {
			return "idea intake is not enabled"
		}
		return r.Idea.HandleConfirm(ctx, chatID, args)
	default:
		return fmt.Sprintf("unknown command: /%s", name)
	}
}

// handleNonced validates the shared "<workflow> <nonce>" argument shape
// used by /status, /pause, and /resume, consumes the nonce exactly once,
// then invokes action.
func (r *CommandRouter) handleNonced(ctx context.Context, chatID string, args []string, action func(context.Context, string) (string, error)) string {
	if len(args) != 2 {
		return "usage: /<command> <workflow> <nonce>"
	}
	workflow, nonce := args[0], args[1]

	if err := r.Nonces.Consume(nonce, chatID, workflow); err != nil {
		return err.Error()
	}
	reply, err := action(ctx, workflow)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return reply
}

// handleApprove implements Constitution C11's "Telegram never approves
// high-risk actions" by delegating to Task 25's already-built guard
// (internal/authn.TelegramApprove) and returning only its Reply text.
// This function performs no further action regardless of the guard's
// Allowed value — it never calls provenance.Store.AddApprover or any
// other approval side effect — so no approval, high-risk or otherwise,
// can ever complete through this command router.
func (r *CommandRouter) handleApprove(ctx context.Context, args []string) string {
	if len(args) != 1 {
		return "usage: /approve <plan-id>"
	}
	planID := args[0]

	if r.ResolvePlanContext == nil || r.SecureSurfaceURL == nil {
		return "approve is not wired to the approval gate"
	}
	planCtx, err := r.ResolvePlanContext(ctx, planID)
	if err != nil {
		return fmt.Sprintf("error resolving plan %s: %v", planID, err)
	}
	result := authn.TelegramApprove(planID, planCtx, r.SecureSurfaceURL)
	return result.Reply
}

func (r *CommandRouter) handleFreeze(ctx context.Context, args []string) string {
	if len(args) != 0 {
		return "usage: /freeze"
	}
	if r.FreezeEvolution != nil {
		if err := r.FreezeEvolution(ctx, evolve.FreezeBudgetExceeded); err != nil {
			return fmt.Sprintf("error: freeze evolution: %v", err)
		}
	}
	evolve.MirrorDurableFreeze(evolve.FreezeBudgetExceeded)
	return "frozen"
}
