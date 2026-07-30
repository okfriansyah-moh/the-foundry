// Command foundry is the Foundry CLI entrypoint. It is bootstrap tooling for
// operating the Foundry control plane; it holds no side-effect authority itself.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/okfriansyah-moh/the-foundry/internal/observe"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: foundry <doctor|keygen|login|plan|projection|status|principal|profile|policy|migrate|evidence|cost|budget|audit|mission|product|promotions>")
		os.Exit(1)
	}

	// OTel trace wiring (docs/PLAN.md Task 31): opt-in via
	// observe.EnvTracingEnabled; a no-op shutdown when unset, so every CLI
	// invocation pays this cost unconditionally with zero runtime overhead
	// when tracing is off.
	ctx := context.Background()
	shutdownTracing, err := observe.SetupTracing(ctx, "foundry")
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("foundry: setup tracing: %w", err))
		os.Exit(1)
	}
	defer func() { _ = shutdownTracing(ctx) }()

	switch os.Args[1] {
	case "doctor":
		if err := runDoctor(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "keygen":
		if err := runKeygen(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "login":
		if err := runLogin(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "plan":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry plan <submit|approve|verify|revoke|run> ...")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "submit":
			err = runPlanSubmit(os.Args[3:])
		case "approve":
			err = runPlanApprove(os.Args[3:])
		case "verify":
			err = runPlanVerify(os.Args[3:])
		case "revoke":
			err = runPlanRevoke(os.Args[3:])
		case "run":
			err = runPlanRun(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown plan subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "projection":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry projection <rebuild|rollout>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "rebuild":
			err = runProjectionRebuild(os.Args[3:])
		case "rollout":
			err = runProjectionRollout(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown projection subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "status":
		if err := runStatus(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "principal":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry principal <create>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "create":
			err = runPrincipalCreate(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown principal subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "profile":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry profile <create|show|list>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "create":
			err = runProfileCreate(os.Args[3:])
		case "show":
			err = runProfileShow(os.Args[3:])
		case "list":
			err = runProfileList(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown profile subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "policy":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry policy <resolve>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "resolve":
			err = runPolicyResolve(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown policy subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "migrate":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry migrate <up|down|status>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "up":
			err = runMigrateUp(os.Args[3:])
		case "down":
			err = runMigrateDown(os.Args[3:])
		case "status":
			err = runMigrateStatus(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown migrate subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "evidence":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry evidence <verify|show> <id>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "verify":
			err = runEvidenceVerify(os.Args[3:])
		case "show":
			err = runEvidenceShow(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown evidence subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "cost":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry cost <show>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "show":
			err = runCostShow(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown cost subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "budget":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry budget <raise>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "raise":
			err = runBudgetRaise(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown budget subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "audit":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry audit <verify>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "verify":
			err = runAuditVerify(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown audit subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "mission":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry mission <create|show|start|resume|list|status|pause|kill|ceremony>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "create":
			err = runMissionCreate(os.Args[3:])
		case "show":
			err = runMissionShow(os.Args[3:])
		case "start":
			err = runMissionStart(os.Args[3:])
		case "resume":
			err = runMissionResume(os.Args[3:])
		case "list":
			err = runMissionList(os.Args[3:])
		case "status":
			err = runMissionStatus(os.Args[3:])
		case "pause":
			err = runMissionPause(os.Args[3:])
		case "kill":
			err = runMissionKill(os.Args[3:])
		case "ceremony":
			err = runMissionCeremony(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown mission subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "product":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry product <new>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "new":
			err = runProductNew(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown product subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "promotions":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry promotions <unfreeze>")
			os.Exit(1)
		}
		if err := runPromotions(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "opportunity":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry opportunity <list|show|report>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "list":
			err = runOpportunityList(os.Args[3:])
		case "show":
			err = runOpportunityShow(os.Args[3:])
		case "report":
			err = runOpportunityReport(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown opportunity subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
