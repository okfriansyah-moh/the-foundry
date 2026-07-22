package state

import "testing"

func TestNormalizeResultCode(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantCode ResultCode
		wantOK   bool
	}{
		{"canonical code", "PROVEN_BLOCKED", ResultProvenBlocked, true},
		{"deprecated alias normalizes to canonical", DeprecatedAliasTenXBranchesReady, ResultTenXBranchHandoffReady, true},
		{"unknown code", "NOT_A_REAL_CODE", "", false},
		{"empty string", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizeResultCode(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("NormalizeResultCode(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if got != tc.wantCode {
				t.Fatalf("NormalizeResultCode(%q) = %q, want %q", tc.in, got, tc.wantCode)
			}
		})
	}
}

func TestNormalizeResultCode_AliasNeverEmittedByString(t *testing.T) {
	code, ok := NormalizeResultCode(DeprecatedAliasTenXBranchesReady)
	if !ok {
		t.Fatal("expected alias to normalize successfully")
	}
	if code.String() == DeprecatedAliasTenXBranchesReady {
		t.Fatalf("String() emitted the deprecated alias: %q", code.String())
	}
	if code.String() != string(ResultTenXBranchHandoffReady) {
		t.Fatalf("String() = %q, want %q", code.String(), ResultTenXBranchHandoffReady)
	}

	// Every canonical result code's String() must never equal the alias.
	for _, e := range resultCodeRegistry {
		if e.Code.String() == DeprecatedAliasTenXBranchesReady {
			t.Fatalf("registry code %q strings to the deprecated alias", e.Code)
		}
	}
}
