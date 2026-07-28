package billing

import (
	"context"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/mission"
)

type ReconciliationInput struct {
	SubscriptionsUSD float64
	RefundsUSD       float64
	CancellationsUSD float64
	DiscountsUSD     float64
	At               time.Time
}

type ReconciliationRow struct {
	NetMRRUSD        float64
	SubscriptionsUSD float64
	RefundsUSD       float64
	CancellationsUSD float64
	DiscountsUSD     float64
	At               time.Time
}

func Reconcile(in ReconciliationInput) ReconciliationRow {
	return ReconciliationRow{
		NetMRRUSD:        in.SubscriptionsUSD - in.RefundsUSD - in.CancellationsUSD - in.DiscountsUSD,
		SubscriptionsUSD: in.SubscriptionsUSD,
		RefundsUSD:       in.RefundsUSD,
		CancellationsUSD: in.CancellationsUSD,
		DiscountsUSD:     in.DiscountsUSD,
		At:               in.At.UTC(),
	}
}

// MissionNetMRRSource bridges reconciliation rows into mission evaluator input.
type MissionNetMRRSource struct {
	Available bool
	Latest    ReconciliationRow
}

func (m MissionNetMRRSource) Observe(_ context.Context, _ string, at time.Time) (mission.LedgerSample, error) {
	if !m.Available {
		return mission.LedgerSample{At: at.UTC(), Available: false}, nil
	}
	return mission.LedgerSample{
		At:               m.Latest.At.UTC(),
		SubscriptionsUSD: m.Latest.SubscriptionsUSD,
		RefundsUSD:       m.Latest.RefundsUSD,
		CancellationsUSD: m.Latest.CancellationsUSD,
		DiscountsUSD:     m.Latest.DiscountsUSD,
		Available:        true,
	}, nil
}
