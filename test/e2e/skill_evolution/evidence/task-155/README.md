# Task 155 evidence

This archive is produced by the hermetic skill-evolution end-to-end test.

- `promotion-rows.jsonl` shows the personal promotion and append-only rollback records; `org-proposal-rows.jsonl` records a proposal without activation.
- `profile-isolation-proof.txt` proves the same catalog selects personal v2 while the organization profile remains on baseline v1.
- `version-diff.txt` and `rollback-proof.txt` contain bounded byte counts and digests proving the promoted change, restored bytes, and retained v1/v2/v3 history without copying executable prompt text into evidence.
- `gate-proof.txt` records the permission-expansion and cumulative-drift freeze negative paths.
- `authority-before.sha256` and `authority-after.sha256` cover config, policy, SCM, and deploy trees and must match byte-for-byte.

No credentials, network calls, canonical repository writes, SCM operations, or deploy operations are used.
