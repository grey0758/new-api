# Codex installer credential changes

Linux and Windows install commands now load the private installer credentials
on every launcher invocation. Updating a User environment variable or writing
an env file alone does not refresh an already running terminal's environment.

Linux installs a per-user launcher in `~/.local/bin/codex`, resolving the native
release binary before replacing its symlink. The launcher sources the private
`$CODEX_HOME/env` and forwards arguments and the native exit status. The pasted
command also refreshes its parent shell's environment and command cache.
Conflicting `API_KEY` and `OPENAI_API_KEY` variables are cleared. Existing Linux
login sessions and `auth.json` are preserved.

Windows uses the selected source's native release package, verifies its SHA256
and version, and avoids the old npm executable search. Its PowerShell and cmd
launchers read the private `opencodex-env.json`. Setup updates both Process and
User environment, prepends the launcher directory to PATH, updates the current
PowerShell alias and performs file-backed Codex login. It supports Windows
PowerShell 5 and protects the configuration and credential files with user-only
ACLs. It never kills existing Codex processes or changes CurrentUser execution
policy. Both R2 and official downloads use the same credential setup.

Paste the updated command into the terminal used to start Codex. Other terminals
that already resolve an older npm installation must be reopened. A running
Codex process retains its own session; setup does not terminate it.

Verification: `bun run test:install-command` exercises all generated sources
and platforms, including repeated Unix installs with conflicting inherited keys,
installation failure, quoting and config preservation. Real-host acceptance
artifacts live in the operations workspace under
`artifacts/install-key-switch-20261023`; they contain status summaries only.
