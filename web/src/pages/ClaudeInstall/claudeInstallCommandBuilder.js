import { Monitor, Terminal } from 'lucide-react';

export const CLAUDE_DEFAULT_MODEL = 'claude-opus-4-7';

export const CLAUDE_MODELS = [CLAUDE_DEFAULT_MODEL, 'claude-opus-4-8'];

export const CLAUDE_PLATFORMS = [
  { id: 'linux', label: 'Linux', icon: Terminal },
  { id: 'windows', label: 'Windows', icon: Monitor },
];

function trimTrailingSlash(value) {
  return String(value || '').trim().replace(/\/+$/, '');
}

export function normalizeClaudeBaseUrl(value) {
  const fallback = 'https://apicc.opencodex.uk';
  const raw = trimTrailingSlash(value || fallback);

  try {
    const url = new URL(raw);
    url.pathname = url.pathname.replace(/\/+$/, '').replace(/\/v1$/i, '');
    if (url.pathname === '/') {
      url.pathname = '';
    }
    url.search = '';
    url.hash = '';
    return url.toString().replace(/\/+$/, '');
  } catch {
    return raw.replace(/\/v1$/i, '') || fallback;
  }
}

export function defaultClaudeBaseUrlFromStatus() {
  try {
    const status = JSON.parse(localStorage.getItem('status') || '{}');
    const serverAddress =
      status.server_address || status.data?.server_address || window.location.origin;
    return normalizeClaudeBaseUrl(serverAddress);
  } catch {
    return normalizeClaudeBaseUrl(window.location.origin);
  }
}

function bashQuote(value) {
  return String(value || '')
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\$/g, '\\$')
    .replace(/`/g, '\\`');
}

function powershellQuote(value) {
  return `'${String(value || '').replace(/'/g, "''")}'`;
}

function buildLinuxCommand(apiKey, baseUrl, model) {
  const key = bashQuote(apiKey || 'YOUR_ANTHROPIC_KEY');
  const url = bashQuote(normalizeClaudeBaseUrl(baseUrl));
  const modelName = bashQuote(model || CLAUDE_DEFAULT_MODEL);

  return [
    `ANTHROPIC_API_KEY="${key}"`,
    `ANTHROPIC_BASE_URL="${url}"`,
    `ANTHROPIC_MODEL="${modelName}"`,
    `ANTHROPIC_CUSTOM_MODEL_OPTION="$ANTHROPIC_MODEL"`,
    `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY="1"`,
    `CLAUDE_CODE_SIMPLE="1"`,
    `export ANTHROPIC_API_KEY ANTHROPIC_BASE_URL ANTHROPIC_MODEL ANTHROPIC_CUSTOM_MODEL_OPTION CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY CLAUDE_CODE_SIMPLE`,
    `unset ANTHROPIC_AUTH_TOKEN CLAUDE_MODEL`,
    `refresh_path() { export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:/usr/local/bin:/usr/bin:/bin:$PATH"; hash -r 2>/dev/null || true; }`,
    `refresh_path`,
    `(command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1 && echo "Node.js/npm 已安装: $(node --version) / $(npm --version)" || (curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt-get install -y nodejs npm))`,
    `refresh_path`,
    `if npm ping --registry https://registry.npmjs.org >/dev/null 2>&1; then NPM_REGISTRY="https://registry.npmjs.org"; else NPM_REGISTRY="https://registry.npmmirror.com"; fi`,
    `NPM_PREFIX="$HOME/.local"`,
    `mkdir -p "$NPM_PREFIX/bin" "$HOME/.claude"`,
    `npm config set prefix "$NPM_PREFIX" >/dev/null 2>&1 || true`,
    `export PATH="$NPM_PREFIX/bin:$PATH"`,
    `if command -v claude >/dev/null 2>&1; then echo "Claude Code 已安装，检查并升级: $(claude --version)"; else echo "Claude Code 未安装，开始安装"; fi`,
    `npm install -g --registry "$NPM_REGISTRY" @anthropic-ai/claude-code@latest || (npm config set registry https://registry.npmmirror.com && npm install -g @anthropic-ai/claude-code@latest)`,
    `refresh_path`,
    `ENV_FILE="$HOME/.claude/env"`,
    `printf 'export ANTHROPIC_API_KEY=%q\\nexport ANTHROPIC_BASE_URL=%q\\nexport ANTHROPIC_MODEL=%q\\nexport ANTHROPIC_CUSTOM_MODEL_OPTION=%q\\nexport CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=%q\\nexport CLAUDE_CODE_SIMPLE=%q\\nunset ANTHROPIC_AUTH_TOKEN\\nunset CLAUDE_MODEL\\n' "$ANTHROPIC_API_KEY" "$ANTHROPIC_BASE_URL" "$ANTHROPIC_MODEL" "$ANTHROPIC_CUSTOM_MODEL_OPTION" "$CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY" "$CLAUDE_CODE_SIMPLE" > "$ENV_FILE"`,
    `chmod 600 "$ENV_FILE"`,
    `for SHELL_RC in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile" "$HOME/.zshrc" "$HOME/.zprofile"; do touch "$SHELL_RC" 2>/dev/null || true; if [ -f "$SHELL_RC" ]; then sed -i '/# OpenCodex Claude environment/,+1d;/\\.claude\\/env/d;/^export ANTHROPIC_API_KEY=/d;/^export CLAUDE_MODEL=/d;/^export ANTHROPIC_AUTH_TOKEN=/d;/^export ANTHROPIC_BASE_URL=/d;/^export ANTHROPIC_MODEL=/d;/^export ANTHROPIC_CUSTOM_MODEL_OPTION=/d;/^export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=/d;/^export CLAUDE_CODE_SIMPLE=/d' "$SHELL_RC" 2>/dev/null || true; printf '\\n# OpenCodex Claude environment\\n[ -f "$HOME/.claude/env" ] && . "$HOME/.claude/env"\\n' >> "$SHELL_RC"; fi; done`,
    `node -e 'const fs=require("fs");const os=require("os");const home=os.homedir();const settings={$schema:"https://json.schemastore.org/claude-code-settings.json",model:process.env.ANTHROPIC_MODEL,permissions:{allow:["Bash","Read","Edit","Write","WebFetch","mcp__*"],deny:[],defaultMode:"bypassPermissions",skipDangerousModePermissionPrompt:true},env:{ANTHROPIC_API_KEY:process.env.ANTHROPIC_API_KEY,ANTHROPIC_BASE_URL:process.env.ANTHROPIC_BASE_URL,ANTHROPIC_MODEL:process.env.ANTHROPIC_MODEL,ANTHROPIC_CUSTOM_MODEL_OPTION:process.env.ANTHROPIC_CUSTOM_MODEL_OPTION,CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY:process.env.CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY,CLAUDE_CODE_SIMPLE:process.env.CLAUDE_CODE_SIMPLE}};fs.writeFileSync(home+"/.claude/settings.json",JSON.stringify(settings,null,2)+"\\n");fs.writeFileSync(home+"/.claude.json",JSON.stringify({hasCompletedOnboarding:true},null,2)+"\\n");'`,
    `echo "==============================="`,
    `echo "Node.js : $(node --version 2>/dev/null || echo '未检测到')"`,
    `echo "npm     : $(npm --version 2>/dev/null || echo '未检测到')"`,
    `echo "Claude  : $(claude --version 2>/dev/null || echo '未检测到')"`,
    `echo "API_KEY : \${ANTHROPIC_API_KEY:0:15}..."`,
    `echo "BASE_URL: $ANTHROPIC_BASE_URL"`,
    `echo "MODEL   : $ANTHROPIC_MODEL"`,
    `echo "环境变量: $ENV_FILE"`,
    `echo "权限模式: bypassPermissions"`,
    `echo "精简模式: CLAUDE_CODE_SIMPLE=1"`,
    `echo "==============================="`,
    `claude --model "$ANTHROPIC_MODEL" --permission-mode bypassPermissions`,
  ].join(' && ');
}

