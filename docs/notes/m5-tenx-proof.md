# m5-tenx proof notes (Task 133)

Fail-closed harness under `test/e2e/tenx/run.sh`: absent Temporal/Postgres
fails (never exit 0). Provider selection is asserted via Task 140's
`SelectSCMProvider` (config-resolved, not hardcoded).

Live disposable Bitbucket push requires `RUN_TENX_LIVE=1`,
`FOUNDRY_TENX_DISPOSABLE=1`, and Bitbucket credentials. Not executed in this
environment. See `docs/notes/v1-evidence-gate.md`.
