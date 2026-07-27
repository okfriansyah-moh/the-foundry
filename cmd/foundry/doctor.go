package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const doctorTimeout = 10 * time.Second

// runDoctor pings PostgreSQL (SELECT 1) and Temporal (GetSystemInfo) over the
// compose network, printing PASS/FAIL per check. It returns an error if any
// check fails, causing the caller to exit non-zero (Task 4/SKP-02).
func runDoctor(_ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()

	pgDSN := os.Getenv("PG_DSN")
	if pgDSN == "" {
		pgDSN = "postgres://foundry:foundry@postgres:5432/foundry?sslmode=disable"
	}
	temporalHostPort := os.Getenv("TEMPORAL_HOSTPORT")
	if temporalHostPort == "" {
		temporalHostPort = "temporal:7233"
	}

	pgErr := checkPostgres(ctx, pgDSN)
	printResult("postgres", pgErr)

	temporalErr := checkTemporal(ctx, temporalHostPort)
	printResult("temporal", temporalErr)

	if pgErr != nil || temporalErr != nil {
		return fmt.Errorf("doctor: one or more checks failed")
	}
	return nil
}

func checkPostgres(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		return fmt.Errorf("query postgres: %w", err)
	}
	return nil
}

func checkTemporal(ctx context.Context, hostPort string) error {
	conn, err := grpc.NewClient(hostPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial temporal: %w", err)
	}
	defer func() { _ = conn.Close() }()

	client := workflowservice.NewWorkflowServiceClient(conn)
	if _, err := client.GetSystemInfo(ctx, &workflowservice.GetSystemInfoRequest{}); err != nil {
		return fmt.Errorf("temporal GetSystemInfo: %w", err)
	}
	return nil
}

func printResult(check string, err error) {
	if err != nil {
		fmt.Printf("FAIL %s: %v\n", check, err)
		return
	}
	fmt.Printf("PASS %s\n", check)
}
