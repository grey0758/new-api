# Native Responses sessions across group changes

This guard is opt-in and defaults off. It applies only to authenticated native
POST `/v1/responses` requests with an explicit turn identity and a session ID
matching `prompt_cache_key`. Other API requests retain existing routing.

## Routing contract

- A server-observed completed turn, or the bridge's explicit terminal failure,
  may change channel/group only on a different, complete user turn with paired
  tools and an exact unchanged accepted-history prefix.
- An active, paused or unknown tool turn keeps its physical channel. The
  distributor independently rechecks channel status, model and current group
  authorization. No permission to an old group is restored by session affinity.
- If the new group authorizes that same physical channel, continuation and
  billing use the current group. If it does not, return400
  `native_session_route_changed` before dispatch with an actionable explanation.
  A temporarily cooling original channel instead returns503 without dispatch.
- A safe route/group switch creates an internal bridge-only cache-key epoch.
  Switching back creates another epoch. Client metadata, call IDs, history,
  tool inputs/results and ordinary provider requests are not rewritten.
- A claimed business request never falls through to another NewAPI retry or
  selection cycle. Exact overlapping requests retain their owner; only its
  lease may finish the registry record. Other requests wait at most60s for an
  inflight boundary, then fail before dispatch. The bridge remains responsible
  for exact duplicate coalescing and tool/RPC validation.
- After process loss, a15-minute expired lease becomes **unknown**, never
  completed. Only the original authorized physical route may consult its relay.
- A new turn admitted on the same physical channel from paused/unknown state
  does not retire the old business owner until its relay proves acceptance.
- Only the exact tool-free native Responses compaction envelope bypasses the
  business registry. It cannot move, retire or replace a business RPC, and may
  reuse a retired business turn ID. A compaction label with tools is not exempt.

## Configuration and storage

Production3001 uses these three values through the inspect-backed release
inventory's private env override, not a reduced container command:

```text
CODEX_NATIVE_ROUTE_GUARD_ENABLED=true
CODEX_NATIVE_ROUTE_GUARD_REQUIRE_REDIS=true
CODEX_NATIVE_ROUTE_GUARD_NAMESPACE=opencodex-uk-3001
```

The namespace must uniquely identify the user ledger. Session ownership is
scoped by namespace and authenticated user, independently of token, model and
group changes. Production requires shared Redis; unavailable/corrupt registry
state fails503 before dispatch, with no process-local fallback.

Records contain routing metadata, history/turn digests and an opaque epoch;
they contain no prompt, tool payload, OAuth or API credential. Atomic Redis
CAS is shared across blue/green processes, with at most8192 sessions and128
retired turns per session. A live owner is never silently evicted. Capacity
failures fail closed; retired-turn capacity returns429 and requests a fresh
session with completed work summarized.

Digest-only routing records expire31 days after update, outliving the bridge's
30-day original-key affinity. A shorter lifetime could resurrect the old
account binding after a group switch. This is independent of private HTTP /
JSON-RPC packet retention, which remains48 hours with existing bounded rotation.

`native_route` admin metadata correlates selected/previous group/channel,
safe-boundary and switch decisions with session/turn digests. Pre-relay
rejections emit `native_route_rejected` with request ID and fixed code. They
do not contain customer content. Ingress-only errors have no relay hop because
no relay request occurred.

## Limits and release checks

The registry starts observing at activation. It does not invent an old route
or completion proof for history predating activation, and does not recover an
old missing/disabled live RPC. Preserve those sessions and correlate their
exact request IDs; never repair them by replaying tools or resetting state.
Unknown transport failures remain unknown even after HTTP disconnects.

Regression coverage includes real Gin distribution with SQLite-backed
abilities, current-group enforcement, switch-back epochs, tool-bearing fake
compaction, large JSON integers, token/user/model isolation, concurrency,
cancellation, retired turns, storage capacity and isolated actual Redis CAS/TTL.
The old distributor fails both independent authorization/epoch regressions.
Local complete Go, focused race, CI/Web/Docker/security, signed immutable
amd64 image, SLSA/SPDX and scoped3001 blue/green acceptance are required before
production activation. Passing tests do not by themselves mean deployed.
