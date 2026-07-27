package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/okfriansyah-moh/the-foundry/internal/secrets/filestore"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("planrunner", flag.ContinueOnError)
	planPath := fs.String("plan", "docs/PLAN.md", "path to the PLAN.md-shaped file to drive")
	mode := fs.String("mode", "real", "real (invokes claude -p, git, telegram) or dryrun (in-memory fakes, for test/planrunner_dryrun.sh)")
	onlyTask := fs.Int("only-task", 0, "restrict this run to a single task number (0 = process the whole eligible backlog)")
	failTasks := fs.String("fail-task", "", "dryrun only: comma-separated task numbers whose Validate always fails, to exercise the halt path")
	autoApprove := fs.Bool("auto-approve", true, "dryrun only: automatically approve GATED tasks after NotifyGated, to exercise the approval path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	plan, err := ParsePlan(*planPath)
	if err != nil {
		slog.Error("parse plan", "error", err)
		return 1
	}

	var (
		impl  Implementer
		val   Validator
		scm   SCM
		notif Notifier
	)

	switch *mode {
	case "dryrun":
		impl = &fakeImplementer{}
		val = &fakeValidator{failTasks: parseIntSet(*failTasks)}
		scm = &fakeSCM{}
		notif = &fakeNotifier{autoApprove: *autoApprove}
	case "real":
		impl = &realImplementer{}
		val = &realValidator{}
		scm = &realSCM{}
		token := resolveTelegramToken()
		chatID := os.Getenv("TELEGRAM_CHAT_ID")
		if token == "" || chatID == "" {
			slog.Error("real mode requires a Telegram bot token (secrets store or TELEGRAM_BOT_TOKEN env var) and TELEGRAM_CHAT_ID (see .env.example)")
			return 1
		}
		notif = NewTelegramNotifier(token, chatID)
	default:
		slog.Error("unknown -mode", "mode", *mode)
		return 2
	}

	runner := NewRunner(plan, impl, val, scm, notif)
	ctx := context.Background()

	var outcomes []Outcome
	if *onlyTask != 0 {
		outcomes = []Outcome{runner.RunTask(ctx, *onlyTask)}
	} else {
		outcomes = runner.RunAll(ctx)
	}

	exitCode := 0
	for _, o := range outcomes {
		fmt.Printf("%s task=%d tier=%s reason=%q\n", strings.ToUpper(o.Status), o.Task, o.Tier, o.Reason)
		if o.Status == "halted" || o.Status == "error" {
			exitCode = 1
		}
	}
	return exitCode
}

// planrunnerSecretsScope is the fixed profile scope this tool reads its
// own bootstrap Telegram token under. tools/planrunner drives building
// Foundry itself, before Foundry has any real profiles of its own, so a
// fixed scope name stands in for a real profile ID here.
const planrunnerSecretsScope = "bootstrap"

// telegramTokenSecretName is the secret name resolveTelegramToken reads
// (docs/PLAN.md Task 35 / FND-16's secrets seam).
const telegramTokenSecretName = "telegram_bot_token"

// resolveTelegramToken migrates this tool off a bare TELEGRAM_BOT_TOKEN
// env var and behind Task 35's secrets seam: it tries
// internal/secrets/filestore first, falling back to the original
// TELEGRAM_BOT_TOKEN env var (the .env.example-documented path) when the
// secrets store has nothing provisioned under planrunnerSecretsScope —
// so an existing .env-based bootstrap keeps working unchanged until it
// chooses to provision the secrets store instead.
func resolveTelegramToken() string {
	if path, err := filestore.DefaultPath(); err == nil {
		store := filestore.New(path, filestore.DefaultKeySource())
		if v, err := store.Get(context.Background(), planrunnerSecretsScope, telegramTokenSecretName); err == nil {
			return v
		}
	}
	return os.Getenv("TELEGRAM_BOT_TOKEN")
}

func parseIntSet(csv string) map[int]bool {
	set := map[int]bool{}
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil {
			set[n] = true
		}
	}
	return set
}

// --- real mode: shells out to the dev container / plain git / Telegram. Never touches
// internal/* directly (Task 3 Scope/Out of scope; Constitution C4). ---

type realImplementer struct{}

// Implement runs the implementation protocol headlessly inside the dev container
// (Task 3 Step 4), the same toolchain image every other make target uses (§C).
func (r *realImplementer) Implement(ctx context.Context, card *Card) error {
	prompt := buildPrompt(card)
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", "deploy/docker-compose.yaml",
		"run", "--rm", "-T", "dev", "claude", "-p", "--dangerously-skip-permissions")
	cmd.Stdin = strings.NewReader(prompt)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("headless implement task %d: %w: %s", card.Task, err, truncate(out.String(), 4000))
	}
	slog.Info("implement complete", "task", card.Task, "output", truncate(out.String(), 500))
	return nil
}

