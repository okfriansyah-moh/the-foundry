package state

// DeprecatedAliasTenXBranchesReady is the superseded historical label for
// ResultTenXBranchHandoffReady (state-model.md §2, §3). It MUST NOT appear in
// new configuration, code, diagrams, or documents — it exists solely so
// NormalizeResultCode can accept it on read. String() never emits it.
const DeprecatedAliasTenXBranchesReady = "TEN_X_BRANCHES_READY"

// NormalizeResultCode maps a raw result-code string to its canonical
// ResultCode, accepting DeprecatedAliasTenXBranchesReady as an input alias for
// ResultTenXBranchHandoffReady. The returned ResultCode is always the
// canonical form — the alias is never produced as output. ok is false if s is
// not a known result code (canonical or aliased).
func NormalizeResultCode(s string) (code ResultCode, ok bool) {
	if s == DeprecatedAliasTenXBranchesReady {
		return ResultTenXBranchHandoffReady, true
	}
	candidate := ResultCode(s)
	if _, known := KnownResultCode(candidate); known {
		return candidate, true
	}
	return "", false
}
