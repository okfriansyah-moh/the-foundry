---
id: seed-self-dep
version: "1.0"
tasks:
  - id: t1
    goal: depends on itself
    validation_commands:
      - echo ok
    depends_on:
      - t1
---
## Rationale
Deliberately-failing topology fixture: self-dependency (docs/PLAN.md Task 110).
