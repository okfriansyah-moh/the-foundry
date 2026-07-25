// Command foundry is the Foundry CLI entrypoint. It is bootstrap tooling for
// operating the Foundry control plane; it holds no side-effect authority itself.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: foundry <command>")
		os.Exit(1)
	}

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
	case "plan":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: foundry plan <submit|approve|verify> ...")
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
			fmt.Fprintln(os.Stderr, "usage: foundry projection <rebuild>")
			os.Exit(1)
		}
		var err error
		switch os.Args[2] {
		case "rebuild":
			err = runProjectionRebuild(os.Args[3:])
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
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
