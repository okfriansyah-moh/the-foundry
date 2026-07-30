---
id: seed-cycle
version: "1.0"
tasks:
  - id: t1
    goal: a
    validation_commands:
      - echo ok
    depends_on:
      - t2
  - id: t2
    goal: b
    validation_commands:
      - echo ok
    depends_on:
      - t1
---
## Rationale
Deliberately-failing topology fixture: dependency cycle (docs/PLAN.md Task 110).
