# Seeded fitness violation

This is a seeded violation for docs/PLAN.md Task 37 / FND-18: the
`fitlint doclinks` anchor-checking extension must flag a `#anchor` fragment
that doesn't match any real heading's slug in the resolved target file.

## A Real Heading

[dead same-file anchor](#this-heading-does-not-exist)

[dead cross-file anchor](./broken.md#also-does-not-exist)
