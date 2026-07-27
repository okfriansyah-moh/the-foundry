// docs/PLAN.md Task 97 (FND-15R): rootless-podman verification lane.
//
// Task 34's own "rootless OCI executor sandbox" card never actually
// exercised rootless podman — every lane it shipped with, including the
// bare-runner sandbox-tests CI job its own Status line calls authoritative,
// runs plain (rootful, daemon-owned) docker. That proves real in-container
// non-root isolation (--user 10001:10001, --cap-drop=ALL) but says nothing
// about engine-level rootlessness: whether the container ENGINE itself ever
// runs as host root.
//
// This file adds the one test that actually distinguishes the two
// properties: it inspects the HOST-side UID that owns a running container's
// process (via /proc/<pid>/status, which only the host kernel — not
// anything running inside the container's own user namespace — can see).
// Under rootless podman, the container's process is launched directly by
// the invoking unprivileged user via a user-namespace + /etc/subuid mapping
// (no root daemon ever execs it), so the host sees that unprivileged UID,
// never 0. Under plain rootful docker (or rootful podman), a root daemon
// execs the container process directly, so the host sees UID 0 regardless
// of whatever --user the container itself was launched with — this is
// exactly the property engine-level "rootless" claims and in-container
// non-root isolation does not, on its own, prove.
//
// This requires a genuine Linux host with /proc (there is no /proc on
// macOS/Windows Docker Desktop hosts, since the daemon and every container
// process run inside a Linux VM the calling process's own /proc can't see)
// and a real rootless-podman install — see requireRootlessPodman below for
// exactly what is checked before running. Every assertion here fails loudly
// rather than silently no-op-ing if those preconditions hold but the
// property doesn't.
package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rootlessTestImage is deliberately NOT DefaultImage/testImage(): the
// property under test (host-side UID of a container process) is a generic
// container-engine property, independent of this repo's own
// foundry-executor-sandbox image, so this file avoids taking on that
// image's build as a precondition. A small, universally available image is
// enough to hold a process open long enough to inspect. Overridable in case
// a runner's mirror/registry access differs.
func rootlessTestImage() string {
	if v := os.Getenv("FOUNDRY_SANDBOX_ROOTLESS_TEST_IMAGE"); v != "" {
		return v
	}
	return "busybox:latest"
}

// requireRootlessPodman skips unless: RUN_SANDBOX=1 (this package's usual
// real-container gate), the `podman` binary is on PATH (this test is about
// podman specifically, not whatever FOUNDRY_SANDBOX_TEST_ENGINE otherwise
// names for the rest of this package's suite, which defaults to docker),
// podman itself reports it is actually running rootless, and the host is
// Linux (the /proc-based UID inspection this test relies on has no
// equivalent on macOS/Windows Docker-Desktop-style hosts, where the engine
// runs inside a Linux VM the caller's own /proc can't see).
func requireRootlessPodman(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("skipping: rootless-UID verification requires a real Linux host with /proc (got GOOS=%q) — this is expected to run for real only in .github/workflows/ci.yaml's sandbox-tests-rootless job (docs/PLAN.md Task 97)", runtime.GOOS)
	}
	if os.Getenv("RUN_SANDBOX") != "1" {
		t.Skip("skipping: set RUN_SANDBOX=1 to run real-container sandbox tests (docs/PLAN.md Task 97)")
	}
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skipf("skipping: podman not found on PATH: %v — this test proves a property specific to rootless podman, not exercisable via docker (docs/PLAN.md Task 97)", err)
	}
	out, err := exec.Command("podman", "info", "--format", "{{.Host.Security.Rootless}}").CombinedOutput()
	if err != nil {
		t.Skipf("skipping: `podman info` failed: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "true" {
		t.Skipf("skipping: podman reports Host.Security.Rootless=%q, not \"true\" — this test verifies the rootless property specifically and does not apply to a rootful podman install", got)
	}
}

// containerHostPID returns the PID of name's main process as the HOST
// (not the container's own) PID namespace sees it — the same value
// `<engine> top`/`ps` on the host would show.
func containerHostPID(t *testing.T, engine, name string) int {
	t.Helper()
	out, err := exec.Command(engine, "inspect", "--format", "{{.State.Pid}}", name).CombinedOutput()
	if err != nil {
		t.Fatalf("%s inspect --format {{.State.Pid}} %s: %v: %s", engine, name, err, out)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("parse %s inspect PID output %q: %v", engine, strings.TrimSpace(string(out)), err)
	}
	if pid <= 0 {
		t.Fatalf("%s inspect returned non-positive PID %d for container %s", engine, pid, name)
	}
	return pid
}

// hostRealUID reads /proc/<pid>/status on THIS host and returns the real
// UID of the process as the host kernel sees it. For a container process
// under a rootless-podman user-namespace mapping, this is the invoking
// unprivileged user's own host UID — a distinct value from whatever UID
// that same process believes it is running as inside its own user
// namespace (e.g. root, or Config.User's 10001), which is exactly the
// distinction this test exists to make visible. Polls briefly since a
// process that has only just been reported "started" by the engine can, in
// rare cases, need a moment before /proc/<pid> is fully populated.
func hostRealUID(t *testing.T, pid int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			lastErr = err
			time.Sleep(100 * time.Millisecond)
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "Uid:") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				t.Fatalf("unexpected Uid line format in /proc/%d/status: %q", pid, line)
			}
			uid, err := strconv.Atoi(fields[1])
			if err != nil {
				t.Fatalf("parse real UID from %q (line %q): %v", fields[1], line, err)
			}
			return uid
		}
		t.Fatalf("no \"Uid:\" line found in /proc/%d/status", pid)
	}
	t.Fatalf("could not read /proc/%d/status within 5s: %v", pid, lastErr)
	return -1
}

