# Unix Codex installation

Linux and macOS use the official native standalone installer. R2 and official
package sources preserve the selected API URL, model, reasoning and permissions.
Node.js, npm, Homebrew and administrator access are unnecessary for this flow.
The upstream installer verifies archive checksums and chooses the host platform.

The generated command verifies the installed executable before replacing config.
It backs up config.toml, env and touched shell startup files under a private
install-backup directory, writes configuration with mode 0600, and preserves
auth.json. Credentials are supplied through CODEX_API_KEY in the private env file.
Repeated execution retains one source line per startup file and does not create
a missing .bash_profile which could shadow .profile. CODEX_HOME is supported.

Run `cd web && bun run test:install-command` for generated-command checks and
real shell/config roundtrips with synthetic credentials and an offline installer.
The roundtrips exercise both platforms/sources, special characters in paths and
keys, installation failure, repeated installs, backup and login preservation.
These checks do not replace native macOS execution. Network acceptance must
use an isolated HOME/CODEX_HOME, temporary restricted credentials, and the
selected public API URL; include shell tools, result submission and resume.
