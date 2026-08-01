// Command foundryd is the Temporal worker process hosting the kernel's
// DeliverPlan workflow and its activities (docs/PLAN.md Task 12, SKP-10).
// It is the only process that ever performs the side effects the kernel
// owns (Constitution C4) — worktree mutation, executor invocation,
// evidence persistence, transition/lease/receipt storage.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	_ "github.com/jackc/pgx/v5/stdlib"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"

	"github.com/okfriansyah-moh/the-foundry/internal/api"
	"github.com/okfriansyah-moh/the-foundry/internal/authn"
	"github.com/okfriansyah-moh/the-foundry/internal/billing"
	"github.com/okfriansyah-moh/the-foundry/internal/deploy"
	"github.com/okfriansyah-moh/the-foundry/internal/evidence"
	"github.com/okfriansyah-moh/the-foundry/internal/executor/capability"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/claudecode"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/copilot"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/cursor"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/fake"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/geminicli"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/local"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/openai"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/opencode"
	_ "github.com/okfriansyah-moh/the-foundry/internal/executor/windsurf"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel"
	"github.com/okfriansyah-moh/the-foundry/internal/kernel/integrator"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/cost"
	"github.com/okfriansyah-moh/the-foundry/internal/ledger/extops"
	"github.com/okfriansyah-moh/the-foundry/internal/mission"
	"github.com/okfriansyah-moh/the-foundry/internal/notify"
	"github.com/okfriansyah-moh/the-foundry/internal/observe"
	"github.com/okfriansyah-moh/the-foundry/internal/opportunity"
	"github.com/okfriansyah-moh/the-foundry/internal/policy/compiler"
	"github.com/okfriansyah-moh/the-foundry/internal/policy/pdp"
	"github.com/okfriansyah-moh/the-foundry/internal/profile"
	"github.com/okfriansyah-moh/the-foundry/internal/provenance"
	"github.com/okfriansyah-moh/the-foundry/internal/recovery"
	"github.com/okfriansyah-moh/the-foundry/internal/verify"
	"github.com/okfriansyah-moh/the-foundry/internal/worktree"
)

// queuePriorityConfigPath is docs/PLAN.md Task 96 (FND-14R2)'s
// config/queue-priority.yaml (Task 33) — the source of the four lanes
// this process now registers one Temporal worker per lane for, replacing
// the single hardcoded "foundry-core" task queue Task 12 Step 8 started
// with.

// recoveryScanInterval is docs/PLAN.md Task 94 (FND-13R)'s liveness
// supervisor scan cadence. Not env-configurable — no task asks for that,
// and disaster-recovery.md §20.10's own stall thresholds (5m StaleAfter,
// 30m NoProgressAfter, both recovery.Config zero-value defaults) are what
// actually bound detection latency, not this loop's tick.
const recoveryScanInterval = time.Minute

// notifyTickInterval/notifyClaimLimit size docs/PLAN.md Task 30's
// notify.Engine delivery loop (Engine.Run's own doc comment names
// cmd/foundryd as its production caller) — wired into foundryd for the
// first time by Task 94, which needs a real, delivering Notifier for its
// recovery.Supervisor. Previously this Engine was only ever constructed
// by test harnesses (test/soak/telegram, test/drill/brownout).
const (
	notifyTickInterval = 5 * time.Second
	notifyClaimLimit   = 20
)

// apiRateLimitPerSecond/apiRateLimitBurst/apiIntakeQueueCapacity size
// docs/PLAN.md Task 95's control-plane self-protection for internal/api's
// real routes (docs/foundry/docs/operations/control-plane-protection.md).
// No task or governing doc names a specific number, so — per the plan's
// No-gaps rule — these are the smallest reversible defaults: generous
// enough that no legitimate single-principal caller (the CLI, a human
// operator) is throttled in normal use, tight enough to bound a runaway
// or malicious caller. Not env-configurable — no task asks for that, and
// a hardcoded default is the cheaper-to-reverse choice over inventing a
// config surface no one has asked for yet.
// decision: apiRateLimitPerSecond=10, apiRateLimitBurst=20,
// apiIntakeQueueCapacity=200 (Task 95, 2026-07-26).
const (
	apiRateLimitPerSecond  = 10
	apiRateLimitBurst      = 20
	apiIntakeQueueCapacity = 200
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("foundryd: %v", err)
	}
}

