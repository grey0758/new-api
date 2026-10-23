# Native Responses routing

Native Codex requests use the currently authorized token/user group, model,
channel status and ordinary channel selection. Session IDs, turn IDs and tool
history do not lock a request to a historical NewAPI channel or group.

The native route registry, original-channel cooldown rejection, history/turn
admission constraints, route-based retry suppression and bridge cache-key epochs
have been removed. The retired native-route environment variables have no effect;
NewAPI no longer connects to that dedicated Redis dependency. Existing archived
routing digests may expire under their prior retention policy; deleting or
restoring them is unnecessary.

Changing groups does not grant access to a channel outside the current group.
An unavailable channel is handled by ordinary authorized selection and cooldown
rules. Client prompt_cache_key, turn metadata and tool history remain intact.
Bridge account authentication, tool RPC validation, duplicate detection and
terminal errors remain independent. A terminal tool error is never retried merely
because historical-channel locking was removed.

Private request/error tracing retains its existing48-hour rotation.