func buildPrompt(card *Card) string {
	var b strings.Builder
	for _, f := range []string{"AGENTS.md", ".ai/skills/task-implementation/SKILL.md"} {
		if content, err := os.ReadFile(f); err == nil {
			b.Write(content)
			b.WriteString("\n\n")
		}
	}
	b.WriteString("Implement exactly this task card:\n\n")
	b.WriteString(card.Body)
	return b.String()
}

type realValidator struct{}

// Validate runs the card's own Validation commands, then the repo-wide gate
// (`make test && make fitness`), exactly as Task 3 Step 4 and docs/PLAN.md §A step 4
// require.
func (r *realValidator) Validate(ctx context.Context, card *Card) (bool, string, error) {
	var out strings.Builder
	for _, cmdStr := range card.Validation {
		cmd := exec.CommandContext(ctx, "docker", "compose", "-f", "deploy/docker-compose.yaml",
			"run", "--rm", "-T", "dev", "bash", "-c", cmdStr)
		out.WriteString(fmt.Sprintf("$ %s\n", cmdStr))
		res, err := cmd.CombinedOutput()
		out.Write(res)
		if err != nil {
			return false, out.String(), fmt.Errorf("validation command %q: %w", cmdStr, err)
		}
	}
	for _, target := range [][]string{{"make", "test"}, {"make", "fitness"}} {
		cmd := exec.CommandContext(ctx, target[0], target[1:]...)
		out.WriteString(fmt.Sprintf("$ %s\n", strings.Join(target, " ")))
		res, err := cmd.CombinedOutput()
		out.Write(res)
		if err != nil {
			return false, out.String(), fmt.Errorf("%s: %w", strings.Join(target, " "), err)
		}
	}
	return true, out.String(), nil
}

type realSCM struct{}

// Commit implements the manual-protocol Git step (docs/PLAN.md §A step 5/§C): branch
// `task/<N>-<slug>`, conventional commit with a `Task: <N>` footer, merged to main. Every
// argument is passed as its own exec.Command argument — never through a shell string —
// so untrusted card content can never be interpreted as a shell command (OWASP A05).
func (r *realSCM) Commit(ctx context.Context, card *Card) error {
	slug := slugify(card.Alias)
	branch := fmt.Sprintf("task/%d-%s", card.Task, slug)
	steps := [][]string{
		{"git", "checkout", "-b", branch},
		{"git", "add", "-A"},
		{"git", "commit", "-m", fmt.Sprintf("task %d: %s\n\nTask: %d", card.Task, card.Title, card.Task)},
		{"git", "checkout", "main"},
		{"git", "merge", "--no-ff", branch, "-m", fmt.Sprintf("merge task %d\n\nTask: %d", card.Task, card.Task)},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, truncate(string(out), 2000))
		}
	}
	return nil
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// --- dryrun mode: in-memory fakes for test/planrunner_dryrun.sh. Never shells out,
// never touches this repo's real git state or a live Telegram bot. ---

type fakeImplementer struct{}

func (f *fakeImplementer) Implement(ctx context.Context, card *Card) error { return nil }

type fakeValidator struct{ failTasks map[int]bool }

func (f *fakeValidator) Validate(ctx context.Context, card *Card) (bool, string, error) {
	if f.failTasks[card.Task] {
		return false, "simulated validation failure", nil
	}
	return true, "simulated validation ok", nil
}

type fakeSCM struct{}

func (f *fakeSCM) Commit(ctx context.Context, card *Card) error { return nil }

type fakeNotifier struct{ autoApprove bool }

func (f *fakeNotifier) NotifyGated(ctx context.Context, card *Card, reason, validationOutput string) error {
	fmt.Printf("GATED_PENDING task=%d reason=%q\n", card.Task, reason)
	return nil
}

func (f *fakeNotifier) NotifyHalt(ctx context.Context, card *Card, reason string) error {
	fmt.Printf("HALT_ALERT task=%d reason=%q\n", card.Task, reason)
	return nil
}

func (f *fakeNotifier) QueueDigest(card *Card) {
	fmt.Printf("DIGEST_QUEUED task=%d\n", card.Task)
}

func (f *fakeNotifier) FlushDigest(ctx context.Context) error { return nil }

func (f *fakeNotifier) WaitApproval(ctx context.Context, card *Card) (bool, error) {
	if !f.autoApprove {
		return false, nil
	}
	time.Sleep(10 * time.Millisecond) // simulate the /approve round trip
	return true, nil
}

func (f *fakeNotifier) Frozen(ctx context.Context) bool { return false }
