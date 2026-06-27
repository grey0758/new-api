import { Laptop, Monitor, Terminal } from 'lucide-react';

export const PLATFORMS = [
  { id: 'linux', label: 'Linux', icon: Terminal },
  { id: 'macos', label: 'macOS', icon: Laptop },
  { id: 'windows', label: 'Windows', icon: Monitor },
];

export const INSTALL_MODELS = ['gpt-5.5', 'gpt-5.4'];

function trimTrailingSlash(value) {
  return String(value || '').trim().replace(/\/+$/, '');
}

export function normalizeCodexBaseUrl(value) {
  const fallback = 'https://api.opencodex.uk/v1';
  const raw = trimTrailingSlash(value || fallback);

  try {
    const url = new URL(raw);
    url.pathname = url.pathname.replace(/\/+$/, '');
    if (!url.pathname || url.pathname === '/') {
      url.pathname = '/v1';
    } else if (!/\/v1$/i.test(url.pathname)) {
      url.pathname = `${url.pathname}/v1`;
    }
    url.search = '';
    url.hash = '';
    return url.toString().replace(/\/+$/, '');
  } catch {
    return raw || fallback;
  }
}

export function defaultBaseUrlFromStatus() {
  try {
    const status = JSON.parse(localStorage.getItem('status') || '{}');
    const serverAddress =
      status.server_address || status.data?.server_address || window.location.origin;
    return normalizeCodexBaseUrl(serverAddress);
  } catch {
    return normalizeCodexBaseUrl(window.location.origin);
  }
}

function shellQuote(value) {
  return String(value || '').replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

function powershellQuote(value) {
  return `'${String(value || '').replace(/'/g, "''")}'`;
}

function buildUnixCommand(platform, apiKey, baseUrl, model) {
  const installer =
    platform === 'macos'
      ? `if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then if command -v brew >/dev/null 2>&1; then brew install node; else echo "请先安装 Node.js LTS: https://nodejs.org"; exit 1; fi; fi`
      : `if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then if command -v apt-get >/dev/null 2>&1; then curl -fsSL https://deb.nodesource.com/setup_24.x | sudo -E bash - && sudo apt-get install -y nodejs; elif command -v dnf >/dev/null 2>&1; then curl -fsSL https://rpm.nodesource.com/setup_24.x | sudo -E bash - && sudo dnf install -y nodejs; else echo "请先安装 Node.js LTS: https://nodejs.org"; exit 1; fi; fi`;

  return [
    `API_KEY="${shellQuote(apiKey || 'YOUR_KEY')}"`,
    `BASE_URL="${shellQuote(normalizeCodexBaseUrl(baseUrl))}"`,
    `MODEL="${shellQuote(model || INSTALL_MODELS[0])}"`,
    installer,
    `NPM_PREFIX="$HOME/.local"`,
    `mkdir -p "$NPM_PREFIX/bin" "$HOME/.codex"`,
    `npm config set prefix "$NPM_PREFIX" >/dev/null 2>&1 || true`,
    `export PATH="$NPM_PREFIX/bin:$PATH"`,
    `npm install -g @openai/codex@latest --include=optional`,
    `cat > "$HOME/.codex/config.toml" <<EOF
model_provider = "codex"
model = "$MODEL"
model_reasoning_effort = "high"
disable_response_storage = true
approval_policy = "never"
sandbox_mode = "danger-full-access"
web_search = "live"

[model_providers.codex]
name = "codex"
base_url = "$BASE_URL"
wire_api = "responses"
api_key = "$API_KEY"
env_key = "CODEX_API_KEY"
supports_websockets = false

[notice.model_migrations]
"gpt-5.3-codex" = "$MODEL"
EOF`,
    `printf '{"OPENAI_API_KEY":"%s"}\\n' "$API_KEY" > "$HOME/.codex/auth.json"`,
    `printf 'export CODEX_API_KEY=%q\\nexport OPENAI_API_KEY=%q\\n' "$API_KEY" "$API_KEY" > "$HOME/.codex/env"`,
    `chmod 600 "$HOME/.codex/auth.json" "$HOME/.codex/env"`,
    `codex --version`,
    `codex`,
  ].join(' && ');
}

function buildWindowsCommand(apiKey, baseUrl, model) {
  const key = powershellQuote(apiKey || 'YOUR_KEY');
  const url = powershellQuote(normalizeCodexBaseUrl(baseUrl));
  const modelName = powershellQuote(model || INSTALL_MODELS[0]);

  return [
    `$apiKey = ${key}`,
    `$baseUrl = ${url}`,
    `$model = ${modelName}`,
    `Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force`,
    `if (-not (Get-Command node -ErrorAction SilentlyContinue)) { winget install OpenJS.NodeJS.LTS --accept-source-agreements --accept-package-agreements --silent }`,
    `$env:Path = [System.Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [System.Environment]::GetEnvironmentVariable('Path','User')`,
    `npm install -g @openai/codex@latest --force --include=optional`,
    `$codexDir = Join-Path $HOME '.codex'`,
    `New-Item -ItemType Directory -Force -Path $codexDir | Out-Null`,
    `$toml = @"
model_provider = "codex"
model = "$model"
model_reasoning_effort = "high"
disable_response_storage = true
approval_policy = "on-request"
approvals_reviewer = "auto_review"
sandbox_mode = "workspace-write"
web_search = "live"

[model_providers.codex]
name = "codex"
base_url = "$baseUrl"
wire_api = "responses"
api_key = "$apiKey"
env_key = "CODEX_API_KEY"
supports_websockets = false

[notice.model_migrations]
"gpt-5.3-codex" = "$model"
"@`,
    `[System.IO.File]::WriteAllText((Join-Path $codexDir 'config.toml'), $toml, [System.Text.UTF8Encoding]::new($false))`,
    `$auth = @{ OPENAI_API_KEY = $apiKey } | ConvertTo-Json -Compress`,
    `[System.IO.File]::WriteAllText((Join-Path $codexDir 'auth.json'), $auth, [System.Text.UTF8Encoding]::new($false))`,
    `[System.Environment]::SetEnvironmentVariable('CODEX_API_KEY', $apiKey, 'User')`,
    `[System.Environment]::SetEnvironmentVariable('OPENAI_API_KEY', $apiKey, 'User')`,
    `codex --version`,
    `codex`,
  ].join('; ');
}

export function buildInstallCommand(platform, apiKey, baseUrl, model) {
  if (platform === 'windows') {
    return buildWindowsCommand(apiKey, baseUrl, model);
  }

  return buildUnixCommand(platform, apiKey, baseUrl, model);
}
