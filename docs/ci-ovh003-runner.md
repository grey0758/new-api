# Production build host

Production tags use the repository-scoped `ovh003-newapi` runner on ovh003.
The reusable Go/Web/Docker test matrix and PR CI continue on GitHub-hosted
Linux runners. A manual production-tag dispatch can select `github` to use
the existing cloud build path.

The self-hosted account has no sudo, production SSH keys, or host Docker socket
access. Buildx connects to a dedicated rootless BuildKit Unix socket. Runner
and builder share a host limit of 2 CPU equivalents and 6 GiB RAM. Each runner
instance processes one job at a time. Local BuildKit cache persists between
builds; the existing GitHub Actions cache remains enabled.

The production workflow still requires an exact release tag reachable from
main, the complete test matrix, Trivy, keyless Cosign signatures, SLSA v1
provenance, and SPDX SBOM attestations. It publishes an immutable amd64 digest;
the existing operations release orchestrator handles target-specific blue-green
deployment separately. Production credentials remain on the operations host.

External contributors require maintainer approval for fork workflows. PR
workflows must continue using GitHub-hosted runners; the self-hosted label is
reserved for trusted production builds.

Before ovh003 FlowAPI DR promotion, the pre-start hook pauses admission and
stops managed runners plus the isolated builder. Admission stays paused until
an operator verifies the host role and explicitly resumes it. Host installation,
runtime verification, and DR controls are maintained in
`/home/grey/work/ops-delivery-automation/docs/github-runner.md`.
