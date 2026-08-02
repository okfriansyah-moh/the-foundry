# V1 release proof notes (Task 151)

`make v1-proof` fails closed when required credentials are absent.

Protected proofs A–F require real PostgreSQL, Temporal, object store, OIDC+WebAuthn
test environment, Telegram Bot API, billed provider, rootless OCI, Fly.io personal
deploy target, Stripe test mode, disposable Bitbucket, and isolated personal/
organization deployments.

Local `V1_PROOF_ALLOW_SKIP=1` yields exit code 2 SKIPPED and must never be archived
as PASS evidence.
