# Codex official package R2 mirror for api.opencodex.uk

Deployed 2026-10-02 UTC. Public install page: `https://api.opencodex.uk/install/`.
The page's Linux, macOS and Windows command builders use
`https://api.opencodex.uk/codex-cache/`. The legacy NewAPI API routes remain
on port 3001. The selected Cloudflare LB edge proxies to sgp001, whose
`api.opencodex.uk` Nginx vhost forwards `/codex-cache/` to the Worker. The
Worker reads a dedicated private R2 bucket in the `opencodex.uk` Cloudflare
account. No R2 credentials or management token are on sgp001 or the edges.

| Component | Value |
| --- | --- |
| Cloudflare account | `9706f2292fc27ab30618368e4527e73e` |
| Zone | `opencodex.uk` / `437cad4e4c9fb10599bdae2b450a93f5` |
| Worker | `opencodex-codex-release-cache` |
| R2 bucket | `opencodex-codex-release-cache` (APAC) |
| Schedule | `0 * * * *` UTC |
| Worker source | `deploy/codex-r2-release-cache/` |
| sgp001 route | `/etc/nginx/sites-available/opencodex.uk`, exact `/codex-cache/` location |
| NewAPI production | `newapi-3001-codex-r2-mirror-20261002`, local image `newapi-local:codex-r2-mirror-20261002`, port `127.0.0.1:50121` |
| NewAPI front proxy | `/etc/nginx/sites-available/newapi-3001-front-proxy.conf` -> `50121` |
| sgp001 backup | `/home/grey/backups/codex-r2-mirror-3001-20261002T194820Z` |
| sgp001 Nginx backup | `/etc/nginx/sites-available/opencodex.uk.bak-codex-cache-20261002T193143Z` |

The scheduled Worker checks `https://releases.openai.com/codex/channels/latest`
with a unique query each hour. It copies six official standalone package
archives (Linux, macOS and Windows, x64 and arm64) plus
`codex-package_SHA256SUMS` into versioned R2 keys. R2 checks each archive
against the official release metadata SHA-256 as it streams. Only after all
objects succeed does the Worker publish `channels/latest` and the versioned
`release.json`. If upstream is unavailable, the last complete version remains
served. The Worker allows only these keys and the installer script.

The Unix command downloads `install.sh`, a pinned copy of the official
installer with only its release URL changed to this mirror and external
fallback disabled. It uses the official archive digest and checksum manifest
verification. Windows reads the mirrored official release metadata, downloads
its package archive, verifies SHA-256, and extracts `bin/codex.exe` into the
user's standalone package directory. This replaces the npm download portion
on `api.opencodex.uk` only; account key, model and configuration generation
remain in NewAPI. The installer script is published separately using
`publish-installer.sh` when the official script changes.

Initial release `0.160.0` synchronized successfully: `rust-v0.160.0` with
all six archives and checksum file in R2. The public mirror returned an exact
32-byte HTTP `206` range and full HTTP `200` GET. An isolated Linux `HOME`
installed and ran `codex-cli 0.160.0` through the public URL. NewAPI's command
generator check and web build passed. Direct sgp001, forced sgp003 edge and
public install/API routes passed. Windows and macOS command generation passed;
native Windows/macOS execution was not available on this Linux operator host.

For rollback, restore the saved `newapi-3001-front-proxy.conf` to return the
existing production container on port `50101`, run `nginx -t` and reload.
Restore the saved `opencodex.uk` vhost to remove `/codex-cache/` if needed.
The old container and existing database are retained. Do not replace the
database with the backup; the backup contains Nginx and container inspection
state only. The R2 bucket and Worker can remain while disconnected.

The 2026-10-02 20:00 UTC scheduled event completed successfully with `release sync 0.160.0 false`, confirming the hourly check and unchanged-version path.
