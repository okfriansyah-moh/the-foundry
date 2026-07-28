# Track B exit report

- Prohibition proof: `bash scripts/check_tenx_prohibition.sh .`
- Runtime allowed-call proof: `go test ./test/e2e/tenx/... -run Allowed`
- SCM adapter proof: `go test ./internal/scm/... -run Contract`
- Live dry-run: operator-gated; this harness skips cleanly when Temporal/PostgreSQL are unavailable.
