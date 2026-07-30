// Package research is the untrusted opportunity-research intake for
// docs/PLAN.md Task 101 (OPP-02; Constitution C23). It lets real research —
// web content and LLM summarization — propose evidence for the Task 100
// opportunity model while guaranteeing three things:
//
//   - nothing fabricates a SourceRef: a claim may be Observed only if its
//     SourceRef resolves to a stored, hash-verified artifact;
//   - no fetched or generated text is ever treated as an instruction: a claim
//     whose text is an imperative addressed to the system is refused outright
//     (Contain), never stored as evidence;
//   - research proposes; it never decides. This package may not call
//     opportunity.Score or opportunity.Decide — that separation of duties is
//     CI-enforced by `fitlint research-boundary`.
//
// Every claim this package produces is marked Untrusted. The Skeptic role may
// only lower confidence (emit reject candidates), never raise it. Research
// runs through the LLM provider's own server-side web_search/web_fetch tools
// (transport decision B11); the deterministic default used by every non-gated
// test and by CI is ReplayResearcher (cassette-backed). LiveResearcher is
// gated behind RUN_OPPORTUNITY_LIVE=1 and is first-party-API-only.
//
// Boundary: no import of internal/kernel. This package proposes and
// summarizes; it never writes a verdict and is never the sole basis for an
// Observed label.
package research
