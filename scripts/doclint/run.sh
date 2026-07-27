#!/usr/bin/env bash
# docs/PLAN.md Task 37 (FND-18): documentation-governance lints (V12's
# documentation CI gates, docs/foundry/docs/governance/documentation-rules.md
# "V12 lint gates (additions)"), callable standalone (`make doclint`) or as
# part of the full suite (`make fitness` calls this exact script — see
# scripts/fitness.sh's step (d)) — one implementation, never two.
#
# Covers: doc-link resolution + anchor checking, duplicate Mermaid diagram
# D-ID detection, the single-source contract heuristic, the container-
# inventory lint, and composed-file reproducibility (which absorbs and
# retires Task 2's standalone scripts/check-ai-harness.sh).
#
# The superseded-term scan (stray superseded status label, see
# cmd/fitlint's termAllowlist) and the plain existing-file link check
# already run repo-wide via cmd/fitlint's `term` and `doclinks` commands in
# scripts/fitness.sh's steps (b)/(d); `doclinks` is invoked here too, over
# the same doc roots, since anchor checking is new behavior added to that
# same command (docs/PLAN.md Task 37).
set -euo pipefail

cd "$(dirname "$0")/../.."

fitlint_bin="$(mktemp -d)/fitlint"
go build -o "${fitlint_bin}" ./cmd/fitlint

echo "== doclint: doc-link resolver (incl. anchor checking) =="
"${fitlint_bin}" doclinks . docs/foundry

echo "== doclint: duplicate Mermaid diagram D-ID detector =="
"${fitlint_bin}" mermaidid docs

echo "== doclint: single-source contract heuristic =="
"${fitlint_bin}" contract docs

echo "== doclint: container-inventory lint =="
"${fitlint_bin}" containers .

echo "== doclint: composed-file reproducibility (absorbs check-ai-harness.sh) =="
bash scripts/doclint/ai-harness-repro.sh

echo "doclint OK"
