package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

// SourceStripeTestMode marks reconciled rows that prove the Stripe test-mode path, not earned revenue.
const SourceStripeTestMode = "stripe_test_mode"

// ReconciliationInput is the normalized revenue sample used to compute net MRR.
type ReconciliationInput struct {
	SubscriptionsUSD float64
	RefundsUSD       float64
	CancellationsUSD float64
	DiscountsUSD     float64
	At               time.Time
}

// ReconciliationRow is one persisted revenue_reconciliation record.
type ReconciliationRow struct {
	ID               string
	Source           string
	NetMRRUSD        float64
	SubscriptionsUSD float64
	RefundsUSD       float64
	CancellationsUSD float64
	DiscountsUSD     float64
	At               time.Time
}

// Reconcile computes net MRR from normalized revenue components.
func Reconcile(in ReconciliationInput) ReconciliationRow {
	return ReconciliationRow{
		NetMRRUSD:        in.SubscriptionsUSD - in.RefundsUSD - in.CancellationsUSD - in.DiscountsUSD,
		SubscriptionsUSD: in.SubscriptionsUSD,
		RefundsUSD:       in.RefundsUSD,
		CancellationsUSD: in.CancellationsUSD,
		DiscountsUSD:     in.DiscountsUSD,
		At:               in.At.UTC(),
		Source:           SourceStripeTestMode,
	}
}

// RevenueProvider is the Stripe read surface the reconciler needs.
type RevenueProvider interface {
	ListBalanceTransactions(ctx context.Context, since, until time.Time) ([]BalanceTransaction, error)
	ListSubscriptions(ctx context.Context, testClockID string) ([]Subscription, error)
}

// RevenueStore persists and reads reconciled revenue rows.
type RevenueStore struct {
	db *sql.DB
}

// NewRevenueStore wraps db as the revenue reconciliation store.
func NewRevenueStore(db *sql.DB) *RevenueStore { return &RevenueStore{db: db} }

// Insert writes one reconciliation row idempotently by row ID.
func (s *RevenueStore) Insert(ctx context.Context, row ReconciliationRow) (ReconciliationRow, error) {
	if s == nil || s.db == nil {
		return ReconciliationRow{}, fmt.Errorf("billing: revenue store DB is required")
	}
	if row.At.IsZero() {
		row.At = time.Now().UTC()
	} else {
		row.At = row.At.UTC()
	}
	if row.ID == "" {
		row.ID = fmt.Sprintf("rev_stripe_%d", row.At.UnixNano())
	}
	if row.Source == "" {
		row.Source = SourceStripeTestMode
	}

	const q = `
INSERT INTO revenue_reconciliation
    (id, at, subscriptions_usd, refunds_usd, cancellations_usd, discounts_usd, net_mrr_usd, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE SET
    at = EXCLUDED.at,
    subscriptions_usd = EXCLUDED.subscriptions_usd,
    refunds_usd = EXCLUDED.refunds_usd,
    cancellations_usd = EXCLUDED.cancellations_usd,
    discounts_usd = EXCLUDED.discounts_usd,
    net_mrr_usd = EXCLUDED.net_mrr_usd,
    source = EXCLUDED.source
RETURNING id, source, at, subscriptions_usd, refunds_usd, cancellations_usd, discounts_usd, net_mrr_usd`

	var got ReconciliationRow
	err := s.db.QueryRowContext(ctx, q, row.ID, row.At, row.SubscriptionsUSD, row.RefundsUSD, row.CancellationsUSD, row.DiscountsUSD, row.NetMRRUSD, row.Source).
		Scan(&got.ID, &got.Source, &got.At, &got.SubscriptionsUSD, &got.RefundsUSD, &got.CancellationsUSD, &got.DiscountsUSD, &got.NetMRRUSD)
	if err != nil {
		return ReconciliationRow{}, fmt.Errorf("billing: insert revenue reconciliation: %w", err)
	}
	got.At = got.At.UTC()
	return got, nil
}

// Latest loads the newest Stripe test-mode reconciliation row at or before at.
func (s *RevenueStore) Latest(ctx context.Context, at time.Time) (ReconciliationRow, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	const q = `
SELECT id, source, at, subscriptions_usd, refunds_usd, cancellations_usd, discounts_usd, net_mrr_usd
FROM revenue_reconciliation
WHERE source = $1 AND at <= $2
ORDER BY at DESC
LIMIT 1`
	var row ReconciliationRow
	err := s.db.QueryRowContext(ctx, q, SourceStripeTestMode, at.UTC()).
		Scan(&row.ID, &row.Source, &row.At, &row.SubscriptionsUSD, &row.RefundsUSD, &row.CancellationsUSD, &row.DiscountsUSD, &row.NetMRRUSD)
	if errors.Is(err, sql.ErrNoRows) {
		return ReconciliationRow{}, sql.ErrNoRows
	}
	if err != nil {
		return ReconciliationRow{}, fmt.Errorf("billing: load latest revenue reconciliation: %w", err)
	}
	row.At = row.At.UTC()
	return row, nil
}

