# Supply-Chain Security

[← Back to Delivery Foundry master index](../../delivery_foundry.md) · [Migration map](../../docs/MIGRATION_MAP_V11_TO_V12.md)

Normative status: **Normative.** Dependency and package changes are never Tier A0 (see `autonomy/admission-tiers.md`): every dependency change carries executable supply-chain risk and requires at minimum the A1 gate — immutable version, lockfile diff, provenance attestation, vulnerability scan, SBOM update, license check, and prepared rollback.

The preserved package, npm, library, and malware defense architecture follows.


---

<!-- Relocated from V11: §13.3 Package, npm, library, malware defense (lines 8238-8399) -->

## 13.3 Package, npm, library, and malware defense

Dependencies and agent capabilities are executable supply-chain inputs.

### 13.3.1 Default-deny installation

Default rules:

```text
no unpinned package versions
no implicit latest tags
no unreviewed git dependencies
no arbitrary tarball URLs
no arbitrary registry
no automatic lifecycle scripts
no curl-pipe-shell
no npx of an unpinned package
no global installation from discovery results
no binary-only capability without approved provenance
```

### 13.3.2 npm-safe intake

For a newly discovered npm package:

```text
npm metadata and tarball fetch without execution
→ verify package name and registry
→ inspect maintainers and publish history
→ verify lockfile resolution
→ inspect tarball file list
→ inspect package.json scripts
→ verify signatures and provenance where available
→ scan vulnerabilities and malware alerts
→ install with scripts disabled in quarantine
→ explicitly approve only required install scripts
→ run in an egress-restricted sandbox
```

Baseline commands for supported npm versions:

```bash
npm ci --ignore-scripts
npm audit signatures
npm install-scripts ls
```

When install scripts are genuinely required:

- inspect the exact pinned package version;
- approve by package and version;
- set strict script-allow policy;
- run scripts only in an isolated sandbox;
- block access to secrets and the host filesystem;
- record stdout, file writes, processes, and network attempts.

Never use a global `dangerously-allow-all-scripts` escape hatch in unattended workflows.

### 13.3.3 Registry and package policy

```yaml
package_policy:
  registries:
    npm:
      allowed:
        - https://registry.npmjs.org
      prefer_internal_mirror: true

  versions:
    require_lockfile: true
    forbid_latest_tag: true
    forbid_wildcards: true
    require_integrity_hash: true

  sources:
    git_dependencies: approval_required
    remote_tarballs: forbidden
    local_file_dependencies: project_root_only

  scripts:
    default: deny
    strict_allowlist: true

  provenance:
    require_when_available: true
    trusted_publishing_for_release: true

  malware:
    block_known_malicious: true
```

### 13.3.4 Vulnerability and malware scanning

Run before staging and after every lockfile change:

```text
ecosystem-native audit
OSV-Scanner on source and lockfiles
Dependabot or provider-equivalent alerts
dependency review in change requests
container image scan
malware advisory check
license-policy check
SBOM generation
```

Scanning alone is insufficient. A zero-known-CVE package may still be malicious.

### 13.3.5 Typosquatting and dependency confusion

Block or escalate when:

- package name differs by a small edit from an existing dependency;
- a public package shadows a private package name;
- package owner recently changed;
- package was very recently published and requests install scripts;
- package has unexpected new maintainers;
- package introduces an unexpected binary;
- source repository and published artifact do not match;
- package version makes a suspicious major jump;
- package downloads code at install or runtime.

Organization profiles should prefer an internal registry proxy with an explicit package allowlist.

### 13.3.6 Provenance and immutable references

Prefer:

- OIDC trusted publishing;
- short-lived publishing credentials;
- provenance attestations;
- registry signatures;
- exact lockfile integrity;
- SBOM;
- container image digests;
- Git commit SHAs.

For GitHub Actions, pin third-party actions to full-length commit SHAs. Tags alone are mutable.

### 13.3.7 Capability-package security

Agent and skill packages are scanned for:

```text
secret-reading instructions
filesystem escape
network exfiltration
policy bypass language
instructions to disable tests
instructions to rewrite guardrails
encoded payloads
unbounded recursive delegation
destructive shell commands
hidden external downloads
telemetry
prompt injection against other agents
memory modification
self-promotion
```

Prompts are code. Treat them like source code with review, versioning, tests, signatures, and rollback.

