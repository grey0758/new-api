# Generic Responses message IDs in stateless history

A completed assistant message returned by one provider can have a generic
`item_` identifier. Sending that self-contained history to Krill's Codex endpoint
fails validation because it expects message identifiers beginning with `msg`.
Adding a new user message preserves the historical ID, so retrying cannot repair
the request. This is a protocol compatibility failure, not lost session history.

For the exact Krill `/codex` Responses endpoint, explicitly stateless requests
(`store:false`, no conversation or previous response) omit only a unique,
unreferenced `item_` ID on a completed assistant message containing complete
text/refusal content. Message content, phase, annotations, encrypted reasoning,
tool arguments, `call_id` and completed results remain unchanged. No stored
history or caller transcript is rewritten, and no extra request is dispatched.
Other providers and ambiguous, referenced, partial or malformed items retain
the existing behavior. The earlier custom-tool compatibility remains in place.

The [official Responses input schema](https://developers.openai.com/api/reference/resources/responses/methods/create)
accepts assistant message inputs without persisted message IDs. Direct synthetic
Krill comparisons confirm that generic message IDs fail while omitted IDs and
standard message IDs both complete with identical expected text and no tools.
Regression tests cover byte preservation, DTO and passthrough requests,
stateless scope, linked/reference/duplicate exclusions, and completed tool
history. Unsupported history errors stay terminal and request-scoped; the
recovery text no longer directs users to a historical channel.
