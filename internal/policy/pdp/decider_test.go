package pdp

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/policy"
	"github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
)

// bundleDir is the rego bundle this task's card requires under
// config/policy/rego/. go test sets the working directory to this
// package's directory, so the relative path is stable regardless of
// where `go test`/`go build` itself is invoked from.
const bundleDir = "../../../config/policy/rego"

// testPlatform mirrors config/policy/platform.yaml (Task 22's platform
// layer fixture) closely enough to exercise every rego rule in
// config/policy/rego/authz.rego.
func testPlatform() compiler.LayerPolicy {
	ref := "config/validation-allowlist.yaml"
	return compiler.LayerPolicy{
		PermissionsAllowlist: []string{"repo-read", "repo-write", "ci-trigger"},
		DeploymentModes: map[string]compiler.Mode{
			"preview":    compiler.ModeAuto,
			"staging":    compiler.ModeCommand,
			"production": compiler.ModeCommand,
		},
		BudgetCeilingsUSD: map[string]float64{
			"workflow_usd": 100,
			"task_usd":     5,
		},
		ExecutorAllowlist:      []string{"fake", "claude-code"},
		ValidationAllowlistRef: &ref,
		NotificationClasses:    []string{"telegram-low-risk", "telegram-veto-digest", "email"},
		RiskTierControls: map[string]compiler.RiskTierControl{
			"A0": {AutoAllowed: true, RequireReview: false},
			"A1": {AutoAllowed: true, RequireReview: true},
			"A2": {AutoAllowed: true, RequireReview: true},
			"H":  {AutoAllowed: false, RequireReview: true},
		},
	}
}

// testResolved compiles testPlatform through the real compiler, with org
// tightening notification_classes to forbid Telegram — the docs' own N6.1
// example (see internal/policy/compiler/golden_test.go) — so authz_test's
// conformance-1 case has a real, compiler-produced Resolved to compare
// against a "no compiler" reconstruction.
func testResolved(t testing.TB) *compiler.Resolved {
	t.Helper()
	resolved, err := compiler.Compile(
		testPlatform(),
		compiler.LayerPolicy{NotificationClasses: []string{"email"}},
		compiler.LayerPolicy{},
		compiler.LayerPolicy{},
	)
	if err != nil {
		t.Fatalf("compiler.Compile: %v", err)
	}
	return resolved
}

func newTestDecider(t testing.TB, resolved *compiler.Resolved) *OPADecider {
	t.Helper()
	digest, err := BundleDigest(bundleDir)
	if err != nil {
		t.Fatalf("BundleDigest: %v", err)
	}
	d, err := NewOPADecider(context.Background(), bundleDir, digest, resolved)
	if err != nil {
		t.Fatalf("NewOPADecider: %v", err)
	}
	return d
}

func TestDecide_Permission(t *testing.T) {
	resolved := testResolved(t)
	d := newTestDecider(t, resolved)

	cases := []struct {
		name      string
		resource  string
		wantAllow bool
	}{
		{"allowed permission", "repo-read", true},
		{"permission not in allowlist", "secrets-read", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, err := d.Decide(context.Background(), policy.Request{
				Principal:    "agent-1",
				Action:       "permission",
				Resource:     tc.resource,
				PolicyDigest: resolved.Digest,
			})
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if dec.Allow != tc.wantAllow {
				t.Fatalf("Allow = %v, want %v (reason: %s)", dec.Allow, tc.wantAllow, dec.Reason)
			}
			if dec.Reason == "" {
				t.Fatal("Reason must not be empty")
			}
		})
	}
}

