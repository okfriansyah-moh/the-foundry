package executor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRunSubprocessWithEnv_ConcurrentScopesIsolated proves Task 117 (SEC-03): N
// concurrent invocations, each carrying a DIFFERENT credential in its
// per-invocation env, each observe exactly their own value and never another's.
// Because the credential is passed per-child (never os.Setenv on the shared
// process), there is no interleaving window for cross-talk. Run under -race.
func TestRunSubprocessWithEnv_ConcurrentScopesIsolated(t *testing.T) {
	const n = 24
	const varName = "FOUNDRY_TEST_SECRET"

	// Guard: the shared process env must never carry the secret at any point.
	if _, ok := os.LookupEnv(varName); ok {
		t.Fatalf("%s must be unset in the parent before this test", varName)
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("secret-scope-%d", i)
			res, err := RunSubprocessWithEnv(context.Background(), "",
				"printenv "+varName, nil, nil, map[string]string{varName: want}, 5*time.Second)
			if err != nil {
				errs <- fmt.Errorf("scope %d: run: %w", i, err)
				return
			}
			got := strings.TrimSpace(res.Stdout)
			if got != want {
				errs <- fmt.Errorf("scope %d: child saw %q, want its own %q (CROSS-SCOPE LEAK)", i, got, want)
				return
			}
			// The parent process env must not have gained the secret.
			if _, ok := os.LookupEnv(varName); ok {
				errs <- fmt.Errorf("scope %d: secret leaked into the parent process env", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	// Final assertion: the parent env is still clean.
	if _, ok := os.LookupEnv(varName); ok {
		t.Fatal("secret must never be present in the parent process env")
	}
}

// TestBuildChildEnv_ExtraOverridesAllowlistWithoutParentMutation proves the
// child env is constructed (not read back from a mutated process) and that
// extraEnv overrides an allowlisted value for the child only.
func TestBuildChildEnv_ExtraOverridesAllowlistWithoutParentMutation(t *testing.T) {
	const name = "FOUNDRY_TEST_OVERRIDE"
	t.Setenv(name, "from-parent")
	env := buildChildEnv([]string{name}, map[string]string{name: "from-invocation"})
	found := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, name+"=") {
			found = strings.TrimPrefix(kv, name+"=")
		}
	}
	if found != "from-invocation" {
		t.Fatalf("child env should carry the per-invocation value, got %q", found)
	}
	// The parent value is unchanged (we never mutate it).
	if os.Getenv(name) != "from-parent" {
		t.Fatalf("parent env was mutated: %q", os.Getenv(name))
	}
}
