package projection

import (
	"context"
	"testing"
)

func TestProjector_TableAndVersionDefaults(t *testing.T) {
	p := &Projector{}
	if got := p.table(); got != DefaultTable {
		t.Fatalf("table() = %q, want %q", got, DefaultTable)
	}
	if got := p.version(); got != ProjectorVersion {
		t.Fatalf("version() = %q, want %q", got, ProjectorVersion)
	}

	p2 := &Projector{Table: ShadowTable, Version: "v1"}
	if got := p2.table(); got != ShadowTable {
		t.Fatalf("table() = %q, want %q", got, ShadowTable)
	}
	if got := p2.version(); got != "v1" {
		t.Fatalf("version() = %q, want %q", got, "v1")
	}
}

func TestProjector_UpsertSQL_KnownTables(t *testing.T) {
	live := &Projector{}
	sqlText, err := live.upsertSQL()
	if err != nil {
		t.Fatalf("upsertSQL() for default table: %v", err)
	}
	if sqlText != upsertProjectionSQL {
		t.Fatalf("upsertSQL() for default table did not return upsertProjectionSQL")
	}

	shadow := &Projector{Table: ShadowTable}
	sqlText, err = shadow.upsertSQL()
	if err != nil {
		t.Fatalf("upsertSQL() for shadow table: %v", err)
	}
	if sqlText != upsertProjectionShadowSQL {
		t.Fatalf("upsertSQL() for shadow table did not return upsertProjectionShadowSQL")
	}
}

// TestProjector_UpsertSQL_UnknownTableFailsClosed pins the OWASP A05
// defense-in-depth guard: an unrecognized Table value must never reach a
// query — Tick (via upsertSQL) fails closed instead.
func TestProjector_UpsertSQL_UnknownTableFailsClosed(t *testing.T) {
	p := &Projector{Table: "workflow_status_projection; DROP TABLE workflow_transitions;--"}
	if _, err := p.upsertSQL(); err == nil {
		t.Fatal("expected upsertSQL to reject an unrecognized table, got nil error")
	}
}

func TestRollout_RequiresToVersion(t *testing.T) {
	// db is nil: toVersion is validated before Rollout ever touches it.
	if _, err := Rollout(context.Background(), nil, ""); err == nil {
		t.Fatal("expected Rollout to reject an empty toVersion before touching db")
	}
}