func run() error {
	temporalHostPort := envOr("TEMPORAL_HOSTPORT", "temporal:7233")
	pgDSN := envOr("PG_DSN", "postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable")
	keyDir := os.Getenv("FOUNDRY_KEY_DIR")
	worktreeRoot := envOr("FOUNDRY_WORKTREE_ROOT", "/var/lib/foundry/worktrees")
	evidenceRoot := envOr("FOUNDRY_EVIDENCE_ROOT", "/var/lib/foundry/evidence")
	costDefaultsPath := envOr("FOUNDRY_COST_DEFAULTS", "config/cost-defaults.yaml")
	validationAllowlistPath := envOr("FOUNDRY_VALIDATION_ALLOWLIST", "config/validation-allowlist.yaml")
	metricsAddr := envOr("FOUNDRY_METRICS_ADDR", observe.DefaultMetricsAddr)

	// bgCtx outlives run()'s own setup work and is only cancelled once
	// w.Run below returns — it exists solely to bound the metrics
	// server's lifetime to this process's, not to any single request.
	bgCtx, cancelBg := context.WithCancel(context.Background())
	defer cancelBg()

	// OTel trace wiring (docs/PLAN.md Task 31): opt-in via
	// observe.EnvTracingEnabled; SetupTracing returns a no-op shutdown when
	// unset, so this is always safe to call. The Temporal client
	// interceptor below resolves its tracer from whatever global provider
	// this call installs (a real one, or OTel's own zero-cost no-op).
	shutdownTracing, err := observe.SetupTracing(bgCtx, "foundryd")
	if err != nil {
		return fmt.Errorf("setup tracing: %w", err)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	tracingInterceptor, err := temporalotel.NewTracingInterceptor(temporalotel.TracerOptions{})
	if err != nil {
		return fmt.Errorf("build temporal tracing interceptor: %w", err)
	}

	// Prometheus /metrics (docs/PLAN.md Task 31): served for this
	// process's lifetime; Serve returns (nil) once bgCtx is cancelled
	// below, after w.Run returns.
	go func() {
		if err := observe.Serve(bgCtx, metricsAddr); err != nil {
			log.Printf("foundryd: metrics server: %v", err)
		}
	}()

	if keyDir == "" {
		d, err := provenance.DefaultKeyDir()
		if err != nil {
			return fmt.Errorf("resolve key dir: %w", err)
		}
		keyDir = d
	}
	// approverKeys is the Ed25519 keypair used both to verify ApprovedPlan
	// signatures (Public, as before this task) and — new in this task —
	// to (re-)sign an ApprovedPlan when internal/api's approve route
	// records a new Approver (mirrors cmd/foundry/plan_approve.go's own
	// use of the same keypair for the initial approval).
	approverKeys, err := provenance.LoadKeyPair(keyDir)
	if err != nil {
		return fmt.Errorf("load approver keypair: %w", err)
	}

	db, err := sql.Open("pgx", pgDSN)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	rawStore, err := provenance.OpenPGRawStore(pgDSN)
	if err != nil {
		return fmt.Errorf("open provenance store: %w", err)
	}
	defer func() { _ = rawStore.Close() }()

	costDefaults, err := cost.LoadDefaults(costDefaultsPath)
	if err != nil {
		return fmt.Errorf("load cost defaults: %w", err)
	}

	// docs/PLAN.md Task 99 (SKP-11R): the real, honest validator — Task
	// 13's own internal/verify.Runner, loaded against the same allowlist
	// its own Constitution C10 contract has always required, finally wired
	// into the kernel's ValidateTask activity instead of trusting the
	// executor's self-report.
	validationAllowlist, err := verify.LoadAllowlist(validationAllowlistPath)
	if err != nil {
		return fmt.Errorf("load validation allowlist: %w", err)
	}
	evidenceStore, err := buildEvidenceStore(bgCtx, evidenceRoot)
	if err != nil {
		return err
	}

	activities := kernel.NewActivities(
		provenance.NewStore(rawStore, approverKeys.Public),
		&worktree.Manager{Root: worktreeRoot},
		evidenceStore,
		kernel.NewPGLeaseStore(db),
		kernel.NewPGReceiptStore(db),
		kernel.NewPGTransitionStore(db),
		cost.NewStore(db),
		costDefaults,
		verify.NewRunner(validationAllowlist),
	)

	// docs/PLAN.md Task 102 (OPP-03): the kernel-owned opportunity verdict
	// gate. It re-derives the scorecard from stored evidence and refuses any
	// build whose BUILD verdict is missing, expired, unreproducible, out of
	// envelope or unbacked by a real validation signal (Constitution C4/C23).
	// RealSignal is fail-closed (DenyRealSignal) until Task 139 supplies the
	// allowlisted real-signal verifier.
	oppConfig, err := opportunity.LoadConfig(envOr("FOUNDRY_OPPORTUNITY_THRESHOLDS", "config/opportunity-thresholds.yaml"))
	if err != nil {
		return fmt.Errorf("load opportunity thresholds: %w", err)
	}
	oppGate := &kernel.OpportunityGate{
		Loader:     opportunity.NewStore(db),
		Config:     oppConfig,
		Reserver:   oppValidationReserver{store: cost.NewStore(db)},
		RealSignal: kernel.DenyRealSignal{},
	}

	// docs/PLAN.md Task 106 (RTC-02): construct MissionLoop's activities from
	// the real Postgres mission store, cost store and net-MRR source, and
	// register MissionLoop + its eight activities on the lane workers so
	// `foundry mission pause|kill|start|resume` signal a workflow a production
	// worker is actually running (Constitution C2/C18).
	missionStore := mission.NewStore(db)
	// docs/PLAN.md Task 126 (VEN-16): when a Stripe test-mode key is present,
	// wire the real revenue reconciler and feed mission observation from real
	// revenue rows; otherwise mission observation stays on the unimplemented
	// source. Test mode only — a live-mode key refuses to load while maturity
	// is immature (enforced in internal/billing).
	var netMRRSource mission.NetMRRSource = mission.UnimplementedNetMRRSource{}
	if stripeKey := os.Getenv("STRIPE_TEST_KEY"); stripeKey != "" {
		stripeClient, serr := billing.NewStripeClient(billing.ClientConfig{SecretKey: stripeKey, MaturityCriteria: billing.DefaultMaturityCriteria()})
		if serr != nil {
			return fmt.Errorf("construct Stripe client: %w", serr)
		}
		revenueStore := billing.NewRevenueStore(db)
		reconciler := billing.NewReconciler(stripeClient, revenueStore)
		netMRRSource = billing.MissionNetMRRSource{Store: revenueStore}
		go func() {
			ticker := time.NewTicker(15 * time.Minute)
			defer ticker.Stop()
			for {
				if rerr := reconciler.Run(bgCtx); rerr != nil && !errors.Is(rerr, context.Canceled) {
					log.Printf("foundryd: Stripe revenue reconcile: %v", rerr)
				}
				select {
				case <-bgCtx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	missionActs := mission.NewActivities(
		missionStore, // LoopContractChecker
		missionStore, // ReadinessChecker
		missionStore, // MissionStateRecorder
		missionStore, // GateEventRecorder
		missionStore, // GateEventResolver
		kernel.NewPGTransitionStore(db),
		cost.NewStore(db),
		netMRRSource,
		kernel.NewPGReceiptStore(db),
	)

	// docs/PLAN.md Task 121 (MMR-01): the durable portfolio supervisor. All
	// mutable portfolio state (activation, spend-to-date, fair-schedule
	// counter) lives in Postgres via PortfolioStore, so the active-mission
	// cap, per-mission budget isolation and the fairness bound survive a
	// foundryd restart instead of resetting to zero. ReconcilePortfolio is the
	// single durable step PortfolioLoop drives.
	portfolioStore := mission.NewPortfolioStore(db)
	portfolioActs := &mission.PortfolioActivities{Portfolio: portfolioStore, Missions: missionStore}

	// docs/PLAN.md Tasks 84/85/90 (PRV-01/02/07): wire the kernel-owned
	// executor selector so that when a DeliverPlanInput carries a resolved
	// executor_allowlist, ExecuteTask selects (and policy-checks) the adapter
	// inside internal/kernel instead of using an unchecked name (Constitution
	// C4). Loaded from the same config the fitness lint validates; a nil
	// allowlist still uses the pre-Task-85 legacy path, so this is
	// backward-compatible for callers that don't resolve policy.
	capRegistry, err := capability.Load(envOr("FOUNDRY_EXECUTOR_CAPABILITIES", "config/executor-capabilities.yaml"))
	if err != nil {
		return fmt.Errorf("load executor capability registry: %w", err)
	}
	routingTable, err := kernel.LoadRoutingTable(envOr("FOUNDRY_EXECUTOR_ROUTING", "config/executor-routing.yaml"))
	if err != nil {
		return fmt.Errorf("load executor routing table: %w", err)
	}
	activities.CapabilityRegistry = capRegistry
	// Task 120 (COST-02): load the per-model rate table so completed tasks
	// incur a real, reconcilable cost (unknown when a model has no rate, never
	// a fabricated default).
	if rates, rerr := cost.LoadRateTable(envOr("FOUNDRY_MODEL_RATES", "config/executor-model-rates.yaml")); rerr != nil {
		log.Printf("foundryd: model rate table unavailable (costs recorded as unknown): %v", rerr)
	} else {
		activities.ModelRates = rates
	}
	// Task 115 (SEC-01): wire the production sandbox runner so ExecuteTask runs
	// sandbox-required executors inside the sandbox and refuses host execution.
	wireSandbox(activities)
	// Task 123 (MMR-03): wire the failure-signature store so runTask records a
	// normalized signature per failed attempt, giving the liveness supervisor's
	// PoisonedTask/InfiniteRetry conditions live data to classify against.
	activities.FailureSignatures = kernel.NewPGFailureSignatureStore(db)
	// Task 125 (VEN-15): wire the kernel-owned production deploy path — the
	// real Fly adapter (only when a scoped token is present), the extops ledger
	// for idempotency/receipts, and the profile quota enforcer so deploys are
	// bounded. A missing FLY_API_TOKEN leaves DeployProduct unwired; it errors
	// if ever invoked rather than deploying with no credential.
	activities.ExternalOps = extops.NewStore(db)
	if flyToken := os.Getenv("FLY_API_TOKEN"); flyToken != "" {
		activities.DeployAdapter = deploy.FlyAdapter{Token: flyToken}
	}
	if deployQuotas, qerr := deploy.LoadQuotas(envOr("FOUNDRY_QUOTAS", "config/quotas.yaml")); qerr != nil {
		log.Printf("foundryd: deploy quota enforcer unavailable: %v", qerr)
	} else {
		activities.DeployQuota = deploy.NewQuotaEnforcer(deployQuotas)
	}
	// docs/PLAN.md Task 108 (RTC-04): wire the durable Postgres integration
	// queue so the 10x IntegrateChangeSet activity has a real reader/writer.
	// The Integrator's CAS pusher is supplied once Task 140 selects a
	// policy-derived SCM provider; until then IntegrateChangeSet fails closed.
	activities.IntegrationQueue = integrator.NewPGQueue(db)
	activities.ExecutorSelector = kernel.ExecutorSelector{
		Default: envOr("FOUNDRY_EXECUTOR", "claude-code"),
		Routing: routingTable,
		Profile: envOr("FOUNDRY_PROFILE", "personal"),
	}
	// Task 129 (INF-02): the provider-fallback health circuit-breaker and the
	// per-task attempt budget, so an unavailable/rate-limited provider fails
	// over to the next policy-allowed candidate (bounded, fail-closed).
	activities.ExecutorHealth = capability.NewHealthTracker()
	if budget, berr := kernel.LoadFallbackAttemptBudget(envOr("FOUNDRY_EXECUTOR_ROUTING", "config/executor-routing.yaml")); berr != nil {
		log.Printf("foundryd: fallback attempt budget unavailable (using default): %v", berr)
	} else {
		activities.FallbackAttemptBudget = budget
	}

	// docs/PLAN.md Task 36 (FND-17): foundryd's HTTP API, served
	// alongside the Temporal worker for this process's lifetime, bound
	// to bgCtx the same way the metrics server above is.
	apiServer, err := buildAPIServer(bgCtx, db, temporalHostPort, pgDSN, rawStore, approverKeys, evidenceStore)
	if err != nil {
		return fmt.Errorf("build api server: %w", err)
	}
	apiAddr := envOr("FOUNDRY_API_ADDR", ":8081")
	httpSrv := &http.Server{Addr: apiAddr, Handler: apiServer}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("foundryd: api server: %v", err)
		}
	}()
	go func() {
		<-bgCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	c, err := client.Dial(client.Options{
		HostPort:     temporalHostPort,
		Interceptors: []interceptor.ClientInterceptor{tracingInterceptor},
	})
	if err != nil {
		return fmt.Errorf("dial temporal at %s: %w", temporalHostPort, err)
	}
	defer c.Close()

	// docs/PLAN.md Task 94 (FND-13R): the liveness supervisor, running
	// alongside the metrics/API-server goroutines above for this
	// process's lifetime, bound to bgCtx the same way. notifyEngine is a
	// real, delivering internal/notify.Engine (Task 30) — the Postgres
	// queue this writes to is 0007_notifications, the same table Task
	// 30's own tests exercise. FOUNDRY_TELEGRAM_BOT_TOKEN/
	// FOUNDRY_OPS_CHAT_ID name foundryd's own production bot
	// credentials, deliberately distinct from tools/planrunner's
	// disposable bootstrap TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID
	// (.env.example's own comment: "never point it at the same bot
	// Foundry's eventual production Telegram engine will use").
	notifyEngine := notify.NewEngine(
		notify.NewPostgresStore(db),
		&notify.HTTPSender{Token: os.Getenv("FOUNDRY_TELEGRAM_BOT_TOKEN")},
		notify.NewRateLimiter(notify.DefaultLimits()),
		notify.Config{},
	)
	go notifyEngine.Run(bgCtx, notifyTickInterval, notifyClaimLimit)

	// Task 112 (INT-04): the inbound half — a real CommandRouter fed by a
	// durable-offset getUpdates receiver. Env-gated (inert without a bot token).
	startTelegramInbound(bgCtx, db, c, envOr("TEMPORAL_NAMESPACE", "default"))

	supervisor := &recovery.Supervisor{
		Source: &recovery.CompositeProjectionSource{
			PG:         &recovery.PostgresProjectionSource{DB: db},
			Heartbeats: &recovery.TemporalHeartbeatSource{Client: c},
		},
		Controller: &recovery.TemporalController{
			Client:    c,
			Namespace: envOr("TEMPORAL_NAMESPACE", "default"),
		},
		Notifier:  notifyEngine,
		OpsChatID: os.Getenv("FOUNDRY_OPS_CHAT_ID"),
	}
	go func() {
		if err := supervisor.Run(bgCtx, recoveryScanInterval); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("foundryd: recovery supervisor: %v", err)
		}
	}()

	queueCfg, err := observe.LoadQueueConfig(envOr("FOUNDRY_QUEUE_PRIORITY_CONFIG", "config/queue-priority.yaml"))
	if err != nil {
		return fmt.Errorf("load queue priority config: %w", err)
	}

	// docs/PLAN.md Task 96 (FND-14R2): one worker per configured lane,
	// each polling its own lane's Temporal task queue and registering
	// kernel.DeliverPlan + today's activities identically to the prior
	// single-worker registration — 4 workers instead of 1. Which lane a
	// given DeliverPlan execution starts on is internal/kernel.LaneSelector's
	// deterministic config lookup (Constitution C4); no worker here makes
	// that decision, it only polls the queue its lane was assigned.
	workers := make([]worker.Worker, 0, len(queueCfg.Lanes))
	for _, lane := range queueCfg.Lanes {
		lw := worker.New(c, lane.TaskQueue, worker.Options{
			Interceptors: []interceptor.WorkerInterceptor{tracingInterceptor},
		})
		lw.RegisterWorkflow(kernel.DeliverPlan)
		registerActivities(lw, activities)
		lw.RegisterActivityWithOptions(oppGate.RequireBuildVerdict, activity.RegisterOptions{Name: kernel.ActivityRequireBuildVerdict})
		lw.RegisterWorkflow(mission.MissionLoop)
		registerMissionActivities(lw, missionActs)
		lw.RegisterWorkflow(kernel.TenXDeliver)
		lw.RegisterActivityWithOptions(activities.SelectBranchDeliveryPolicy, activity.RegisterOptions{Name: kernel.ActivitySelectBranchDeliveryPolicy})
		lw.RegisterActivityWithOptions(activities.IntegrateChangeSet, activity.RegisterOptions{Name: kernel.ActivityIntegrateChangeSet})
		if err := lw.Start(); err != nil {
			return fmt.Errorf("start worker for lane %q (task queue %q): %w", lane.Name, lane.TaskQueue, err)
		}
		workers = append(workers, lw)
	}

	// docs/PLAN.md Task 121 (MMR-01): the portfolio supervisor runs on its own
	// dedicated Temporal task queue ("its own lane"), so portfolio supervision
	// can neither starve nor be starved by product delivery. A single worker
	// registers PortfolioLoop + its one durable activity.
	portfolioWorker := worker.New(c, mission.PortfolioTaskQueue, worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{tracingInterceptor},
	})
	portfolioWorker.RegisterWorkflow(mission.PortfolioLoop)
	portfolioWorker.RegisterActivityWithOptions(portfolioActs.ReconcilePortfolio, activity.RegisterOptions{Name: mission.ActivityPortfolioReconcile})
	if err := portfolioWorker.Start(); err != nil {
		return fmt.Errorf("start portfolio supervisor worker (task queue %q): %w", mission.PortfolioTaskQueue, err)
	}
	workers = append(workers, portfolioWorker)

	// Ensure the default portfolio exists (cap from the personal profile's
	// max_active_missions quota) and start its supervisor idempotently: a
	// deterministic workflow ID plus ALLOW_DUPLICATE_FAILED_ONLY reuse means a
	// restart re-adopts the running supervisor rather than starting a second
	// one that could race the cap. decision (no-gaps rule, docs/PLAN.md §A):
	// the card leaves portfolio→profile identity unspecified; the smallest
	// reversible choice is one "default" portfolio bounded by the personal
	// quota, recorded here rather than inventing a broader profile-enumeration
	// scheme.
	if err := ensurePortfolioSupervisor(bgCtx, c, portfolioStore, deliveryLaneQueue(queueCfg)); err != nil {
		log.Printf("foundryd: portfolio supervisor not started (portfolio supervision disabled this run): %v", err)
	}
	defer func() {
		for _, lw := range workers {
			lw.Stop()
		}
	}()

	// worker.InterruptCh returns a <-chan interface{} that closes on
	// SIGINT/SIGTERM (docs/PLAN.md Task 12 Step 8) — the same shutdown
	// signal every one of the per-lane workers above now stops on via the
	// deferred Stop() loop, since Start() (unlike the prior single Run())
	// does not block on it itself.
	<-worker.InterruptCh()
	return nil
}

// stripeWebhookHandler returns the signature-verified Stripe webhook handler
// when STRIPE_WEBHOOK_SECRET is configured (docs/PLAN.md Task 126), else nil so
// the route is not mounted.
func stripeWebhookHandler(db *sql.DB) http.Handler {
	secret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if secret == "" {
		return nil
	}
	h, err := billing.NewWebhookHandler(secret, billing.NewEventStore(db))
	if err != nil {
		log.Printf("foundryd: Stripe webhook disabled: %v", err)
		return nil
	}
	return h
}

func buildEvidenceStore(ctx context.Context, evidenceRoot string) (evidence.Store, error) {
	profileID := envOr("FOUNDRY_PROFILE", "personal")
	backend := evidence.Backend(envOr("FOUNDRY_EVIDENCE_BACKEND", string(evidence.BackendFilesystem)))
	requireObjectStore := profileID == "production" || os.Getenv("FOUNDRY_EVIDENCE_REQUIRE_OBJECT_STORE") == "1"

	opts := evidence.StoreForProfileOptions{
		Policy: evidence.StorePolicy{
			ProfileID:          profileID,
			Backend:            backend,
			RequireObjectStore: requireObjectStore,
		},
		FilesystemRoot: evidenceRoot,
		S3: evidence.S3StoreOptions{
			Endpoint:        envOr("FOUNDRY_EVIDENCE_S3_ENDPOINT", "minio:9000"),
			AccessKeyID:     envOr("FOUNDRY_EVIDENCE_S3_ACCESS_KEY", "minioadmin"),
			SecretAccessKey: envOr("FOUNDRY_EVIDENCE_S3_SECRET_KEY", "minioadmin"),
			SessionToken:    os.Getenv("FOUNDRY_EVIDENCE_S3_SESSION_TOKEN"),
			Secure:          os.Getenv("FOUNDRY_EVIDENCE_S3_SECURE") == "1" || strings.EqualFold(os.Getenv("FOUNDRY_EVIDENCE_S3_SECURE"), "true"),
			Bucket:          envOr("FOUNDRY_EVIDENCE_S3_BUCKET", "foundry-evidence"),
			Region:          os.Getenv("FOUNDRY_EVIDENCE_S3_REGION"),
			ProfileID:       profileID,
			CacheDir:        filepath.Join(evidenceRoot, "cache"),
		},
	}
	store, err := evidence.NewStoreForProfile(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("build evidence store: %w", err)
	}
	return store, nil
}

// defaultPortfolioID is the single portfolio the foundryd startup supervises
// by default (see ensurePortfolioSupervisor's no-gaps decision).
const defaultPortfolioID = "default"

// deliveryLaneQueue returns the delivery lane's Temporal task queue so
// MissionLoop children (and their DeliverPlan grandchildren) run on the
// delivery lane rather than the portfolio supervisor's own lane.
func deliveryLaneQueue(cfg observe.QueueConfig) string {
	for _, lane := range cfg.Lanes {
		if lane.Name == "delivery" {
			return lane.TaskQueue
		}
	}
	if len(cfg.Lanes) > 0 {
		return cfg.Lanes[0].TaskQueue
	}
	return ""
}

// ensurePortfolioSupervisor ensures the default portfolio_state row exists
// (cap from the personal profile's max_active_missions quota) and starts its
// PortfolioLoop supervisor idempotently. A deterministic workflow ID plus
// ALLOW_DUPLICATE_FAILED_ONLY reuse means a restart re-adopts the already
// running supervisor rather than starting a second one that could race the cap
// (docs/PLAN.md Task 121).
func ensurePortfolioSupervisor(ctx context.Context, c client.Client, store *mission.PortfolioStore, missionQueue string) error {
	quotas, err := deploy.LoadQuotas(envOr("FOUNDRY_QUOTAS", "config/quotas.yaml"))
	if err != nil {
		return fmt.Errorf("load quotas: %w", err)
	}
	cap := 0
	if q, ok := quotas["personal"]; ok {
		cap = q.MaxActiveMissions
	}
	if err := store.EnsurePortfolio(ctx, defaultPortfolioID, cap); err != nil {
		return fmt.Errorf("ensure portfolio: %w", err)
	}
	_, err = c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    mission.PortfolioWorkflowID(defaultPortfolioID),
		TaskQueue:             mission.PortfolioTaskQueue,
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, mission.PortfolioLoop, mission.PortfolioLoopInput{
		PortfolioID:       defaultPortfolioID,
		MissionTaskQueue:  missionQueue,
		DeliveryTaskQueue: missionQueue,
	})
	if err != nil {
		// An already-running supervisor is success, not an error: the restart
		// re-adopted it rather than double-activating.
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &already) {
			return nil
		}
		return fmt.Errorf("start portfolio supervisor: %w", err)
	}
	return nil
}

