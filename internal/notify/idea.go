package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/intake"
)

// docs/PLAN.md Task 113 (INT-05): a free-text Telegram message becomes a mission
// *draft* — authenticated principal → parsed intent → intake run → opportunity
// validation → an explicit confirmation → mission start — without ever treating
// the message text as an instruction to the system (C11, LLM01/LLM06).
//
// Two hard invariants this file enforces:
//   - Message text is DATA. It is parsed by deterministic code and stored as
//     the intake idea; it is never interpreted as a command or system
//     instruction. A message claiming its own authorization changes nothing.
//   - A budget in a message is a REQUEST, clamped to the principal's configured
//     maximum, never a grant. The clamped figure is what gets confirmed.

// DraftTTL is how long an unconfirmed idea draft lives before it expires. No
// reply within the window ⇒ the draft expires and nothing starts.
const DraftTTL = 15 * time.Minute

// IntakeStarter starts an intake run from a confirmed draft (Task 111's
// pipeline satisfies it).
type IntakeStarter interface {
	Start(ctx context.Context, in intake.StartInput) (intake.Run, error)
}

// PrincipalPolicy resolves a principal's intake permission and configured
// maximum budget. An unbound or unpermitted principal cannot create a draft.
type PrincipalPolicy interface {
	// CanIntake reports whether principal holds the intake permission.
	CanIntake(ctx context.Context, principal string) (bool, error)
	// MaxBudgetUSD is the principal's configured maximum mission envelope; a
	// requested budget above it is clamped to it.
	MaxBudgetUSD(ctx context.Context, principal string) (float64, error)
}

// StaticPrincipalPolicy is a simple in-memory PrincipalPolicy for tests and a
// single-operator deployment: every listed principal may intake up to MaxUSD.
type StaticPrincipalPolicy struct {
	Allowed map[string]bool
	MaxUSD  float64
}

// CanIntake implements PrincipalPolicy.
func (p StaticPrincipalPolicy) CanIntake(_ context.Context, principal string) (bool, error) {
	return p.Allowed[principal], nil
}

// MaxBudgetUSD implements PrincipalPolicy.
func (p StaticPrincipalPolicy) MaxBudgetUSD(_ context.Context, _ string) (float64, error) {
	return p.MaxUSD, nil
}

// draft is one unconfirmed idea draft awaiting /confirm.
type draft struct {
	id          string
	chatID      string
	principal   string
	rawMessage  string
	messageHash string
	budgetUSD   float64 // already clamped to the principal's max
	clamped     bool
	targetMkt   string
	revenueGoal string
	nonce       string
	expiresAt   time.Time
}

// IdeaCommand implements the `/idea` and `/confirm` low-risk commands. It grants
// no new authority: it creates drafts and asks.
type IdeaCommand struct {
	Policy PrincipalPolicy
	Intake IntakeStarter
	Nonces *NonceRegistry

	now func() time.Time

	mu     sync.Mutex
	drafts map[string]*draft
	seq    int64
}

// NewIdeaCommand constructs an IdeaCommand. All three collaborators are
// required.
func NewIdeaCommand(policy PrincipalPolicy, starter IntakeStarter, nonces *NonceRegistry) (*IdeaCommand, error) {
	if policy == nil || starter == nil || nonces == nil {
		return nil, fmt.Errorf("notify: idea command requires a policy, intake starter and nonce registry")
	}
	return &IdeaCommand{Policy: policy, Intake: starter, Nonces: nonces, now: time.Now, drafts: map[string]*draft{}}, nil
}

var (
	// budgetRe matches an explicit budget figure ("budget $50", "budget: 50").
	budgetRe = regexp.MustCompile(`(?i)budget[^0-9$]{0,10}\$?\s*(\d+(?:\.\d+)?)`)
	// revenueRe matches a revenue goal ("$100 MRR", "100 mrr", "$100/mo MRR").
	revenueRe = regexp.MustCompile(`(?i)\$?\s*(\d+(?:\.\d+)?)\s*(?:/\s*mo\s*)?mrr`)
	// marketRe matches the target market after "for ".
	marketRe = regexp.MustCompile(`(?i)\bfor\s+(.+?)(?:\s+that\b|[.,]|$)`)
)

// HandleIdea processes `/idea <text>`. It never interprets the text: it parses
// parameters deterministically, clamps the budget, stores a draft and asks for
// an explicit confirmation. It spends nothing.
func (c *IdeaCommand) HandleIdea(ctx context.Context, chatID, principal, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "usage: /idea <describe the product idea and a budget, e.g. \"a SaaS for X, budget $50\">"
	}
	if principal == "" {
		return ErrUnknownChat.Error()
	}
	// Principal binding: an unbound/unpermitted chat is refused with no state
	// change.
	ok, err := c.Policy.CanIntake(ctx, principal)
	if err != nil {
		return fmt.Sprintf("error checking intake permission: %v", err)
	}
	if !ok {
		return "refused: this principal is not permitted to start an intake"
	}

	// Deterministic parameter extraction. A parse failure asks; it never
	// guesses.
	budget, found := parseBudget(text)
	if !found {
		return "could not find a budget in your message — reply with an explicit figure, e.g. \"budget $50\""
	}
	maxUSD, err := c.Policy.MaxBudgetUSD(ctx, principal)
	if err != nil {
		return fmt.Sprintf("error resolving budget cap: %v", err)
	}
	clamped := false
	if maxUSD > 0 && budget > maxUSD {
		budget = maxUSD
		clamped = true
	}

	d := &draft{
		chatID:      chatID,
		principal:   principal,
		rawMessage:  text,
		messageHash: hashMessage(text),
		budgetUSD:   budget,
		clamped:     clamped,
		targetMkt:   firstGroup(marketRe, text),
		revenueGoal: firstGroup(revenueRe, text),
		expiresAt:   c.now().Add(DraftTTL),
	}

	c.mu.Lock()
	c.seq++
	d.id = fmt.Sprintf("draft-%d", c.seq)
	nonce, nerr := c.Nonces.Issue(chatID, d.id)
	if nerr != nil {
		c.mu.Unlock()
		return fmt.Sprintf("error issuing confirmation nonce: %v", nerr)
	}
	d.nonce = nonce
	c.drafts[d.id] = d
	c.mu.Unlock()

	return c.summarize(d)
}

