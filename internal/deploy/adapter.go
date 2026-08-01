package deploy

import "context"

type Adapter interface {
	DeployPreview(ctx context.Context, product string, artifact string) (Record, error)
	DeployProduction(ctx context.Context, product string, artifact string) (Record, error)
	Rollback(ctx context.Context, product string, ref string) (Record, error)
	Health(ctx context.Context, url string) error
}

type Record struct {
	Product          string `json:"product"`
	Environment      string `json:"environment"`
	Ref              string `json:"ref"`
	VerificationMode string `json:"verification_mode"`
	// URL is the real reachable URL the deploy exposes (docs/PLAN.md Task 125);
	// empty for adapters/paths that do not surface one.
	URL string `json:"url,omitempty"`
}
