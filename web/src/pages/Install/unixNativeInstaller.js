// Keep this dependency-free module identical in the NewAPI and OpenCodex builders.
const shellQuote = (value) =>
  "'" + String(value).replaceAll("'", "'\\''") + "'";

const mergeMacosConfig = String.raw`from pathlib import Path
import re, sys
old, candidate, project = map(Path, sys.argv[1:])
if not old.exists():
    sys.exit(0)
kept = []
section = None
skip = False
project_section = 'projects.' + '"' + str(project).replace('\\', '\\\\').replace('"', '\\"') + '"'
for line in old.read_text().splitlines():
    match = re.match(r'^\s*\[([^]]+)\]\s*$', line)
    if match:
        section = match.group(1)
        skip = section in ('model_providers.codex', 'model_providers.manual', 'notice.model_migrations', project_section)
    if skip or (section is None and re.match(r'^\s*(model_provider|model|model_reasoning_effort|approval_policy|sandbox_mode|web_search|disable_response_storage)\s*=', line)):
        continue
    kept.append(line)
content = candidate.read_text()
marker = '[model_providers.manual]'
assert content.count(marker) == 1
candidate.write_text(content.replace(marker, '\n'.join(kept).strip() + '\n\n' + marker, 1))`;

export function buildUnixNativeInstallCommand({
  os = 'linux',
  apiKey,
  baseUrl,
  model,
  reasoningEffort,
  config,
  packageSource = 'r2',
}) {
  const isMacos = os === 'macos';
  const installer =
    packageSource === 'official'
      ? 'https://releases.openai.com/codex/install.sh'
      : 'https://api.opencodex.uk/codex-cache/install.sh';
  const toml = [
    `model_provider = "${isMacos ? 'manual' : 'codex'}"`,
    `model = ${JSON.stringify(model)}`,
    `model_reasoning_effort = ${JSON.stringify(reasoningEffort)}`,
    `approval_policy = ${JSON.stringify(config.approvalPolicy)}`,
    `sandbox_mode = ${JSON.stringify(config.sandboxMode)}`,
    'web_search = "live"',
    '',
    `[model_providers.${isMacos ? 'manual' : 'codex'}]`,
    `name = "${isMacos ? 'Manual' : 'codex'}"`,
    `base_url = ${JSON.stringify(baseUrl)}`,
    'wire_api = "responses"',
    'env_key = "CODEX_API_KEY"',
    `supports_websockets = ${config.supportsWebsockets}`,
    '',
    '[notice.model_migrations]',
    `"gpt-5.3-codex" = ${JSON.stringify(model)}`,
  ].join('\n');
  const env = [
    'unset API_KEY OPENAI_API_KEY',
    `export CODEX_API_KEY=${shellQuote(apiKey.trim() || 'YOUR_KEY')}`,
    `export BASE_URL=${shellQuote(baseUrl)}`,
    `export MODEL_NAME=${shellQuote(model)}`,
    'export PATH="$HOME/.local/bin:$PATH"',
  ].join('\n');
  const script = [
    'set -eu',
    'umask 077',
    'for dep in curl tar mktemp awk sed cp mv chmod readlink; do command -v "$dep" >/dev/null 2>&1 || { printf "缺少依赖: %s\\n" "$dep" >&2; exit 1; }; done',
    'CODEX_HOME_PATH="${CODEX_HOME:-$HOME/.codex}"',
    'export CODEX_HOME="$CODEX_HOME_PATH"',
    `WORK_DIR="$HOME/${config.workspaceName}"`,
    'CODEX_BIN="$HOME/.local/bin/codex"',
    'mkdir -p "$CODEX_HOME_PATH" "$WORK_DIR"',
    'INSTALL_SCRIPT="$(mktemp)"',
    'trap \'rm -f "$INSTALL_SCRIPT"\' EXIT',
    `curl -fsSL --connect-timeout 10 --max-time 60 ${shellQuote(installer)} -o "$INSTALL_SCRIPT"`,
    'CODEX_NON_INTERACTIVE=true CODEX_HOME="$CODEX_HOME_PATH" CODEX_INSTALL_DIR="$HOME/.local/bin" sh "$INSTALL_SCRIPT"',
    '[ -x "$CODEX_BIN" ]',
    '"$CODEX_BIN" --version',
    'BACKUP_DIR="$(mktemp -d "$CODEX_HOME_PATH/install-backup.XXXXXX")"',
    'for name in config.toml env; do if [ -f "$CODEX_HOME_PATH/$name" ]; then cp -p "$CODEX_HOME_PATH/$name" "$BACKUP_DIR/$name"; fi; done',
    // Escape runtime paths as TOML basic strings, including macOS paths with spaces.
    String.raw`PROJECT_PATH="$WORK_DIR"`,
    String.raw`PROJECT_PATH="${'$'}{PROJECT_PATH//\\/\\\\}"`,
    String.raw`PROJECT_PATH="${'$'}{PROJECT_PATH//\"/\\\"}"`,
    String.raw`PROJECT_PATH="${'$'}{PROJECT_PATH//$'\n'/\\n}"`,
    String.raw`PROJECT_PATH="${'$'}{PROJECT_PATH//$'\r'/\\r}"`,
    String.raw`PROJECT_PATH="${'$'}{PROJECT_PATH//$'\t'/\\t}"`,
    `printf '%s\\n' ${toml.split('\n').map(shellQuote).join(' ')} > "$CODEX_HOME_PATH/config.toml.new"`,
    String.raw`printf '\n[projects."%s"]\ntrust_level = "trusted"\n' "$PROJECT_PATH" >> "$CODEX_HOME_PATH/config.toml.new"`,
    ...(isMacos
      ? [
          `python3 -c ${shellQuote(mergeMacosConfig)} "$CODEX_HOME_PATH/config.toml" "$CODEX_HOME_PATH/config.toml.new" "$WORK_DIR"`,
        ]
      : []),
    'chmod 600 "$CODEX_HOME_PATH/config.toml.new"',
    'mv "$CODEX_HOME_PATH/config.toml.new" "$CODEX_HOME_PATH/config.toml"',
    `printf '%s\\n' ${env.split('\n').map(shellQuote).join(' ')} > "$CODEX_HOME_PATH/env.new"`,
    // A POSIX shell can source this file; never write API credentials to auth.json.
    String.raw`quote_sh() { printf "'"; printf '%s' "$1" | sed "s/'/'\\\\''/g"; printf "'"; }`,
    String.raw`{ printf 'export CODEX_HOME='; quote_sh "$CODEX_HOME_PATH"; printf '\n'; } >> "$CODEX_HOME_PATH/env.new"`,
    'chmod 600 "$CODEX_HOME_PATH/env.new"',
    'mv "$CODEX_HOME_PATH/env.new" "$CODEX_HOME_PATH/env"',
    ...(!isMacos
      ? [
          // Reload the private env on every launch, including from old terminals.
          'NATIVE_BIN="$(readlink -f "$CODEX_BIN")"',
          'if [ "$NATIVE_BIN" = "$CODEX_BIN" ]; then mkdir -p "$CODEX_HOME_PATH/bin"; cp -p "$CODEX_BIN" "$CODEX_HOME_PATH/bin/codex-native"; NATIVE_BIN="$CODEX_HOME_PATH/bin/codex-native"; fi',
          String.raw`{ printf '#!/bin/sh\nset -eu\nif [ -z "${'$'}{CODEX_HOME:-}" ]; then CODEX_HOME='; quote_sh "$CODEX_HOME_PATH"; printf '; fi\nexport CODEX_HOME\n[ ! -r "$CODEX_HOME/env" ] || . "$CODEX_HOME/env"\nexec '; quote_sh "$NATIVE_BIN"; printf ' "$@"\n'; } > "$CODEX_BIN.new"`,
          'chmod 700 "$CODEX_BIN.new"',
          'mv -f "$CODEX_BIN.new" "$CODEX_BIN"',
        ]
      : []),
    String.raw`for rc in "$HOME/.profile" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.zshenv" "$HOME/.zshrc" "$HOME/.zprofile"; do if [ "$rc" = "$HOME/.bash_profile" ] && [ ! -f "$rc" ]; then continue; fi; if [ -f "$rc" ]; then cp -p "$rc" "$BACKUP_DIR/$(basename "$rc")"; else : > "$rc"; fi; tmp="$(mktemp)"; awk -v env_path="$CODEX_HOME_PATH/env" '!/^# OpenCodex environment$/ && !/^# End OpenCodex environment$/ && !( /^[[:space:]]*\[ -f / && (index($0, env_path) || index($0, ".codex/env")) )' "$rc" > "$tmp"; { printf '\n# OpenCodex environment\n[ -f '; quote_sh "$CODEX_HOME_PATH/env"; printf ' ] && . '; quote_sh "$CODEX_HOME_PATH/env"; printf '\n# End OpenCodex environment\n'; } >> "$tmp"; cat "$tmp" > "$rc"; rm -f "$tmp"; done`,
    '. "$CODEX_HOME_PATH/env"',
    ...(isMacos
      ? [
          'printf "%s\\n" "$CODEX_API_KEY" | "$CODEX_BIN" login --with-api-key >/dev/null',
          'chmod 600 "$CODEX_HOME_PATH/auth.json"',
          'if command -v launchctl >/dev/null 2>&1; then launchctl setenv CODEX_API_KEY "$CODEX_API_KEY"; fi',
          '"$CODEX_BIN" app-server daemon restart >/dev/null',
        ]
      : []),
    'printf "配置已写入；原配置备份: %s\\n" "$BACKUP_DIR"',
    'cd "$WORK_DIR"',
    'if [ "${OPENCODEX_INSTALL_NO_LAUNCH:-0}" != 1 ]; then "$CODEX_BIN"' +
      (config.launchBypassFlag
        ? ' --dangerously-bypass-approvals-and-sandbox'
        : '') +
      '; fi',
  ].join('; ');
  const command = `bash -c ${shellQuote(script)}`;
  return isMacos
    ? command
    : `${command} && . "${'$'}{CODEX_HOME:-$HOME/.codex}/env" && { hash -r 2>/dev/null || true; }`;
}
