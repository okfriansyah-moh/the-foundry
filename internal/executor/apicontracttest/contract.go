package apicontracttest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/executor"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// Options configures the shared API-adapter contract suite for one provider.
type Options struct {
	// Name is the registry name the adapter registers under.
	Name string
	// New constructs a fresh adapter AFTER the test env (BaseURLEnv) is set.
	New func() executor.Adapter
	// BaseURLEnv is the env var the adapter reads to override its base URL,
	// pointed by these tests at the httptest server.
	BaseURLEnv string
}

const stubContent = "api-contract-ok"

// Run executes the full API contract suite as subtests of t.
func Run(t *testing.T, o Options) {
	t.Helper()
	t.Run("Registered", func(t *testing.T) {
		if _, err := executor.Get(o.Name); err != nil {
			t.Fatalf("executor.Get(%q): %v", o.Name, err)
		}
	})
	t.Run("RunReturnsAssistantContent", func(t *testing.T) { testRun(t, o, http.StatusOK, false) })
	t.Run("RunNonOKStatusIsError", func(t *testing.T) { testRun(t, o, http.StatusInternalServerError, true) })
	t.Run("RejectsEmptyGoal", func(t *testing.T) {
		a := o.New()
		if err := a.Prepare(context.Background(), worktree.Workspace{Path: t.TempDir()}, executor.TaskPacket{}); err == nil {
			t.Fatal("expected error for empty goal")
		}
	})
}

func testRun(t *testing.T, o Options, status int, wantErr bool) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + stubContent + `"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`))
	}))
	defer srv.Close()
	t.Setenv(o.BaseURLEnv, srv.URL)

	a := o.New()
	ws := worktree.Workspace{Path: t.TempDir()}
	if err := a.Prepare(context.Background(), ws, executor.TaskPacket{Goal: "hello"}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	summary, err := a.Run(context.Background())
	if wantErr {
		if err == nil {
			t.Fatal("expected error for non-OK status")
		}
		return
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary.Claimed != stubContent {
		t.Fatalf("Summary.Claimed = %q, want %q", summary.Claimed, stubContent)
	}
	arts, err := a.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(arts.Paths) == 0 {
		t.Fatal("Collect returned no artifacts")
	}
}