// buildAPIServer wires internal/api.Server's real production
// dependencies (docs/PLAN.md Task 36):
//
//   - PDP: the real OPA-backed policy.Decider (internal/policy/pdp,
//     Task 23), compiled from the platform policy layer only — no
//     org/profile/workflow layer loader exists yet (Task 22 only compiles
//     in-memory LayerPolicy values; no task has built a YAML loader for
//     the lower three layers), so this is a platform-only Resolved
//     policy, the smallest reversible choice available without inventing
//     that loader (see config/policy/rego/authz.rego's own doc comment
//     on the "api" action this compiles against).
//   - Session verification: internal/authn's own session keypair
//     (~/.foundry/keys by default, FOUNDRY_SESSION_KEY_DIR to override),
//     generated on first run if absent — the same key `foundry login`
//     must be pointed at for a token this server issues to verify
//     anywhere else (an existing Task 25 assumption, not a new one).
//   - WebAuthn: a Postgres-backed credential store (authn.NewPGUserStore) and a
//     durable, single-use challenge-session store (authn.NewPGSessionStore) —
//     Task 114 (INT-06) makes registered passkeys, in-flight challenges and
//     signature counters survive a foundryd restart, preserving clone detection
//     across it.
func buildAPIServer(ctx context.Context, db *sql.DB, temporalHostPort, pgDSN string, rawStore *provenance.PGRawStore, approverKeys *provenance.KeyPair, evidenceStore evidence.Store) (*api.Server, error) {
	// Task 116 (SEC-02): compile all four policy layers for the profile in
	// force, not platform-only. The org layer (and its kernel-only push rule)
	// and the profile layer are loaded from their real sources; a configured
	// path that fails to load is a hard error, never a silent platform-only
	// fallback. Empty env → that layer is skipped (an empty layer tightens
	// nothing), the smallest reversible default.
	orgLayerPath := os.Getenv("FOUNDRY_ORG_POLICY")
	profileLayerPath := os.Getenv("FOUNDRY_PROFILE_POLICY")
	resolved, _, err := compiler.CompileFourLayer(orgLayerPath, profileLayerPath)
	if err != nil {
		return nil, fmt.Errorf("compile four-layer policy: %w", err)
	}
	bundleDir := envOr("FOUNDRY_POLICY_BUNDLE_DIR", "config/policy/rego")
	bundleDigest, err := pdp.BundleDigest(bundleDir)
	if err != nil {
		return nil, fmt.Errorf("digest rego bundle at %s: %w", bundleDir, err)
	}
	decider, err := pdp.NewOPADecider(ctx, bundleDir, bundleDigest, resolved)
	if err != nil {
		return nil, fmt.Errorf("construct PDP: %w", err)
	}

	sessionKeyDir := os.Getenv("FOUNDRY_SESSION_KEY_DIR")
	if sessionKeyDir == "" {
		d, err := authn.DefaultSessionKeyDir()
		if err != nil {
			return nil, fmt.Errorf("resolve session key dir: %w", err)
		}
		sessionKeyDir = d
	}
	sessionKey, err := authn.LoadOrGenerateSessionKey(sessionKeyDir)
	if err != nil {
		return nil, fmt.Errorf("load session signing key: %w", err)
	}

	// Task 114 (INT-06): a startup check that names the missing IdP variable
	// when strong auth is enabled but no issuer is configured, rather than
	// failing at first login with a bare error.
	if err := checkStrongAuthIdP(); err != nil {
		return nil, err
	}

	waSvc, err := authn.NewServiceWithSessions(&webauthn.Config{
		RPID:          envOr("FOUNDRY_WEBAUTHN_RPID", "localhost"),
		RPDisplayName: "Foundry",
		RPOrigins:     []string{envOr("FOUNDRY_WEBAUTHN_ORIGIN", "http://localhost:8081")},
	}, authn.NewPGUserStore(db), authn.NewPGSessionStore(db))
	if err != nil {
		return nil, fmt.Errorf("construct webauthn service: %w", err)
	}

	profileRaw, err := profile.OpenPGRawStore(pgDSN)
	if err != nil {
		return nil, fmt.Errorf("open profile store: %w", err)
	}

	// docs/PLAN.md Task 105 (RTC-01): resolve the platform-layer executor
	// allowlist and the lane queue config the deliver route hands to
	// kernel.StartDelivery. Until Task 116 supplies compiled policy layers,
	// the platform allowlist is the set of supported executors in the
	// capability registry — non-empty, never nil (Constitution C4).
	deliverQueueCfg, err := observe.LoadQueueConfig(envOr("FOUNDRY_QUEUE_PRIORITY_CONFIG", "config/queue-priority.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load queue config for deliver route: %w", err)
	}
	deliverCapRegistry, err := capability.Load(envOr("FOUNDRY_EXECUTOR_CAPABILITIES", "config/executor-capabilities.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load capability registry for deliver route: %w", err)
	}
	var deliverAllowlist []string
	for _, rec := range deliverCapRegistry.Executors {
		if rec.Availability == capability.AvailabilitySupported {
			deliverAllowlist = append(deliverAllowlist, rec.Provider)
		}
	}

	return api.NewServer(api.Dependencies{
		DB:                       db,
		TemporalHostPort:         temporalHostPort,
		TemporalNamespace:        envOr("TEMPORAL_NAMESPACE", "default"),
		Evidence:                 evidenceStore,
		Profiles:                 profile.NewStore(profileRaw),
		Provenance:               provenance.NewStore(rawStore, approverKeys.Public),
		QueueConfig:              deliverQueueCfg,
		DeliverExecutorAllowlist: deliverAllowlist,
		ApprovalSigningKey:       approverKeys.Private,
		SessionPub:               &sessionKey.PublicKey,
		WebAuthn:                 waSvc,
		Decider:                  decider,
		PolicyDigest:             resolved.Digest,
		// docs/PLAN.md Task 126 (VEN-16): the signature-verified, durable
		// Stripe webhook endpoint, mounted only when a signing secret is
		// configured (test mode).
		StripeWebhook: stripeWebhookHandler(db),
		// docs/PLAN.md Task 95: mount observe.Middleware's rate limit +
		// bounded admission in front of this server's real routes — Task
		// 33's own internal/observe middleware protected nothing until
		// this wiring.
		RateLimiter: observe.NewLimiter(apiRateLimitPerSecond, apiRateLimitBurst),
		IntakeQueue: observe.NewIntakeQueue("api", apiIntakeQueueCapacity),
	})
}

