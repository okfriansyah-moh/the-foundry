# Canonical Agent and Skill Packages

The `agents/`, `skills/`, and `domain-skills/` trees are Delivery Foundry's provider-neutral source of truth.
Product repositories declare package names in `.foundry/skills/enabled.yaml`; they do not copy or own the global
catalogs.

Catalog validation is declaration-only. It reads package metadata and enablement, and it grants no executor,
network, secret, SCM, merge, or deployment capability. Runtime-specific copies are a materialization concern for
CAP-02 (Task 154), not this catalog. OpenHands and 9Router remain optional, deferred externals under ADR-001.
