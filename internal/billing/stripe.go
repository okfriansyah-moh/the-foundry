package billing

import "context"

type CheckoutSession struct {
	ID string
}

type StripeClient interface {
	CreateCheckoutSession(ctx context.Context, customerID string, amountCents int64) (CheckoutSession, error)
}

// TestModeClient is a deterministic stub for test-mode billing flows.
type TestModeClient struct{}

func (TestModeClient) CreateCheckoutSession(_ context.Context, customerID string, _ int64) (CheckoutSession, error) {
	return CheckoutSession{ID: "cs_test_" + customerID}, nil
}
