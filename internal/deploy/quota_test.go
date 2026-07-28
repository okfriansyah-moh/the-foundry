package deploy

import "testing"

// TestQuotaActiveMissions proves the Task 81 (EVO-08) per-profile
// max_active_missions extension is enforced: a profile cannot exceed its
// active-mission quota, and Release frees a slot.
func TestQuotaActiveMissions(t *testing.T) {
	enforcer := NewQuotaEnforcer(map[string]ProfileQuota{
		"personal": {MaxActiveMissions: 2},
	})
	if err := enforcer.Acquire("personal", Usage{Missions: 2}); err != nil {
		t.Fatalf("acquiring up to the quota should succeed: %v", err)
	}
	if err := enforcer.Acquire("personal", Usage{Missions: 1}); err == nil {
		t.Fatal("acquiring a 3rd mission over the quota must fail")
	}
	enforcer.Release("personal", Usage{Missions: 1})
	if err := enforcer.Acquire("personal", Usage{Missions: 1}); err != nil {
		t.Fatalf("after release, a mission slot should be free: %v", err)
	}
}

// TestQuotaMissionsIsolatedFromWorkflows proves the mission quota is a
// distinct dimension — exhausting missions does not block workflows.
func TestQuotaMissionsIsolatedFromWorkflows(t *testing.T) {
	enforcer := NewQuotaEnforcer(map[string]ProfileQuota{
		"personal": {MaxActiveMissions: 1, MaxWorkflows: 3},
	})
	if err := enforcer.Acquire("personal", Usage{Missions: 1}); err != nil {
		t.Fatal(err)
	}
	if err := enforcer.Acquire("personal", Usage{Workflows: 2}); err != nil {
		t.Fatalf("workflow quota should be independent of mission quota: %v", err)
	}
}

// TestQuotasLoadIncludesMissions proves the shipped config carries the new
// per-profile max_active_missions field.
func TestQuotasLoadIncludesMissions(t *testing.T) {
	quotas, err := LoadQuotas("../../config/quotas.yaml")
	if err != nil {
		t.Fatalf("LoadQuotas: %v", err)
	}
	if quotas["personal"].MaxActiveMissions == 0 {
		t.Fatal("shipped quotas.yaml is missing personal.max_active_missions")
	}
	if quotas["organization"].MaxActiveMissions == 0 {
		t.Fatal("shipped quotas.yaml is missing organization.max_active_missions")
	}
}
