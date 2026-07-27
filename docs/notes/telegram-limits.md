# Telegram Bot API rate limits — verified snapshot

Date: 2026-07-25

Verification method: live `WebFetch` of the official Telegram documentation from this environment (network egress
to `core.telegram.org` succeeded), per this task's governing doc
(`docs/foundry/docs/operations/telegram.md`, header: "Verify current Telegram Bot API limits at implementation
time") and mirroring the staleness-verification discipline of `docs/notes/claude-code-flags.md` (Task 17).

## What was fetched and quoted verbatim

Source: `https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this` (Telegram Bots FAQ).

| Limit | Quoted text |
| --- | --- |
| Per-chat | "In a single chat, avoid sending more than one message per second." |
| Group broadcast | "In a group, bots are not be able to send more than 20 messages per minute." |
| Global/bulk broadcast | "For bulk notifications, bots are not able to broadcast more than about 30 messages per second, unless they enable paid broadcasts." |

This matches the rough figures this task's brief was seeded with (~1 msg/s/chat, ~30 msg/s global) — confirmed
against the live source rather than assumed.

## What was NOT independently reconfirmed this session

- The 4096-character text-message limit: a second `WebFetch` against `core.telegram.org/bots/api#sendmessage` did
  not return the specific character-limit sentence (the fetched excerpt was truncated/summarized by the fetch
  tool, not the raw page). This number is not re-derived from first principles here — it is carried over from
  `docs/foundry/docs/operations/telegram.md` §19.15, which already states it as 4096 hard / 3500 target. Treat the
  4096 figure as **not independently re-verified live in this session**, same caveat class as Task 17's unverified
  JSON schema fields.
- `retry_after` response shape (`{"ok":false,"error_code":429,"parameters":{"retry_after":N}}`) — carried over
  from the governing doc's §19.17, not re-fetched from a live 429 response (would require real flood-control
  against a live bot).

## Internal ceilings this task implements (`internal/notify/bucket.go`)

The governing doc (§19.10) already specifies internal ceilings with a safety margin below the raw limits above;
those margins remain valid against the just-reconfirmed raw numbers, so this task uses them unchanged rather than
inventing new numbers:

| Bucket | Raw verified limit | Internal ceiling used | Margin |
| --- | --- | --- | --- |
| Private chat | 1 msg/s | 0.80 msg/s (burst 1) | 20% |
| Group/supergroup | 20 msg/min | 15 msg/min (burst 1) | 25% |
| Global | ~30 msg/s | 25 msg/s (burst 25) | ~17% |

Implementation: `internal/notify.RateLimiter` wraps `golang.org/x/time/rate.Limiter` (already an indirect module
dependency via the existing dependency graph — promoted to direct in `go.mod` for this task) for both the global
bucket and one per-chat bucket, keyed by chat id and chat type, per
`docs/foundry/docs/operations/telegram.md` §19.16's hierarchical global→chat-type→chat ordering. A send is
permitted only when both buckets have a token available at check time (`RateLimiter.Allow`).
