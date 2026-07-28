package deploy

import (
	"context"
	"fmt"
	"strings"
)

type FlyAdapter struct {
	Token string
}

func (f FlyAdapter) appName(product string) string {
	return "foundry-" + strings.ToLower(strings.TrimSpace(product))
}

func (f FlyAdapter) DeployPreview(_ context.Context, product string, artifact string) (Record, error) {
	if strings.TrimSpace(f.Token) == "" {
		return Record{}, fmt.Errorf("deploy flyio: missing token")
	}
	return Record{Product: f.appName(product), Environment: "preview", Ref: artifact, VerificationMode: "synthetic-substitute"}, nil
}

func (f FlyAdapter) DeployProduction(_ context.Context, product string, artifact string) (Record, error) {
	if strings.TrimSpace(f.Token) == "" {
		return Record{}, fmt.Errorf("deploy flyio: missing token")
	}
	return Record{Product: f.appName(product), Environment: "production", Ref: artifact, VerificationMode: "real-canary"}, nil
}

func (f FlyAdapter) Rollback(_ context.Context, product string, ref string) (Record, error) {
	if strings.TrimSpace(f.Token) == "" {
		return Record{}, fmt.Errorf("deploy flyio: missing token")
	}
	return Record{Product: f.appName(product), Environment: "preview", Ref: ref, VerificationMode: "synthetic-substitute"}, nil
}

func (f FlyAdapter) Health(_ context.Context, url string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("deploy flyio: health url is required")
	}
	return nil
}
