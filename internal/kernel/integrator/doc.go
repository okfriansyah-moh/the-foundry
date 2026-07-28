// Package integrator is the Branch Integrator: the only component that writes
// to shared 10x branches (Constitution C4, Task 58 / TX-05).
//
// The integrator serializes pushes to each branch via a per-branch FIFO queue
// backed by a PostgreSQL advisory lock. Its protocol per queue item:
//
//  1. Acquire branch lease (fencing token from LeaseStore).
//  2. Fetch remote head (scm/read).
//  3. Verify expectedBase matches remote head (drift check — Task 59).
//  4. Fast-forward-only apply of atomic group commits onto branch.
//  5. CAS push via scm/write with fencing token.
//  6. Record receipt {branch, beforeSHA, afterSHA, groupID, manifestDigest}
//     to external operations ledger.
//  7. Release lease.
//
// Force-push is impossible: no code path in this package calls any push API
// with force=true. This is enforced by a negative test (grep + API-shape).
//
// Authority: internal/kernel — no other package may perform this work
// (Constitution C4). This package does not expose any API that could be
// called from outside internal/kernel.
package integrator