func TestDecide_Notify_OrgTightenedTelegram(t *testing.T) {
	resolved := testResolved(t)
	d := newTestDecider(t, resolved)

	dec, err := d.Decide(context.Background(), policy.Request{
		Principal:    "agent-1",
		Action:       "notify",
		Resource:     "telegram-low-risk",
		PolicyDigest: resolved.Digest,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Allow {
		t.Fatalf("expected deny: org tightened notification_classes to forbid Telegram, got allow (reason: %s)", dec.Reason)
	}

	dec, err = d.Decide(context.Background(), policy.Request{
		Principal:    "agent-1",
		Action:       "notify",
		Resource:     "email",
		PolicyDigest: resolved.Digest,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !dec.Allow {
		t.Fatalf("expected allow for email notification, got deny (reason: %s)", dec.Reason)
	}
}

func TestDecide_Execute(t *testing.T) {
	resolved := testResolved(t)
	d := newTestDecider(t, resolved)

	allow, err := d.Decide(context.Background(), policy.Request{
		Action: "execute", Resource: "claude-code", PolicyDigest: resolved.Digest,
	})
	if err != nil || !allow.Allow {
		t.Fatalf("expected allow for claude-code executor, got %+v err=%v", allow, err)
	}

	deny, err := d.Decide(context.Background(), policy.Request{
		Action: "execute", Resource: "codex", PolicyDigest: resolved.Digest,
	})
	if err != nil || deny.Allow {
		t.Fatalf("expected deny for unlisted executor, got %+v err=%v", deny, err)
	}
}

func TestDecide_Deploy(t *testing.T) {
	resolved := testResolved(t)
	d := newTestDecider(t, resolved)

	cases := []struct {
		name      string
		env       string
		approved  bool
		wantAllow bool
	}{
		{"preview is auto — always allowed", "preview", false, true},
		{"staging is command — requires approval", "staging", false, false},
		{"staging is command — approved", "staging", true, true},
		{"unknown env — deny", "canary", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, err := d.Decide(context.Background(), policy.Request{
				Action:       "deploy",
				Resource:     "deployment",
				Context:      map[string]any{"env": tc.env, "approved": tc.approved},
				PolicyDigest: resolved.Digest,
			})
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if dec.Allow != tc.wantAllow {
				t.Fatalf("Allow = %v, want %v (reason: %s)", dec.Allow, tc.wantAllow, dec.Reason)
			}
		})
	}
}

func TestDecide_StalePolicyDigestDenied(t *testing.T) {
	resolved := testResolved(t)
	d := newTestDecider(t, resolved)

	dec, err := d.Decide(context.Background(), policy.Request{
		Action: "permission", Resource: "repo-read", PolicyDigest: "sha256:not-the-real-digest",
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.Allow {
		t.Fatal("expected deny: request carried a policy_digest that does not match the Decider's bound Resolved policy")
	}
}

// --- Conformance test 1 ---------------------------------------------------
//
// "Removing the compiler breaks precedence tests even with the PDP
// present." This is an architecture/boundary test, not a unit test: it
// shows that a PDP fed a policy that was NOT produced by
// compiler.Compile — i.e. one that skipped the org layer's tightening —
// reaches the wrong (unsafe) decision, proving the PDP alone cannot
// reconstruct correct precedence. The compiler-produced Resolved is the
// only thing that gets this right.
func TestConformance_RemovingCompilerBreaksPrecedence(t *testing.T) {
	// The properly compiled policy: org tightened notification_classes to
	// forbid Telegram, so Telegram must be denied.
	compiled := testResolved(t)
	compiledDecider := newTestDecider(t, compiled)

	got, err := compiledDecider.Decide(context.Background(), policy.Request{
		Action: "notify", Resource: "telegram-low-risk", PolicyDigest: compiled.Digest,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got.Allow {
		t.Fatal("compiler-produced policy must deny Telegram after org tightening — precedence is broken")
	}

	// Simulate "the compiler never ran": construct a Resolved directly
	// from the platform layer's raw, untightened notification_classes,
	// bypassing compiler.Compile entirely (no org/profile/workflow merge
	// applied at all). This is deliberately NOT how production code is
	// allowed to build a Resolved — NewOPADecider has no other entry
	// point for policy data, so this fixture exists purely to prove what
	// happens if something upstream of the PDP skipped the compiler.
	platform := testPlatform()
	uncompiled := &compiler.Resolved{
		Effective: compiler.Policy{
			PermissionsAllowlist:   platform.PermissionsAllowlist,
			DeploymentModes:        platform.DeploymentModes,
			BudgetCeilingsUSD:      platform.BudgetCeilingsUSD,
			ExecutorAllowlist:      platform.ExecutorAllowlist,
			ValidationAllowlistRef: *platform.ValidationAllowlistRef,
			NotificationClasses:    platform.NotificationClasses, // still includes Telegram — org's tightening never applied
			RiskTierControls:       platform.RiskTierControls,
		},
		Digest: "sha256:uncompiled-fixture-digest",
	}
	uncompiledDecider := newTestDecider(t, uncompiled)

	got, err = uncompiledDecider.Decide(context.Background(), policy.Request{
		Action: "notify", Resource: "telegram-low-risk", PolicyDigest: uncompiled.Digest,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !got.Allow {
		t.Fatal("expected the uncompiled fixture to (incorrectly) allow Telegram — if it denies too, this test no longer demonstrates the compiler's necessity")
	}

	// The two Deciders reach opposite answers for the identical
	// (action, resource) pair, solely because one was built from
	// compiler.Compile's output and the other bypassed it. That is the
	// proof: the PDP itself contains no precedence logic — remove the
	// compiler from the pipeline and precedence silently breaks even
	// though the PDP is still present and functioning.
}

// --- Conformance test 2 ---------------------------------------------------
//
// "Decisions are pure functions of (request, digest)." Same input always
// produces the same decision, and the Decider type has no mutable field
// through which hidden state could leak into that output.
func TestConformance_DecideIsPure(t *testing.T) {
	resolved := testResolved(t)
	d := newTestDecider(t, resolved)

	req := policy.Request{
		Principal:    "agent-1",
		Action:       "deploy",
		Resource:     "deployment",
		Context:      map[string]any{"env": "staging", "approved": true},
		PolicyDigest: resolved.Digest,
	}

	first, err := d.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("Decide (1st): %v", err)
	}
	second, err := d.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("Decide (2nd): %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Decide is not pure: first=%+v second=%+v", first, second)
	}

	// The public contract (internal/policy.Decider) exposes exactly one
	// method and, being an interface, carries no fields at all — there is
	// no surface through which a caller could observe or mutate hidden
	// state that would make Decide's output depend on anything beyond
	// its arguments.
	deciderType := reflect.TypeOf((*policy.Decider)(nil)).Elem()
	if deciderType.NumMethod() != 1 {
		t.Fatalf("policy.Decider must expose exactly one method, got %d", deciderType.NumMethod())
	}
	if deciderType.Method(0).Name != "Decide" {
		t.Fatalf("policy.Decider's sole method must be Decide, got %q", deciderType.Method(0).Name)
	}
}

// --- Conformance test 3 ---------------------------------------------------
//
// "A weakened policy never reaches the PDP at all." Proven at the type
// level, not by comment: NewOPADecider's only policy-shaped parameter is
// *compiler.Resolved (Task 22's already-validated, non-weakened output),
// and this package's own source never references compiler.LayerPolicy —
// the raw, pre-merge, per-layer type that a weakened policy would have to
// travel through.
func TestConformance_OnlyResolvedPolicyReachesPDP(t *testing.T) {
	fn := reflect.TypeOf(NewOPADecider)
	if fn.Kind() != reflect.Func {
		t.Fatal("NewOPADecider must be a function")
	}
	// Signature: (ctx, bundleDir, pinnedBundleDigest, resolved).
	if fn.NumIn() != 4 {
		t.Fatalf("NewOPADecider must take exactly 4 params, got %d", fn.NumIn())
	}
	resolvedParam := fn.In(3)
	if resolvedParam.Kind() != reflect.Ptr {
		t.Fatalf("NewOPADecider's 4th param must be a pointer, got %s", resolvedParam.Kind())
	}
	elem := resolvedParam.Elem()
	wantPkg := "github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	if elem.PkgPath() != wantPkg || elem.Name() != "Resolved" {
		t.Fatalf("NewOPADecider's 4th param must be *compiler.Resolved, got *%s.%s", elem.PkgPath(), elem.Name())
	}

	// The OPADecider struct itself only stores *compiler.Resolved, never
	// a compiler.LayerPolicy.
	deciderStruct := reflect.TypeOf(OPADecider{})
	resolvedField, ok := deciderStruct.FieldByName("resolved")
	if !ok {
		t.Fatal("OPADecider must have a resolved field")
	}
	if resolvedField.Type != resolvedParam {
		t.Fatalf("OPADecider.resolved must be *compiler.Resolved, got %s", resolvedField.Type)
	}

	// Belt-and-suspenders source check: this package's own non-test .go
	// files must never use the compiler.LayerPolicy identifier as an
	// actual type reference — the raw, pre-merge type. Parsed via go/ast
	// (not a text grep) so a doc comment merely explaining this
	// constraint in prose does not itself trip the check; only a real
	// selector expression (compiler.LayerPolicy used as a type/value)
	// would.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir: %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parser.ParseFile(%s): %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "LayerPolicy" {
				t.Fatalf("%s references compiler.LayerPolicy as a type/value — the PDP must only ever consume compiler.Resolved", name)
			}
			return true
		})
	}
}

// BenchmarkDecide measures per-call latency and reports p99, per this
// task's <5ms p99 acceptance bar. Standard Go benchmark output (ns/op) is
// an average, not a percentile, so durations are collected manually and
// the 99th percentile is reported via b.ReportMetric.
func BenchmarkDecide(b *testing.B) {
	resolved := testResolved(b)
	d := newTestDecider(b, resolved)
	ctx := context.Background()
	req := policy.Request{
		Principal:    "agent-1",
		Action:       "permission",
		Resource:     "repo-read",
		PolicyDigest: resolved.Digest,
	}

	durations := make([]time.Duration, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		if _, err := d.Decide(ctx, req); err != nil {
			b.Fatalf("Decide: %v", err)
		}
		durations = append(durations, time.Since(start))
	}
	b.StopTimer()

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	idx := int(float64(len(durations)) * 0.99)
	if idx >= len(durations) {
		idx = len(durations) - 1
	}
	b.ReportMetric(float64(durations[idx].Nanoseconds()), "ns/p99")
}