// runDetached launches a long-lived container via engine and returns a
// cleanup-registered name, so the caller can inspect its live PID before it
// exits. Not routed through Runner/buildSandboxRunArgs: those launch
// synchronously (RunCommand blocks until the command exits), which is
// unsuitable for inspecting a still-running process's host PID — this test
// is about the engine's own rootlessness, not this package's network/FS-jail
// topology (already covered by sandbox_test.go's escape/legitimate-egress
// suite, which this same rootless lane also re-runs via
// FOUNDRY_SANDBOX_TEST_ENGINE=podman per docs/PLAN.md Task 97 Steps).
func runDetached(t *testing.T, engine string) string {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatalf("randomID: %v", err)
	}
	name := "foundry-rootless-test-" + id
	args := []string{"run", "-d", "--rm", "--name", name, "--user", "10001:10001", rootlessTestImage(), "sleep", "300"}
	// #nosec G204 -- engine and args are fixed/test-constructed, never
	// derived from unsanitized external input.
	out, err := exec.Command(engine, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s run -d %s: %v: %s", engine, rootlessTestImage(), err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command(engine, "rm", "-f", name).Run()
	})
	return name
}

// TestRootless_ContainerProcessRunsUnderUnprivilegedHostUID is this task's
// (docs/PLAN.md Task 97) primary Acceptance line: proves the container
// process's HOST-side owning UID is the invoking unprivileged user's own
// UID, never root — genuine user-namespace remapping, distinct from (and
// stronger than) the in-container non-root isolation Task 34 already
// proved. See TestRootless_NegativeControl_RootfulEngineOwnsHostUIDZero
// below for the card-required negative control showing this same assertion
// fails against a plain rootful engine.
func TestRootless_ContainerProcessRunsUnderUnprivilegedHostUID(t *testing.T) {
	requireRootlessPodman(t)

	hostUID := os.Getuid()
	if hostUID == 0 {
		t.Skip("skipping: this test's assertion (host-side UID != 0) is meaningless when the test process itself is root — CI runners for this job must run as a non-root user, matching rootless podman's own precondition")
	}

	name := runDetached(t, "podman")
	pid := containerHostPID(t, "podman", name)
	gotUID := hostRealUID(t, pid)

	if gotUID == 0 {
		t.Fatalf("expected the container process's HOST-side UID to be non-root under rootless podman's user-namespace remapping, got 0 (root) — pid=%d, container=%s", pid, name)
	}
	if gotUID != hostUID {
		t.Fatalf("expected the container process's HOST-side UID to equal the invoking unprivileged user's UID %d (rootless podman maps the container's namespace via /etc/subuid to the invoking user, not to a separate root daemon), got %d — pid=%d, container=%s", hostUID, gotUID, pid, name)
	}
	t.Logf("confirmed: container %s's host-visible process (pid=%d) is owned by unprivileged host uid %d, not root — genuine engine-level user-namespace remapping, distinct from and stronger than in-container --user 10001:10001 non-root isolation (already proven by sandbox_test.go's escape tests)", name, pid, gotUID)
}

// TestRootless_NegativeControl_RootfulEngineOwnsHostUIDZero is the negative
// control docs/PLAN.md Task 97's Acceptance line explicitly requires:
// "a negative control showing the same assertion would fail against plain
// rootful docker/podman." Run against plain docker (guaranteed rootful,
// root-daemon-owned, and already this package's own default reference
// engine elsewhere in this test suite): a root daemon execs the container
// process directly, so the host sees UID 0 regardless of the container's
// own --user. If this test ever observes a non-zero host UID, the negative
// control itself is broken (e.g. run against an unexpectedly rootless or
// userns-remapped docker install), which would undermine confidence that
// the positive test above is asserting something meaningful rather than a
// tautology that would pass against any engine.
func TestRootless_NegativeControl_RootfulEngineOwnsHostUIDZero(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("skipping: /proc-based UID inspection requires a real Linux host (got GOOS=%q)", runtime.GOOS)
	}
	if os.Getenv("RUN_SANDBOX") != "1" {
		t.Skip("skipping: set RUN_SANDBOX=1 to run real-container sandbox tests (docs/PLAN.md Task 97)")
	}
	const engine = "docker"
	if _, err := exec.LookPath(engine); err != nil {
		t.Skipf("skipping: %s not found on PATH: %v", engine, err)
	}

	name := runDetached(t, engine)
	pid := containerHostPID(t, engine, name)
	gotUID := hostRealUID(t, pid)

	if gotUID != 0 {
		t.Fatalf("negative control failed: expected plain rootful docker's container process to be owned by host UID 0 (root daemon execs container processes directly), got %d — pid=%d, container=%s; this means the positive rootless assertion above is not being meaningfully distinguished from a rootful engine on this host", gotUID, pid, name)
	}
	t.Logf("negative control confirmed: plain rootful docker's container process (pid=%d) IS owned by host root (uid 0) — proving TestRootless_ContainerProcessRunsUnderUnprivilegedHostUID's assertion is a real, engine-dependent property, not a tautology that would pass against any engine", pid)
}
