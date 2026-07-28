#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

echo "DR drill: validating production-shaped compose file"
docker compose -f deploy/docker-compose.prod.yaml config >/dev/null

echo "DR drill: restore workflow is operator-gated; report template at test/drill/rto_rpo_report.md"
