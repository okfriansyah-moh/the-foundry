package billing

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v79"
	"github.com/stripe/stripe-go/v79/client"
)

var (
	ErrInvalidStripeKey = errors.New("billing: Stripe key must be an sk_test key")
	ErrLiveModeKey      = errors.New("billing: live-mode Stripe key refused")
)

// ClientConfig is the enforced boundary for constructing a Stripe client.
type ClientConfig struct {
	SecretKey string

	MaturityCriteria MaturityCriteria
	MaturityEvidence MaturityEvidence

	HTTPClient *http.Client
}

// StripeClient calls Stripe's test-mode API using the official SDK.
type StripeClient struct {
	api *client.API
}

// NewStripeClient builds a test-mode Stripe client. Live keys never load in
// this task; while maturity is immature the refusal reports the maturity gate
// specifically so B13 is an executable control, not documentation.
func NewStripeClient(cfg ClientConfig) (*StripeClient, error) {
	key := strings.TrimSpace(cfg.SecretKey)
	if strings.HasPrefix(key, "sk_live_") {
		criteria := cfg.MaturityCriteria
		if criteria == (MaturityCriteria{}) {
			criteria = DefaultMaturityCriteria()
		}
		matured, missing := criteria.Evaluate(cfg.MaturityEvidence)
		if !matured {
			return nil, fmt.Errorf("%w while billing maturity is immature: %s", ErrLiveModeKey, strings.Join(missing, ", "))
		}
		return nil, fmt.Errorf("%w: VEN-16 enables test mode only", ErrLiveModeKey)
	}
	if !strings.HasPrefix(key, "sk_test_") {
		return nil, ErrInvalidStripeKey
	}

	backends := stripe.NewBackends(cfg.HTTPClient)
	var api client.API
	api.Init(key, backends)
	return &StripeClient{api: &api}, nil
}

// CheckoutSessionRequest describes one Stripe Checkout subscription session.
type CheckoutSessionRequest struct {
	CustomerID    string
	CustomerEmail string
	AmountCents   int64
	Currency      string
	ProductName   string
	SuccessURL    string
	CancelURL     string
	Quantity      int64
	Metadata      map[string]string
}

// CheckoutSession is the subset of Stripe's checkout session object callers use.
type CheckoutSession struct {
	ID          string
	URL         string
	AmountCents int64
	Currency    string
}

// PortalSession is the subset of Stripe's customer portal session callers use.
type PortalSession struct {
	ID  string
	URL string
}

// BalanceTransaction is the provider-neutral balance ledger row used by reconciliation.
type BalanceTransaction struct {
	ID        string
	Type      string
	Currency  string
	Amount    int64
	Net       int64
	CreatedAt time.Time
}

// Subscription is the provider-neutral subscription shape used by reconciliation.
type Subscription struct {
	ID     string
	Status string
	Items  []SubscriptionItem
}

// SubscriptionItem is one recurring price line on a subscription.
type SubscriptionItem struct {
	Currency      string
	UnitAmount    int64
	Quantity      int64
	Interval      string
	IntervalCount int64
}

// CreateCheckoutSession creates a real Stripe test-mode Checkout session with
// the requested amount carried as unit_amount, not encoded into an ID string.
func (c *StripeClient) CreateCheckoutSession(ctx context.Context, req CheckoutSessionRequest) (CheckoutSession, error) {
	if req.AmountCents <= 0 {
		return CheckoutSession{}, fmt.Errorf("billing: checkout amount must be positive")
	}
	currency := strings.ToLower(strings.TrimSpace(req.Currency))
	if currency == "" {
		return CheckoutSession{}, fmt.Errorf("billing: checkout currency is required")
	}
	if strings.TrimSpace(req.ProductName) == "" {
		return CheckoutSession{}, fmt.Errorf("billing: checkout product name is required")
	}
	if strings.TrimSpace(req.SuccessURL) == "" || strings.TrimSpace(req.CancelURL) == "" {
		return CheckoutSession{}, fmt.Errorf("billing: checkout success and cancel URLs are required")
	}
	quantity := req.Quantity
	if quantity == 0 {
		quantity = 1
	}
	if quantity < 0 {
		return CheckoutSession{}, fmt.Errorf("billing: checkout quantity must be positive")
	}

	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL: stripe.String(req.SuccessURL),
		CancelURL:  stripe.String(req.CancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Quantity: stripe.Int64(quantity),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(currency),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String(req.ProductName),
				},
				Recurring: &stripe.CheckoutSessionLineItemPriceDataRecurringParams{
					Interval: stripe.String("month"),
				},
				UnitAmount: stripe.Int64(req.AmountCents),
			},
		}},
		Metadata: req.Metadata,
	}
	params.Context = ctx
	if strings.TrimSpace(req.CustomerID) != "" {
		params.Customer = stripe.String(req.CustomerID)
	} else if strings.TrimSpace(req.CustomerEmail) != "" {
		params.CustomerEmail = stripe.String(req.CustomerEmail)
	}

	sess, err := c.api.CheckoutSessions.New(params)
	if err != nil {
		return CheckoutSession{}, fmt.Errorf("billing: create Stripe checkout session: %w", err)
	}
	return CheckoutSession{ID: sess.ID, URL: sess.URL, AmountCents: req.AmountCents, Currency: currency}, nil
}

