package executor

import (
	"context"
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

type stubAdapter struct{}

func (stubAdapter) Prepare(context.Context, worktree.Workspace, TaskPacket) error { return nil }
func (stubAdapter) Run(context.Context) (Summary, error)                          { return Summary{}, nil }

func (stubAdapter) Collect(context.Context) (Artifacts, error) { return Artifacts{}, nil }

func TestRegisterAndGet(t *testing.T) {
	Register("registry-test-stub", func() Adapter { return stubAdapter{} })

	got, err := Get("registry-test-stub")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := got.(stubAdapter); !ok {
		t.Fatalf("Get returned wrong adapter type: %T", got)
	}
}

func TestGet_Unregistered(t *testing.T) {
	if _, err := Get("registry-test-does-not-exist"); err == nil {
		t.Fatalf("expected error for unregistered adapter name")
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	Register("registry-test-dup", func() Adapter { return stubAdapter{} })
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate registration")
		}
	}()
	Register("registry-test-dup", func() Adapter { return stubAdapter{} })
}