function buildWindowsCommand(apiKey, baseUrl, model) {
  const key = powershellQuote(apiKey || 'YOUR_ANTHROPIC_KEY');
  const url = powershellQuote(normalizeClaudeBaseUrl(baseUrl));
  const modelName = powershellQuote(model || CLAUDE_DEFAULT_MODEL);

  return [
    `try { Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned -Force -ErrorAction Stop } catch { Write-Host ('[!] 无法永久设置 PowerShell 执行策略: ' + $_) -ForegroundColor Yellow }`,
    `Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force`,
    `$ErrorActionPreference = 'Continue'`,
    `$apiKey = ${key}`,
    `$baseUrl = ${url}`,
    `$model = ${modelName}`,
    `function Refresh-Path { $env:Path = [System.Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [System.Environment]::GetEnvironmentVariable('Path','User') }`,
    `Refresh-Path`,
    `if ((Get-Command node -ErrorAction SilentlyContinue) -and (Get-Command npm -ErrorAction SilentlyContinue)) { Write-Host ('Node.js/npm 已安装: ' + (node --version) + ' / ' + (npm --version)) -ForegroundColor Green } else { Write-Host '未检测到 Node.js，尝试安装 LTS...' -ForegroundColor Yellow; if (Get-Command winget -ErrorAction SilentlyContinue) { winget install OpenJS.NodeJS.LTS --accept-source-agreements --accept-package-agreements --silent }; Refresh-Path; if (-not ((Get-Command node -ErrorAction SilentlyContinue) -and (Get-Command npm -ErrorAction SilentlyContinue))) { Write-Host '请先安装 Node.js/npm: https://nodejs.org/en/download' -ForegroundColor Red; exit 1 } }`,
    `if (Get-Command claude -ErrorAction SilentlyContinue) { Write-Host ('Claude Code 已安装，检查并升级: ' + (claude --version)) -ForegroundColor Green } else { Write-Host 'Claude Code 未安装，开始安装' -ForegroundColor Yellow }`,
    `npm install -g @anthropic-ai/claude-code@latest`,
    `if ($LASTEXITCODE -ne 0) { npm config set registry https://registry.npmmirror.com; npm install -g @anthropic-ai/claude-code@latest }`,
    `Refresh-Path`,
    `[System.Environment]::SetEnvironmentVariable('ANTHROPIC_API_KEY',$apiKey,'User')`,
    `[System.Environment]::SetEnvironmentVariable('ANTHROPIC_AUTH_TOKEN',$null,'User')`,
    `[System.Environment]::SetEnvironmentVariable('CLAUDE_MODEL',$null,'User')`,
    `[System.Environment]::SetEnvironmentVariable('ANTHROPIC_BASE_URL',$baseUrl,'User')`,
    `[System.Environment]::SetEnvironmentVariable('ANTHROPIC_MODEL',$model,'User')`,
    `[System.Environment]::SetEnvironmentVariable('ANTHROPIC_CUSTOM_MODEL_OPTION',$model,'User')`,
    `[System.Environment]::SetEnvironmentVariable('CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY','1','User')`,
    `[System.Environment]::SetEnvironmentVariable('CLAUDE_CODE_USE_POWERSHELL_TOOL','1','User')`,
    `[System.Environment]::SetEnvironmentVariable('CLAUDE_CODE_SIMPLE','1','User')`,
    `$env:ANTHROPIC_API_KEY=$apiKey`,
    `$env:ANTHROPIC_AUTH_TOKEN=$null`,
    `$env:CLAUDE_MODEL=$null`,
    `$env:ANTHROPIC_BASE_URL=$baseUrl`,
    `$env:ANTHROPIC_MODEL=$model`,
    `$env:ANTHROPIC_CUSTOM_MODEL_OPTION=$model`,
    `$env:CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY='1'`,
    `$env:CLAUDE_CODE_USE_POWERSHELL_TOOL='1'`,
    `$env:CLAUDE_CODE_SIMPLE='1'`,
    `$claudeDir = Join-Path $HOME '.claude'`,
    `New-Item -ItemType Directory -Force -Path $claudeDir | Out-Null`,
    `$settings = @{ '$schema' = 'https://json.schemastore.org/claude-code-settings.json'; model = $model; permissions = @{ allow = @('Bash','Read','Edit','Write','WebFetch','mcp__*'); deny = @(); defaultMode = 'bypassPermissions'; skipDangerousModePermissionPrompt = $true }; env = @{ ANTHROPIC_API_KEY = $apiKey; ANTHROPIC_BASE_URL = $baseUrl; ANTHROPIC_MODEL = $model; ANTHROPIC_CUSTOM_MODEL_OPTION = $model; CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY = '1'; CLAUDE_CODE_USE_POWERSHELL_TOOL = '1'; CLAUDE_CODE_SIMPLE = '1' } }`,
    `$utf8NoBom = New-Object System.Text.UTF8Encoding $false`,
    `[System.IO.File]::WriteAllText((Join-Path $claudeDir 'settings.json'), ($settings | ConvertTo-Json -Depth 10), $utf8NoBom)`,
    `if (Test-Path (Join-Path $HOME '.claude.json')) { $claudeJson = Get-Content (Join-Path $HOME '.claude.json') -Raw | ConvertFrom-Json } else { $claudeJson = @{} | ConvertTo-Json | ConvertFrom-Json }`,
    `$claudeJson | Add-Member -NotePropertyName hasCompletedOnboarding -NotePropertyValue $true -Force`,
    `[System.IO.File]::WriteAllText((Join-Path $HOME '.claude.json'), ($claudeJson | ConvertTo-Json -Depth 20), $utf8NoBom)`,
    `Write-Host '==============================='`,
    `Write-Host ('Node.js : ' + (node --version))`,
    `Write-Host ('npm     : ' + (npm --version))`,
    `Write-Host ('Claude  : ' + (claude --version))`,
    `Write-Host ('API_KEY : ' + $apiKey.Substring(0, [Math]::Min(15, $apiKey.Length)) + '...')`,
    `Write-Host ('BASE_URL: ' + $baseUrl)`,
    `Write-Host ('MODEL   : ' + $model)`,
    `Write-Host ('配置文件: ' + (Join-Path $claudeDir 'settings.json'))`,
    `Write-Host '权限模式: bypassPermissions'`,
    `Write-Host '精简模式: CLAUDE_CODE_SIMPLE=1'`,
    `Write-Host '==============================='`,
    `claude --model $model --permission-mode bypassPermissions`,
  ].join('; ');
}

export function buildClaudeInstallCommand(platform, apiKey, baseUrl, model) {
  if (platform === 'windows') {
    return buildWindowsCommand(apiKey.trim(), baseUrl, model.trim());
  }

  return buildLinuxCommand(apiKey.trim(), baseUrl, model.trim());
}
