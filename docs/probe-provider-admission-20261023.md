# Account admission failures during active channel probes

ny001 live synthetic regression found an unrelated continuous channel71 probe
receiving HTTP503 `relay_pool_unavailable` at 01:51:54 UTC. The existing probe
discarded the structured cause, recorded only `probe status 503`, and armed a
300-second channel-wide cooldown. Customer-facing protest requests then failed
before dispatch despite a paused tool continuation succeeding on the bound
account. A scheduled recovery probe passed at 01:53:57 UTC.

Probe HTTP errors now retain only an allowlisted code and HTTP status, without
provider message/body. Known 429/5xx relay admission/transport/turn failures
record `probe_skipped` with reason `provider_local_admission`, status and code.
They release the probe flag without promoting the failure into a channel-wide
cooldown or clearing an existing genuine cooldown. Authentication failures,
ordinary 503 and unknown/unstructured failures keep the prior fail-closed path.

The real HTTP test exercises the complete probe-to-scheduler path, including
recovery with a pre-existing cooldown and no private provider message in errors.
It fails on the old implementation. Complete Go suite, focused race and
govulncheck pass on Go1.26.10. Production publication and acceptance evidence
are maintained separately in the parent ops workspace.