// Reconciler pulls Stripe provider state and writes revenue reconciliation rows.
type Reconciler struct {
	provider    RevenueProvider
	store       *RevenueStore
	since       time.Time
	testClockID string
	now         func() time.Time
}

// ReconcilerOption configures a Reconciler.
type ReconcilerOption func(*Reconciler)

// WithReconcilerSince sets the lower bound for balance transaction reconciliation.
func WithReconcilerSince(since time.Time) ReconcilerOption {
	return func(r *Reconciler) { r.since = since.UTC() }
}

// WithTestClock scopes subscription listing to a Stripe test clock.
func WithTestClock(id string) ReconcilerOption {
	return func(r *Reconciler) { r.testClockID = id }
}

// WithReconcilerNow overrides the clock for deterministic tests.
func WithReconcilerNow(now func() time.Time) ReconcilerOption {
	return func(r *Reconciler) { r.now = now }
}

// NewReconciler builds the foundryd-schedulable reconciliation entrypoint.
func NewReconciler(provider RevenueProvider, store *RevenueStore, opts ...ReconcilerOption) *Reconciler {
	r := &Reconciler{provider: provider, store: store, now: func() time.Time { return time.Now().UTC() }}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run performs one reconciliation pass and writes a revenue_reconciliation row.
func (r *Reconciler) Run(ctx context.Context) error {
	if r == nil || r.provider == nil {
		return fmt.Errorf("billing: reconciler provider is required")
	}
	if r.store == nil {
		return fmt.Errorf("billing: reconciler revenue store is required")
	}
	now := r.now().UTC()
	since := r.since
	if since.IsZero() {
		since = now.Add(-24 * time.Hour)
	}

	txs, err := r.provider.ListBalanceTransactions(ctx, since, now)
	if err != nil {
		return fmt.Errorf("billing: reconcile balance transactions: %w", err)
	}
	subs, err := r.provider.ListSubscriptions(ctx, r.testClockID)
	if err != nil {
		return fmt.Errorf("billing: reconcile subscriptions: %w", err)
	}

	row := buildReconciliationRow(subs, txs, now)
	if _, err := r.store.Insert(ctx, row); err != nil {
		return err
	}
	return nil
}

func buildReconciliationRow(subs []Subscription, txs []BalanceTransaction, at time.Time) ReconciliationRow {
	var subscriptionsUSD, refundsUSD, cancellationsUSD, discountsUSD float64
	for _, sub := range subs {
		if sub.Status != "active" && sub.Status != "trialing" {
			continue
		}
		for _, item := range sub.Items {
			if strings.ToLower(item.Currency) != "usd" {
				continue
			}
			subscriptionsUSD += monthlyCents(item) / 100
		}
	}
	for _, tx := range txs {
		if strings.ToLower(tx.Currency) != "usd" {
			continue
		}
		amountUSD := absCents(tx.Net) / 100
		switch strings.ToLower(tx.Type) {
		case "refund", "payment_refund", "refund_failure":
			refundsUSD += amountUSD
		case "adjustment", "payment_reversal":
			cancellationsUSD += amountUSD
		case "credit_note":
			discountsUSD += amountUSD
		}
	}
	return Reconcile(ReconciliationInput{
		SubscriptionsUSD: subscriptionsUSD,
		RefundsUSD:       refundsUSD,
		CancellationsUSD: cancellationsUSD,
		DiscountsUSD:     discountsUSD,
		At:               at,
	})
}

func monthlyCents(item SubscriptionItem) float64 {
	quantity := item.Quantity
	if quantity == 0 {
		quantity = 1
	}
	intervalCount := item.IntervalCount
	if intervalCount == 0 {
		intervalCount = 1
	}
	base := float64(item.UnitAmount * quantity)
	switch strings.ToLower(item.Interval) {
	case "year":
		return base / float64(12*intervalCount)
	case "week":
		return base * 52 / float64(12*intervalCount)
	case "day":
		return base * 365 / float64(12*intervalCount)
	default:
		return base / float64(intervalCount)
	}
}

func absCents(v int64) float64 {
	if v < 0 {
		return float64(-v)
	}
	return float64(v)
}

// MissionNetMRRSource bridges reconciler-written Stripe ledger rows into
// mission evaluation. It surfaces only rows whose source is SourceStripeTestMode;
// reports must label those as test-mode path proof, never earned revenue.
type MissionNetMRRSource struct {
	Store *RevenueStore
}

func (m MissionNetMRRSource) Observe(ctx context.Context, _ string, at time.Time) (mission.LedgerSample, error) {
	if m.Store == nil {
		return mission.LedgerSample{At: at.UTC(), Available: false}, nil
	}
	row, err := m.Store.Latest(ctx, at)
	if errors.Is(err, sql.ErrNoRows) {
		return mission.LedgerSample{At: at.UTC(), Available: false}, nil
	}
	if err != nil {
		return mission.LedgerSample{}, err
	}
	return mission.LedgerSample{
		At:               row.At.UTC(),
		SubscriptionsUSD: row.SubscriptionsUSD,
		RefundsUSD:       row.RefundsUSD,
		CancellationsUSD: row.CancellationsUSD,
		DiscountsUSD:     row.DiscountsUSD,
		Available:        true,
	}, nil
}