// registerActivities registers each Activities method under the name
// workflow.go's activity constants reference, so the workflow's
// ExecuteActivity-by-name calls resolve here.
func registerActivities(w worker.Worker, a *kernel.Activities) {
	w.RegisterActivityWithOptions(a.LoadApprovedPlan, activity.RegisterOptions{Name: kernel.ActivityLoadApprovedPlan})
	w.RegisterActivityWithOptions(a.RecheckApproval, activity.RegisterOptions{Name: kernel.ActivityRecheckApproval})
	w.RegisterActivityWithOptions(a.ReserveBudget, activity.RegisterOptions{Name: kernel.ActivityReserveBudget})
	w.RegisterActivityWithOptions(a.AcquireLease, activity.RegisterOptions{Name: kernel.ActivityAcquireLease})
	w.RegisterActivityWithOptions(a.AcquireWorktree, activity.RegisterOptions{Name: kernel.ActivityAcquireWorktree})
	w.RegisterActivityWithOptions(a.ReleaseWorktree, activity.RegisterOptions{Name: kernel.ActivityReleaseWorktree})
	w.RegisterActivityWithOptions(a.ExecuteTask, activity.RegisterOptions{Name: kernel.ActivityExecuteTask})
	w.RegisterActivityWithOptions(a.ValidateTask, activity.RegisterOptions{Name: kernel.ActivityValidateTask})
	w.RegisterActivityWithOptions(a.RecordEvidence, activity.RegisterOptions{Name: kernel.ActivityRecordEvidence})
	w.RegisterActivityWithOptions(a.AppendTransition, activity.RegisterOptions{Name: kernel.ActivityAppendTransition})
	w.RegisterActivityWithOptions(a.RecordCost, activity.RegisterOptions{Name: kernel.ActivityRecordCost})
	w.RegisterActivityWithOptions(a.RecordFailureSignature, activity.RegisterOptions{Name: kernel.ActivityRecordFailureSignature})
	w.RegisterActivityWithOptions(a.DeployProduct, activity.RegisterOptions{Name: kernel.ActivityDeployProduct})
}

