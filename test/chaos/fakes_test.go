//go:build chaos

package chaos

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type fakeScenario struct {
	name     string
	failures int
	recover  time.Duration
}

type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, s fakeScenario) error {
	if s.failures == 0 {
		return nil
	}
	if s.recover <= 0 {
		return fmt.Errorf("scenario %s: %w", s.name, errors.New("stuck"))
	}
	return nil
}
