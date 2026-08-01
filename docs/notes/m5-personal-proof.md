# m5-personal proof notes (Task 132)

Hermetic harness rewritten under `test/e2e/venture/run.sh` with
`observe.HumanTouchCounter` (no shell-literal HUMAN_TOUCHES=0 claim).

Live control-plane proof (`RUN_VENTURE_LIVE=1`) requires provisioned
Postgres, Temporal, API-billed executor, Fly personal app, Stripe test mode,
and a dedicated Telegram test bot — not available in this adjudication
environment. See `docs/notes/v1-evidence-gate.md`.
