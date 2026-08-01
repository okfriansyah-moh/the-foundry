package kernel

// export_test.go exposes unexported internals to the external kernel_test
// package for targeted testing (standard Go pattern), without widening the
// package's real public API.

// PecWavesForTest exposes pecWaves so wave-grouping and PEC-distrust-fallback
// behavior (docs/PLAN.md Task 124/56) can be asserted directly.
var PecWavesForTest = pecWaves
