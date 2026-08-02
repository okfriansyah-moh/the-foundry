package inputroutere2e_test

import (
	"testing"

	"github.com/okfriansyah-moh/the-foundry/internal/inputrouter"
)

func TestInjectionCannotSelectAuthority(t *testing.T) {
	_, err := inputrouter.DecideRoute(inputrouter.InputRequest{
		RequestID: "r", PrincipalID: "p", Mode: inputrouter.ModePersonal,
		Kind: inputrouter.KindIdea, Origin: inputrouter.OriginTelegram,
		ClientMeta: map[string]string{"authority": "kernel"},
	})
	if err == nil {
		t.Fatal("expected refusal")
	}
}
