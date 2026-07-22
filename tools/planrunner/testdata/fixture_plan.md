# Fixture PLAN (scratch copy for tools/planrunner tests)

## D. Master Task Index

| ✔   | Task | Alias    | Title                    | Phase/Wave | Depends | [P]  |
| --- | ---- | -------- | ------------------------ | ---------- | ------- | ---- |
| ✅  | 1    | FIX-BOOT | Bootstrap fixture task   | M0/S0      | —       | None |
| ☐   | 2    | FIX-LOW  | Low risk fixture task    | M0/S0      | 1       | None |
| ☐   | 3    | FIX-HIGH | High risk fixture task   | M0/S0      | 1       | None |
| ☐   | 4    | FIX-FAIL | Always-fails fixture     | M0/S0      | 1       | None |
| ☐   | 5    | FIX-BLKD | Blocked fixture task     | M0/S0      | 99      | None |

### Task 1 (FIX-BOOT) — Bootstrap fixture task

- **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Boundary:** none
- **Outputs:** `fixture/boot.txt`
- **Validation:** `true`
- **Status:** ✅ 2026-01-01

### Task 2 (FIX-LOW) — Low risk fixture task

- **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Boundary:** none
- **Outputs:** `fixture/low.txt`
- **Validation:** `true`
- **Status:** ☐ Not started

### Task 3 (FIX-HIGH) — High risk fixture task

- **Risk:** High · **Exec:** go-kernel · **Rev:** R3 · **Boundary:** none
- **Outputs:** `fixture/high.txt`
- **Validation:** `true`
- **Status:** ☐ Not started

### Task 4 (FIX-FAIL) — Always-fails fixture

- **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Boundary:** none
- **Outputs:** `fixture/fail.txt`
- **Validation:** `true`
- **Status:** ☐ Not started

### Task 5 (FIX-BLKD) — Blocked fixture task

- **Risk:** Low · **Exec:** infra · **Rev:** R1 · **Boundary:** none
- **Outputs:** `fixture/blocked.txt`
- **Validation:** `true`
- **Status:** ☐ Not started
