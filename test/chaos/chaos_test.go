//go:build chaos

package chaos

import (
	"context"
	"testing"
	"time"
)

func TestChaosScenarioMatrix(t *testing.T) {
	runner := fakeRunner{}
	cases := []fakeScenario{
		{name: "worker-kill", failures: 1, recover: time.Second},
		{name: "temporal-outage", failures: 1, recover: time.Second},
		{name: "postgres-outage", failures: 1, recover: time.Second},
		{name: "provider-storm", failures: 1, recover: time.Second},
		{name: "poisoned-task", failures: 1, recover: time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := runner.Run(context.Background(), tc); err != nil {
				t.Fatalf("scenario %s failed: %v", tc.name, err)
			}
		})
	}
}
