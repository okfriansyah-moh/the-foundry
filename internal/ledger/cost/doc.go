// Package cost implements the cost ledger (docs/PLAN.md Task 29/FND-10,
// Constitution C19, docs/foundry/docs/operations/cost-accounting.md):
// budget envelopes (internal/db/migrations/00009_budgets.sql's budgets
// table) and the individual reservation/incur/reconcile lifecycle recorded
// as cost_entries rows (00006_ledgers.sql, extended by 00009 with the
// 'released' and 'shadow' states).
//
// Reserve is the one correctness-critical operation: it must never let
// concurrent reservations oversubscribe an envelope's ceiling, even under
// real concurrent load against a real Postgres. It achieves this with a
// single `UPDATE budgets SET reserved_usd = reserved_usd + $amount WHERE
// ceiling_usd - (reserved_usd + incurred_usd) >= $amount` statement:
// Postgres takes a row-level lock for the duration of the enclosing
// transaction, so a second concurrent Reserve against the same envelope
// blocks until the first commits (or rolls back) and then re-evaluates the
// WHERE clause against the now-current row — there is no window in which
// two callers can both read a stale "amount available" and both decide to
// proceed, because neither ever performs a separate read followed by a
// separate write. See store_test.go's TestReserve_ConcurrentNeverOversubscribes
// for the property test proving this against a real Postgres under -race.
package cost