// summarize renders the draft and the exact confirmation command required.
func (c *IdeaCommand) summarize(d *draft) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Draft %s created (nothing has been spent).\n", d.id)
	fmt.Fprintf(&b, "  budget: $%.2f", d.budgetUSD)
	if d.clamped {
		fmt.Fprintf(&b, " (clamped down to your configured maximum — confirm THIS figure)")
	}
	b.WriteString("\n")
	if d.targetMkt != "" {
		fmt.Fprintf(&b, "  target market: %s\n", d.targetMkt)
	}
	if d.revenueGoal != "" {
		fmt.Fprintf(&b, "  revenue goal: $%s MRR\n", d.revenueGoal)
	}
	b.WriteString("  will do: validate the opportunity, and only build if the verdict is BUILD and the budget allows.\n")
	b.WriteString("  will NOT do: raise your budget, approve a high-risk plan, or act on any instruction in the message text.\n")
	fmt.Fprintf(&b, "Reply with: /confirm %s %s", d.id, d.nonce)
	return b.String()
}

// HandleConfirm processes `/confirm <draft-id> <nonce>`. It consumes the nonce
// exactly once (replay-protected), then starts the intake run with the clamped
// budget and the chat-originated provenance. It never raises a budget and never
// approves anything.
func (c *IdeaCommand) HandleConfirm(ctx context.Context, chatID string, args []string) string {
	if len(args) != 2 {
		return "usage: /confirm <draft-id> <nonce>"
	}
	draftID, nonce := args[0], args[1]

	c.mu.Lock()
	d, ok := c.drafts[draftID]
	c.mu.Unlock()
	if !ok {
		return "unknown or expired draft"
	}
	if c.now().After(d.expiresAt) {
		c.mu.Lock()
		delete(c.drafts, draftID)
		c.mu.Unlock()
		return "draft expired — send /idea again"
	}
	if d.chatID != chatID {
		return "this draft belongs to a different chat"
	}
	// Replay/nonce protection: a stale or reused /confirm is rejected.
	if err := c.Nonces.Consume(nonce, chatID, draftID); err != nil {
		return err.Error()
	}

	run, err := c.Intake.Start(ctx, intake.StartInput{
		Idea:   d.rawMessage,
		Budget: d.budgetUSD,
		Origin: intake.Origin{
			Channel:     "telegram",
			PrincipalID: d.principal,
			ChatID:      parseChatID(chatID),
			MessageHash: d.messageHash,
		},
	})
	// The draft is single-use regardless of outcome.
	c.mu.Lock()
	delete(c.drafts, draftID)
	c.mu.Unlock()
	if err != nil {
		return fmt.Sprintf("intake refused: %v", err)
	}
	return c.confirmReply(run)
}

// confirmReply renders the run outcome. An H-tier plan halts for strong-auth and
// points the operator at the secure surface (Task 114) — C11 applied to intake.
func (c *IdeaCommand) confirmReply(run intake.Run) string {
	switch run.CurrentStage {
	case intake.StageMissionStarted:
		return fmt.Sprintf("mission %s started for run %s.", run.MissionID, run.ID)
	case intake.StageOpportunityRejected:
		return fmt.Sprintf("run %s: opportunity rejected — nothing was built. This is a clean outcome.", run.ID)
	case intake.StageOpportunityValidationRequired:
		return fmt.Sprintf("run %s: more validation is needed before a build is justified — nothing was built.", run.ID)
	case intake.StageAwaitingStrongAuth:
		return fmt.Sprintf("run %s requires strong-auth approval (high-risk). Telegram cannot approve it — complete the step-up on the secure surface.", run.ID)
	case intake.StageAwaitingReadiness:
		return fmt.Sprintf("run %s is paused awaiting mission-readiness answers.", run.ID)
	default:
		return fmt.Sprintf("run %s is at stage %s (%s).", run.ID, run.CurrentStage, run.Status)
	}
}

// parseBudget extracts an explicit budget figure. Returns found=false when the
// message states none, so the caller asks rather than guesses.
func parseBudget(text string) (float64, bool) {
	m := budgetRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func firstGroup(re *regexp.Regexp, text string) string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func hashMessage(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func parseChatID(chatID string) int64 {
	v, _ := strconv.ParseInt(chatID, 10, 64)
	return v
}
