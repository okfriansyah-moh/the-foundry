package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TelegramNotifier implements Notifier against the real Telegram Bot API. It is the
// disposable bootstrap bot documented in README (never Foundry's eventual production
// bot) and it is bound by Constitution C11: notify/batch/flood-control/low-risk
// commands/veto digest only — never a substitute for the strong-auth approval C12
// requires once real ApprovedPlan provenance exists (see Task 3 card, Step 5 note).
type TelegramNotifier struct {
	Token           string
	ChatID          string
	PollInterval    time.Duration
	HTTPClient      *http.Client
	baseURL         string
	mu              sync.Mutex
	digest          []*Card
	lastFlush       time.Time
	digestEvery     int
	digestMaxWait   time.Duration
	frozen          bool
	updateOffset    int
	approvals       map[int]bool // task -> approved
	approvalWaiters map[int]chan bool
}

// NewTelegramNotifier builds a client for the bootstrap bot named by token/chatID
// (Task 3 Outputs: ".env entry for a disposable bootstrap Telegram bot token").
func NewTelegramNotifier(token, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		Token:           token,
		ChatID:          chatID,
		PollInterval:    5 * time.Second,
		HTTPClient:      &http.Client{Timeout: 30 * time.Second},
		baseURL:         "https://api.telegram.org/bot" + token,
		digestEvery:     defaultDigestEvery,
		digestMaxWait:   2 * time.Hour,
		lastFlush:       time.Now().UTC(),
		approvals:       map[int]bool{},
		approvalWaiters: map[int]chan bool{},
	}
}

func (t *TelegramNotifier) sendMessage(ctx context.Context, text string) error {
	form := url.Values{}
	form.Set("chat_id", t.ChatID)
	form.Set("text", text)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/sendMessage", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram sendMessage returned status %d", resp.StatusCode)
	}
	return nil
}

// NotifyGated sends the blocking gate message (Task 3 Step 5): task, changed files
// aren't known yet at this layer so the card body + reason + validation output stand
// in for them, per the card's own wording ("naming the task ... and the exact
// Risk/Rev reason it's gated").
func (t *TelegramNotifier) NotifyGated(ctx context.Context, card *Card, reason, validationOutput string) error {
	msg := fmt.Sprintf(
		"GATED: Task %d (%s) — %s\nReason: %s\nValidation:\n%s\nReply /approve %d or /reject %d",
		card.Task, card.Alias, card.Title, reason, truncate(validationOutput, 2000), card.Task, card.Task,
	)
	return t.sendMessage(ctx, msg)
}

// NotifyHalt sends the blocking halt alert (Task 3 Step 4: two consecutive failures
// halt the entire runner, never a silent retry loop).
func (t *TelegramNotifier) NotifyHalt(ctx context.Context, card *Card, reason string) error {
	msg := fmt.Sprintf("HALTED: Task %d (%s) — %s\nRunner stopped: %s", card.Task, card.Alias, card.Title, reason)
	return t.sendMessage(ctx, msg)
}

// QueueDigest buffers an AUTO completion for the batched, non-blocking digest
// (Task 3 Step 6).
func (t *TelegramNotifier) QueueDigest(card *Card) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.digest = append(t.digest, card)
}

// FlushDigest sends the queued AUTO completions once the batch reaches digestEvery or
// digestMaxWait has elapsed since the last flush (Task 3 Steps 6-7: the digest batch
// size doubles as the drift cap).
func (t *TelegramNotifier) FlushDigest(ctx context.Context) error {
	t.mu.Lock()
	due := len(t.digest) >= t.digestEvery || (len(t.digest) > 0 && time.Since(t.lastFlush) >= t.digestMaxWait)
	if !due {
		t.mu.Unlock()
		return nil
	}
	batch := t.digest
	t.digest = nil
	t.lastFlush = time.Now().UTC()
	t.mu.Unlock()

	var b strings.Builder
	b.WriteString(fmt.Sprintf("AUTO digest: %d completion(s)\n", len(batch)))
	for _, card := range batch {
		fmt.Fprintf(&b, "- Task %d (%s): %s\n", card.Task, card.Alias, card.Title)
	}
	return t.sendMessage(ctx, b.String())
}

// telegramUpdate is the minimal subset of the Bot API getUpdates response this runner
// needs: plain-text commands in a known chat.
type telegramUpdate struct {
	UpdateID int `json:"update_id"`
	Message  struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

type telegramGetUpdatesResponse struct {
	OK     bool             `json:"ok"`
	Result []telegramUpdate `json:"result"`
}

func (t *TelegramNotifier) pollOnce(ctx context.Context) error {
	q := url.Values{}
	q.Set("offset", strconv.Itoa(t.updateOffset))
	q.Set("timeout", "0")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL+"/getUpdates?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build getUpdates request: %w", err)
	}
	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("getUpdates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed telegramGetUpdatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("decode getUpdates response: %w", err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	for _, u := range parsed.Result {
		if u.UpdateID >= t.updateOffset {
			t.updateOffset = u.UpdateID + 1
		}
		// Only the configured chat may /approve, /reject, or /freeze (OWASP A01 Broken
		// Access Control): otherwise any user who can message this bot could approve a
		// High-risk gated task, defeating the whole point of the Telegram gate.
		if strconv.FormatInt(u.Message.Chat.ID, 10) != t.ChatID {
			continue
		}
		t.handleCommand(u.Message.Text)
	}
	return nil
}

// handleCommand parses /approve <id>, /reject <id>, and /freeze. Caller holds t.mu.
func (t *TelegramNotifier) handleCommand(text string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "/freeze":
		t.frozen = true
	case "/approve":
		if len(fields) < 2 {
			return
		}
		if id, err := strconv.Atoi(fields[1]); err == nil {
			t.resolveLocked(id, true)
		}
	case "/reject":
		if len(fields) < 2 {
			return
		}
		if id, err := strconv.Atoi(fields[1]); err == nil {
			t.resolveLocked(id, false)
		}
	}
}

func (t *TelegramNotifier) resolveLocked(task int, approved bool) {
	t.approvals[task] = approved
	if ch, ok := t.approvalWaiters[task]; ok {
		ch <- approved
		delete(t.approvalWaiters, task)
	}
}

// WaitApproval blocks until a nonce-bound /approve <task> or /reject <task> arrives, or
// ctx is cancelled. Per Task 3 Step 5, there is no auto-timeout-to-reject: "no reply ⇒
// stays paused; never auto-approves" — the caller controls how long it waits via ctx.
func (t *TelegramNotifier) WaitApproval(ctx context.Context, card *Card) (bool, error) {
	t.mu.Lock()
	if approved, ok := t.approvals[card.Task]; ok {
		delete(t.approvals, card.Task)
		t.mu.Unlock()
		return approved, nil
	}
	ch := make(chan bool, 1)
	t.approvalWaiters[card.Task] = ch
	t.mu.Unlock()

	ticker := time.NewTicker(t.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case approved := <-ch:
			return approved, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			if err := t.pollOnce(ctx); err != nil {
				slog.Warn("telegram poll failed", "error", err)
			}
		}
	}
}

// Frozen reports whether a /freeze command has been received (Task 3 Step 7).
func (t *TelegramNotifier) Frozen(ctx context.Context) bool {
	if err := t.pollOnce(ctx); err != nil {
		slog.Warn("telegram poll failed", "error", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.frozen
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "... (truncated)"
}