// CreatePortalSession creates a real Stripe customer portal session.
func (c *StripeClient) CreatePortalSession(ctx context.Context, customerID, returnURL string) (PortalSession, error) {
	if strings.TrimSpace(customerID) == "" {
		return PortalSession{}, fmt.Errorf("billing: portal customer id is required")
	}
	if strings.TrimSpace(returnURL) == "" {
		return PortalSession{}, fmt.Errorf("billing: portal return URL is required")
	}
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	params.Context = ctx

	sess, err := c.api.BillingPortalSessions.New(params)
	if err != nil {
		return PortalSession{}, fmt.Errorf("billing: create Stripe portal session: %w", err)
	}
	return PortalSession{ID: sess.ID, URL: sess.URL}, nil
}

// GetSubscription reads one subscription from Stripe.
func (c *StripeClient) GetSubscription(ctx context.Context, id string) (Subscription, error) {
	if strings.TrimSpace(id) == "" {
		return Subscription{}, fmt.Errorf("billing: subscription id is required")
	}
	params := &stripe.SubscriptionParams{}
	params.Context = ctx
	params.AddExpand("items.data.price")

	sub, err := c.api.Subscriptions.Get(id, params)
	if err != nil {
		return Subscription{}, fmt.Errorf("billing: read Stripe subscription %s: %w", id, err)
	}
	return subscriptionFromStripe(sub), nil
}

// ListBalanceTransactions lists Stripe balance transactions in [since, until).
func (c *StripeClient) ListBalanceTransactions(ctx context.Context, since, until time.Time) ([]BalanceTransaction, error) {
	params := &stripe.BalanceTransactionListParams{
		CreatedRange: &stripe.RangeQueryParams{
			GreaterThanOrEqual: since.UTC().Unix(),
			LesserThan:         until.UTC().Unix(),
		},
	}
	params.Context = ctx
	params.Limit = stripe.Int64(100)

	iter := c.api.BalanceTransactions.List(params)
	var out []BalanceTransaction
	for iter.Next() {
		bt := iter.BalanceTransaction()
		out = append(out, BalanceTransaction{
			ID:        bt.ID,
			Type:      string(bt.Type),
			Currency:  strings.ToLower(string(bt.Currency)),
			Amount:    bt.Amount,
			Net:       bt.Net,
			CreatedAt: time.Unix(bt.Created, 0).UTC(),
		})
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("billing: list Stripe balance transactions: %w", err)
	}
	return out, nil
}

// ListSubscriptions lists Stripe subscriptions, optionally scoped to a test clock.
func (c *StripeClient) ListSubscriptions(ctx context.Context, testClockID string) ([]Subscription, error) {
	params := &stripe.SubscriptionListParams{
		Status: stripe.String("all"),
	}
	params.Context = ctx
	params.Limit = stripe.Int64(100)
	params.AddExpand("data.items.data.price")
	if strings.TrimSpace(testClockID) != "" {
		params.TestClock = stripe.String(testClockID)
	}

	iter := c.api.Subscriptions.List(params)
	var out []Subscription
	for iter.Next() {
		out = append(out, subscriptionFromStripe(iter.Subscription()))
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("billing: list Stripe subscriptions: %w", err)
	}
	return out, nil
}

func subscriptionFromStripe(sub *stripe.Subscription) Subscription {
	out := Subscription{ID: sub.ID, Status: string(sub.Status)}
	if sub.Items == nil {
		return out
	}
	for _, item := range sub.Items.Data {
		if item == nil || item.Price == nil {
			continue
		}
		intervalCount := int64(1)
		interval := ""
		if item.Price.Recurring != nil {
			interval = string(item.Price.Recurring.Interval)
			if item.Price.Recurring.IntervalCount > 0 {
				intervalCount = item.Price.Recurring.IntervalCount
			}
		}
		quantity := item.Quantity
		if quantity == 0 {
			quantity = 1
		}
		out.Items = append(out.Items, SubscriptionItem{
			Currency:      strings.ToLower(string(item.Price.Currency)),
			UnitAmount:    item.Price.UnitAmount,
			Quantity:      quantity,
			Interval:      interval,
			IntervalCount: intervalCount,
		})
	}
	return out
}
