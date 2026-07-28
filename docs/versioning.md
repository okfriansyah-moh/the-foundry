# Temporal workflow versioning policy

- Every kernel workflow change that can affect replay must be guarded with `workflow.GetVersion`.
- Recorded-history replay tests are mandatory for kernel workflow changes.
- Upgrades must preserve in-flight workflow progress across N-1 to N.
- Deprecations remain readable until every active execution has crossed the patch boundary.
