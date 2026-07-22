package identity

import (
	"context"
	"errors"
	"testing"
)

func TestMemStorePrincipalCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	tests := []struct {
		name    string
		p       *Principal
		wantErr bool
	}{
		{name: "valid human", p: &Principal{ID: "dev-personal", Kind: PrincipalHuman, Display: "Dev Human"}},
		{name: "valid service", p: &Principal{ID: "svc-bot", Kind: PrincipalService, Display: "Bot"}},
		{name: "missing id", p: &Principal{Kind: PrincipalHuman, Display: "x"}, wantErr: true},
		{name: "bad kind", p: &Principal{ID: "bad", Kind: "robot", Display: "x"}, wantErr: true},
		{name: "missing display", p: &Principal{ID: "no-display", Kind: PrincipalHuman}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.CreatePrincipal(ctx, tt.p)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("CreatePrincipal(%+v): want error, got nil", tt.p)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreatePrincipal(%+v): unexpected error: %v", tt.p, err)
			}
			got, err := s.GetPrincipal(ctx, tt.p.ID)
			if err != nil {
				t.Fatalf("GetPrincipal(%s): %v", tt.p.ID, err)
			}
			if got.Display != tt.p.Display || got.Kind != tt.p.Kind {
				t.Fatalf("GetPrincipal(%s) = %+v, want %+v", tt.p.ID, got, tt.p)
			}
		})
	}
}

func TestMemStorePrincipalDuplicate(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	p := &Principal{ID: "dup", Kind: PrincipalHuman, Display: "Dup"}
	if err := s.CreatePrincipal(ctx, p); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := s.CreatePrincipal(ctx, p)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second create: want ErrAlreadyExists, got %v", err)
	}
}

func TestMemStorePrincipalNotFound(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	_, err := s.GetPrincipal(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMemStoreOrganizationAndMembers(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	org := &Organization{ID: "dev-org", Name: "Dev Org"}
	if err := s.CreateOrganization(ctx, org); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	principal := &Principal{ID: "dev-org-owner", Kind: PrincipalHuman, Display: "Owner"}
	if err := s.CreatePrincipal(ctx, principal); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}

	if err := s.AddOrgMember(ctx, &OrgMember{OrgID: org.ID, PrincipalID: principal.ID, Role: "owner"}); err != nil {
		t.Fatalf("AddOrgMember: %v", err)
	}

	members, err := s.ListOrgMembers(ctx, org.ID)
	if err != nil {
		t.Fatalf("ListOrgMembers: %v", err)
	}
	if len(members) != 1 || members[0].Role != "owner" {
		t.Fatalf("ListOrgMembers = %+v, want one owner member", members)
	}

	// referential integrity: unknown org/principal rejected.
	if err := s.AddOrgMember(ctx, &OrgMember{OrgID: "no-such-org", PrincipalID: principal.ID, Role: "member"}); err == nil {
		t.Fatal("AddOrgMember with unknown org: want error, got nil")
	}
	if err := s.AddOrgMember(ctx, &OrgMember{OrgID: org.ID, PrincipalID: "no-such-principal", Role: "member"}); err == nil {
		t.Fatal("AddOrgMember with unknown principal: want error, got nil")
	}
}

func TestMemStoreListIsSortedByID(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	for _, id := range []string{"charlie", "alice", "bob"} {
		if err := s.CreatePrincipal(ctx, &Principal{ID: id, Kind: PrincipalHuman, Display: id}); err != nil {
			t.Fatalf("CreatePrincipal(%s): %v", id, err)
		}
	}
	got, err := s.ListPrincipals(ctx)
	if err != nil {
		t.Fatalf("ListPrincipals: %v", err)
	}
	want := []string{"alice", "bob", "charlie"}
	for i, w := range want {
		if got[i].ID != w {
			t.Fatalf("ListPrincipals[%d].ID = %s, want %s", i, got[i].ID, w)
		}
	}
}
