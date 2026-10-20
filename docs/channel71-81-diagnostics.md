# Scoped Codex compatibility and 24-hour diagnostics

Production scope is explicitly configured per process. Empty configuration is
fully disabled. Example for sgp001 NewAPI3002/api.open-codex.com:

```dotenv
CHANNEL_TRACE_SITE=api.open-codex.com
CHANNEL_TRACE_CHANNELS=71,81
CHANNEL_TRACE_DIR=/data/channel-trace
```

2026-10-20 readback: 71=protest-pro-0.15 (manually disabled),
81=krill-ai.com-0.15_3001 (enabled). Configuration does not activate a channel.
Do not copy these IDs to another site's database: .uk's similarly named Krill
channel is47. Requests require the exact HTTP Host, POST, an allowed channel,
and /v1/responses, /v1/responses/compact or /v1/chat/completions.
Other endpoints, WebSocket upgrades and unselected channels are not captured.

The recorder joins the original request, channel selection and attempt ordinal,
the post-conversion upstream request, upstream status/headers/response, fixed
error code and final client response under a generated trace ID and the NewAPI
request ID. X-Channel-Trace-Id is returned to the caller. Existing bridge trace
and provider request IDs are recorded for joining the previously deployed
bridge/relay/official-client diagnostics. Transport timing includes DNS,
connection, first byte, headers and body consumption. Capturing reads the same
network stream; it performs no upstream request and never replays customer tools.
A fallback to an unmonitored channel omits that final response body.

Archives contain sanitized JSON/SSE, tool catalogs, call IDs, arguments and
results. Authentication/cookies, recognized secrets, URL query values, inline
binary, opaque reasoning and incremental deltas are omitted. Completed items
retain sanitized text. Invalid or partial JSON has no raw-text fallback.
This is application-level HTTP capture, not a raw encrypted-network pcap.
Customer bodies stay only in the owning server's mode0700 directory/mode0600
hourly gzipJSONL files. Never commit or export them to Curator/shared artifacts.

Retention is at most24hours, checked everyminute even while idle. Hour-bucket
expiry may evict up to one hour earlier; capacity/disk limits can evict earlier.
Each body captures at most16MiB; seen/captured sizes, captured SHA256, completion,
truncation and memory-budget limitation are explicit. The process has a128MiB
capture budget and64 queued records. Archive budget is2GiB with up to one
segment overshoot; rotation at16MiB and2GiB disk reserve. Blue/green processes
have independent random archive/stats identifiers. Dropped/failed records are
counted in private minute stats and produce a generic journal warning. A crash
can lose queued/inflight records; absence of a record is not success evidence.

Install an independent host minute timer invoking `scripts/channel_trace.py
--directory <actual data mount>/channel-trace maintain` so expiry also continues
while NewAPI is stopped. The same script supports `check` and metadata-only
`query --request-id ID` or `query --trace-id ID`. Only a deliberate trusted-host
`--payload` read reveals sanitized customer bodies. Query scan/result limits and
unreadable segments are explicit. Check stats freshness/loss and file modes
before claiming a complete chain.

## Compatibility evidence

Compared official NewAPI206e0fa5c600127a314a97f88592c067f784f4a6
(relaykit/dto/openai_request.go and openai_responses_compaction_request.go)
with official Codexc6e4438a5d5befe6c8e59b01b6534eb8f0792230
(codex-rs/codex-api/src/common.rs). Preserve reasoning.context/mode,
access_programs, prompt_cache_options and compact tools/parallel_tool_calls/
reasoning/service_tier/cache/text through typed conversion. Existing field
filtering and parameter overrides continue to apply. NewAPI's large plugin/
relaykit migration is not imported into this production fork.

Official reference: https://developers.openai.com/api/docs/guides/reasoning
and https://developers.openai.com/api/docs/guides/function-calling . Retained
reasoning and paired call_id history must remain available for continuation.
The new tests exercise real synthetic HTTP/SSE, secret redaction, allowlists,
retention/capacity and modern tool/reasoning/compaction round trips.

Explicit policy and terminal tool-state errors are never resubmitted through
another channel/selection cycle, even if a retryable HTTP status was mapped.
They retain their cause. Do not remove history, replay completed outputs,
reset sessions, or evade provider moderation. These are confirmed protocol and
diagnostic improvements; no claim is made that they explain every historical
outer_tool_turn_terminated error or that a provider policy refusal is repaired.
