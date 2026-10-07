# Channel47 image reference compatibility candidate

This is tested source on branch `fix/channel47-image-compat-20261007`, based
on main `6596e4ffc`. It is **not deployed** and is not proof that the original
request is repaired.

## Incident and reproduction boundary

On sgp001 NewAPI3002 (`api.open-codex.com`), request
`202610070705503058974008268d9d64Iy5NjNw` failed at
2026-10-07 07:05:57 UTC. Lifecycle rows4138239/4138240 and the matching Docker
logs establish channel47, model `gpt-5.6-terra`, endpoint `/v1/responses`:

1. Upstream `unsupported_parameter`: `Input image references require asset
   resolution that is not supported by rustponsesapi.` The channel's existing
   status mapping presented this as503.
2. `svip` had no eligible retry channel and returned500 `get_channel_failed`.

The stored log has no request body or image representation. Four such errors
occurred on channel47 in the preceding24 hours, across Terra, Astra and Sol.
No original user request or tool was replayed.

Direct synthetic probes through this exact channel's current upstream key
found public image URLs and inline PNGs working in both nonstreaming and
streaming requests. Structured function-call output containing an inline PNG
also completed. All five successful responses were200. This does not reproduce
the original error or prove that every backend accepts references.

Synthetic file IDs failed with invalid URL or not-found errors. An opaque
`sediment://` resource pointer failed400 with an explicit requirement for a
normalized inline Base64 image data URL. An image with no source failed400.
These are separate validation failures, not reproductions of the incident.

## Candidate behavior

Only channel ID47, the exact Krill `/codex` provider identity, and the ordinary
Responses relay mode are eligible. Immediately before the first upstream send,
downloadable HTTP(S) `input_image.image_url` references become inline data
URLs. Both normal DTO conversion and body passthrough enter the hook. Chat
Completions explicitly routed through Responses enter it too.

The gateway's existing file-fetch policy, download-size limit and request
cache apply. PNG, JPEG, WebP and GIF bytes are Base64-encoded without resizing
or recompressing. Image detail, message order, call IDs, tool output, unknown
fields and large integers remain intact. Repeated URLs share the request
cache. Fetch failures return400 without upstream send/retry and without
printing signed image URLs in their errors.

Existing inline images, file IDs, opaque resource pointers, compact requests,
other channels and other providers retain their previous behavior. There is
no local filesystem read, cross-account credential lookup, synthetic image,
image deletion or automatic upstream replay.

Official input formats were checked against
https://developers.openai.com/api/docs/guides/images-vision .

## Verification and release boundary

The complete `go test ./...` suite passed using Go1.26.6 and an isolated
build cache. Six focused tests cover exact provider/channel scoping, image
bytes, gateway fetch/cache integration, private-address rejection, signed-URL
error redaction, structured tool output, byte-preserved unrelated fields,
idempotence and non-image input. The standard installed `go` launcher first
failed with toolchain/cache access errors; invoking the matching cached
toolchain directly with a task-specific GOCACHE succeeded.

No production configuration/database/container or public artifact was changed.
The current NewAPI3002 release remains `.10.04.3`.

Before treating this as the incident fix, obtain the original image source
shape (field names, URI scheme, attachment versus tool-output location), or a
sanitized reproduction of that shape. No image, password, API key, signed URL
or business transcript is required. If the original already used inline
Base64, this conversion cannot fix the upstream failure. A file ID or opaque
asset without retrievable bytes needs a separately validated resolution path.

Any deployment must use the existing signed immutable-image and scoped
blue/green release gates; do not bypass them with a locally rebuilt container.
