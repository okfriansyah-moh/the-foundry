---
name: sonarqube-quality-gate
description: Interpret quality-gate results without fabricating unavailable evidence.
---

# SonarQube Quality Gate

Record the invoked project, revision, gate result, changed-code findings, and coverage. If the service is not
configured or callable, label the gate unverified; never convert absence into success.