// registerMissionActivities registers MissionLoop's eight activities under the
// names internal/mission/workflow.go references (docs/PLAN.md Task 106).
func registerMissionActivities(w worker.Worker, a *mission.Activities) {
	w.RegisterActivityWithOptions(a.RequireLoopContract, activity.RegisterOptions{Name: mission.ActivityRequireLoopContract})
	w.RegisterActivityWithOptions(a.RequireReadiness, activity.RegisterOptions{Name: mission.ActivityRequireReadiness})
	w.RegisterActivityWithOptions(a.ObserveLedger, activity.RegisterOptions{Name: mission.ActivityObserveLedger})
	w.RegisterActivityWithOptions(a.CheckBudget, activity.RegisterOptions{Name: mission.ActivityCheckBudget})
	w.RegisterActivityWithOptions(a.AppendMissionTransition, activity.RegisterOptions{Name: mission.ActivityAppendMissionTransition})
	w.RegisterActivityWithOptions(a.RecordMissionState, activity.RegisterOptions{Name: mission.ActivityRecordMissionState})
	w.RegisterActivityWithOptions(a.RecordGateEvent, activity.RegisterOptions{Name: mission.ActivityRecordGateEvent})
	w.RegisterActivityWithOptions(a.ResolveGateEvent, activity.RegisterOptions{Name: mission.ActivityResolveGateEvent})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// oppValidationReserver adapts the cost ledger to kernel.ValidationReserver so
// a VALIDATE-MORE outcome reserves its bounded Phase-C envelope under the
// mission's experiment budget (docs/PLAN.md Task 102). scopeID is taken from
// the reservation meta's opportunity_id when present.
type oppValidationReserver struct {
	store *cost.Store
}

func (r oppValidationReserver) Reserve(ctx context.Context, amountUSD float64, meta any) (string, error) {
	scopeID := "opportunity"
	if m, ok := meta.(map[string]string); ok {
		if id := m["opportunity_id"]; id != "" {
			scopeID = id
		}
	}
	period := time.Now().UTC().Format("2006-01")
	entry, err := r.store.Reserve(ctx, cost.ScopeMission, scopeID, cost.KindExperiment, period, amountUSD, "opportunity-validation", "v1", meta)
	if err != nil {
		return "", err
	}
	return entry.ID, nil
}
