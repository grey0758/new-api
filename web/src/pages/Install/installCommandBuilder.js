import { Laptop, Monitor, Terminal } from 'lucide-react';

export const PLATFORMS = [
  { id: 'linux', label: 'Linux', icon: Terminal },
  { id: 'macos', label: 'macOS', icon: Laptop },
  { id: 'windows', label: 'Windows', icon: Monitor },
];

export const INSTALL_MODELS = ['gpt-5.5', 'gpt-5.6-sol'];

export const INSTALL_REASONING_EFFORTS = [
  { id: 'xhigh', label: '超高' },
  { id: 'max', label: '最高' },
];

export const DEFAULT_INSTALL_REASONING_EFFORT = 'xhigh';

export const DEFAULT_INSTALL_COMMAND_CONFIG = {
  models: INSTALL_MODELS,
  defaultModel: 'gpt-5.6-sol',
  reasoningEfforts: INSTALL_REASONING_EFFORTS.map((item) => item.id),
  defaultReasoningEffort: DEFAULT_INSTALL_REASONING_EFFORT,
  approvalPolicy: 'never',
  sandboxMode: 'danger-full-access',
  supportsWebsockets: false,
  workspaceName: 'opencodex-workspace',
  windowsProjectPathStyle: 'forward-slash',
  launchBypassFlag: false,
};

const LINUX_CURRENT_COMMAND_TEMPLATE = [
  `API_KEY="__API_KEY__"`,
  `MODEL="__MODEL__"`,
  `BASE_URL="__BASE_URL__"`,
  `NODE_MIN_MAJOR=20`,
  `NODE_LTS_MAJOR=24`,
  `test_url() { curl -fsSI --connect-timeout 5 "$1" >/dev/null 2>&1; }`,
  `refresh_path() { export PATH="$HOME/.local/bin:$HOME/.fnm/aliases/default/bin:$HOME/.nvm/versions/node/$(ls "$HOME/.nvm/versions/node/" 2>/dev/null | sort -V | tail -1)/bin:/snap/bin:/usr/local/bin:/usr/bin:/bin:$PATH" 2>/dev/null; hash -r 2>/dev/null || true; }`,
  `has_node() { refresh_path; command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1 || return 1; NODE_MAJOR="$(node --version 2>/dev/null | sed 's/^v//' | cut -d. -f1)"; case "$NODE_MAJOR" in ""|*[!0-9]*) return 1;; esac; [ "$NODE_MAJOR" -ge "$NODE_MIN_MAJOR" ]; }`,
  `install_node() { echo "========== 安装 Node.js/npm =========="; if command -v apt-get >/dev/null 2>&1 && test_url https://deb.nodesource.com; then curl -fsSL "https://deb.nodesource.com/setup_$NODE_LTS_MAJOR.x" | sudo -E bash - && sudo apt-get install -y nodejs && has_node && return 0; fi; if command -v dnf >/dev/null 2>&1 && test_url https://rpm.nodesource.com; then curl -fsSL "https://rpm.nodesource.com/setup_$NODE_LTS_MAJOR.x" | sudo -E bash - && sudo dnf install -y nodejs && has_node && return 0; fi; if command -v yum >/dev/null 2>&1 && test_url https://rpm.nodesource.com; then curl -fsSL "https://rpm.nodesource.com/setup_$NODE_LTS_MAJOR.x" | sudo -E bash - && sudo yum install -y nodejs && has_node && return 0; fi; if command -v apt-get >/dev/null 2>&1; then sudo apt-get update -y && sudo apt-get install -y nodejs && has_node && return 0; fi; if command -v apk >/dev/null 2>&1; then sudo apk add --no-cache nodejs npm && has_node && return 0; fi; if command -v pacman >/dev/null 2>&1; then sudo pacman -Sy --noconfirm nodejs npm && has_node && return 0; fi; export NVM_DIR="$HOME/.nvm"; if [ ! -s "$NVM_DIR/nvm.sh" ]; then curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash || curl -fsSL https://gitee.com/mirrors/nvm/raw/v0.40.3/install.sh | bash || true; fi; if [ -s "$NVM_DIR/nvm.sh" ]; then unset npm_config_prefix NPM_CONFIG_PREFIX; . "$NVM_DIR/nvm.sh" && nvm install --lts --latest-npm && nvm use --lts --delete-prefix && has_node && return 0; fi; ARCH="$(uname -m)"; case "$ARCH" in x86_64) NODE_ARCH="linux-x64";; aarch64|arm64) NODE_ARCH="linux-arm64";; armv7l) NODE_ARCH="linux-armv7l";; *) NODE_ARCH="";; esac; FULL_VER=""; for api_url in https://nodejs.org/dist/index.json https://registry.npmmirror.com/-/binary/node/index.json; do [ -n "$FULL_VER" ] || FULL_VER="$(curl -fsSL --connect-timeout 10 "$api_url" 2>/dev/null | grep -oP '"version"\\s*:\\s*"\\Kv'$NODE_LTS_MAJOR'\\.[0-9.]+' | head -1 || true)"; done; if [ -n "$NODE_ARCH" ] && [ -n "$FULL_VER" ]; then TAR="node-$FULL_VER-$NODE_ARCH.tar.xz"; for src in "https://nodejs.org/dist/$FULL_VER/$TAR" "https://cdn.npmmirror.com/binaries/node/$FULL_VER/$TAR" "https://registry.npmmirror.com/-/binary/node/$FULL_VER/$TAR"; do curl -fSL --connect-timeout 15 -o "/tmp/$TAR" "$src" && sudo tar -xJf "/tmp/$TAR" -C /usr/local --strip-components=1 && rm -f "/tmp/$TAR" && has_node && return 0; done; fi; return 1; }`,
  `if has_node; then echo "Node.js/npm 版本可用: $(node --version) / $(npm --version)"; else install_node || { echo "====== Node.js/npm 安装失败，请手动安装 Node.js LTS 后重试 ======"; exit 1; }; fi`,
  `install_codex() { echo "========== 安装 Codex =========="; NPM_PREFIX="$HOME/.local"; mkdir -p "$NPM_PREFIX/bin"; npm config set prefix "$NPM_PREFIX" >/dev/null 2>&1 || true; export PATH="$NPM_PREFIX/bin:$PATH"; if npm ping --registry https://registry.npmjs.org --fetch-timeout=8000 >/dev/null 2>&1; then NPM_REGISTRY="https://registry.npmjs.org"; echo "npm 官方源可用"; else NPM_REGISTRY="https://registry.npmmirror.com"; echo "npm 官方源不可用，使用 npmmirror"; fi; export npm_config_fetch_timeout=60000 npm_config_fetch_retries=2; npm install -g @openai/codex@latest --include=optional --registry "$NPM_REGISTRY" || { echo "Codex 安装失败，清理全局残留后重试"; NPM_ROOT="$(npm root -g 2>/dev/null || printf "%s/lib/node_modules" "$NPM_PREFIX")"; rm -rf "$NPM_ROOT/@openai/codex" "$NPM_ROOT/@openai/.codex-"* 2>/dev/null || true; npm cache clean --force >/dev/null 2>&1 || true; npm install -g @openai/codex@latest --force --include=optional --registry "$NPM_REGISTRY"; }; refresh_path; if ! command -v codex >/dev/null 2>&1 || ! codex --version >/dev/null 2>&1; then case "$(uname -s)-$(uname -m)" in Linux-x86_64) CODEX_NATIVE_NAME="@openai/codex-linux-x64";; Linux-aarch64|Linux-arm64) CODEX_NATIVE_NAME="@openai/codex-linux-arm64";; *) CODEX_NATIVE_NAME="";; esac; if [ -n "$CODEX_NATIVE_NAME" ]; then CODEX_NATIVE_SPEC="$(npm view @openai/codex@latest "optionalDependencies.$CODEX_NATIVE_NAME" --registry "$NPM_REGISTRY" --fetch-timeout=60000 2>/dev/null || true)"; [ -n "$CODEX_NATIVE_SPEC" ] && npm install -g @openai/codex@latest "$CODEX_NATIVE_NAME@$CODEX_NATIVE_SPEC" --force --include=optional --registry "$NPM_REGISTRY"; fi; fi; refresh_path; command -v codex >/dev/null 2>&1 && codex --version >/dev/null 2>&1; }`,
  `install_codex || { echo "[×] Codex 安装失败，请检查 npm 输出"; exit 1; }`,
  `CODEX_BIN="$(command -v codex)"`,
  `echo "========== 配置 Codex =========="`,
  `WORK_DIR="$HOME/opencodex-workspace"`,
  `mkdir -p ~/.codex "$WORK_DIR"`,
  `ENV_FILE="$HOME/.codex/env"`,
  `printf 'export API_KEY=%q\nexport BASE_URL=%q\nexport MODEL=%q\nexport MODEL_NAME=%q\nexport CODEX_API_KEY=%q\nexport OPENAI_API_KEY=%q\nexport PATH="$HOME/.local/bin:$HOME/.fnm/aliases/default/bin:$PATH"\n' "$API_KEY" "$BASE_URL" "$MODEL" "$MODEL" "$API_KEY" "$API_KEY" > "$ENV_FILE"`,
  `chmod 600 "$ENV_FILE"`,
  `for SHELL_RC in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile" "$HOME/.zshrc" "$HOME/.zprofile"; do touch "$SHELL_RC" 2>/dev/null || true; if [ -f "$SHELL_RC" ]; then sed -i '/# OpenCodex environment/,+1d;/\\.codex\\/env/d;/^export CODEX_API_KEY=/d;/^export OPENAI_API_KEY=/d;/^export API_KEY=/d;/^export BASE_URL=/d;/^export MODEL=/d;/^export MODEL_NAME=/d;/HOME\\/\\.local\\/bin/d' "$SHELL_RC" 2>/dev/null || true; printf '\\n# OpenCodex environment\\n[ -f "$HOME/.codex/env" ] && . "$HOME/.codex/env"\\n' >> "$SHELL_RC"; fi; done`,
  `. "$ENV_FILE"`,
  `printf '{"model_provider":"codex","model":"%s","model_reasoning_effort":"high","disable_response_storage":true,"approval_policy":"never","sandbox_mode":"danger-full-access","web_search":"live","model_providers":{"codex":{"name":"codex","base_url":"%s","wire_api":"responses","env_key":"CODEX_API_KEY","supports_websockets":false}},"projects":{"'"$WORK_DIR"'":{"trust_level":"trusted"}},"notice":{"model_migrations":{"gpt-5.3-codex":"%s"}}}' "$MODEL" "$BASE_URL" "$MODEL" > ~/.codex/config.json`,
  `printf '{"OPENAI_API_KEY":"%s"}' "$API_KEY" > ~/.codex/auth.json`,
  `printf 'model_provider = "codex"\\nmodel = "%s"\\nmodel_reasoning_effort = "high"\\ndisable_response_storage = true\\napproval_policy = "never"\\nsandbox_mode = "danger-full-access"\\nweb_search = "live"\\n\\n[model_providers.codex]\\nname = "codex"\\nbase_url = "%s"\\nwire_api = "responses"\\nenv_key = "CODEX_API_KEY"\\nsupports_websockets = false\\n\\n[projects."%s"]\\ntrust_level = "trusted"\\n\\n[notice.model_migrations]\\n"gpt-5.3-codex" = "%s"\\n' "$MODEL" "$BASE_URL" "$WORK_DIR" "$MODEL" > ~/.codex/config.toml`,
  `echo "[√] 配置已写入: $HOME/.codex"`,
  `echo "========== 验证 =========="`,
  `echo "Node.js: $(node --version)"`,
  `echo "npm:     $(npm --version)"`,
  `echo "Codex:   $($CODEX_BIN --version)"`,
  `echo "========== 启动 Codex =========="`,
  `cd "$WORK_DIR" && "$CODEX_BIN"`,
].join(' && ');

const LINUX_LEGACY_ENV_CONFIG_COMMAND = String.raw`sed -i '/CODEX_API_KEY/d' ~/.bashrc 2>/dev/null; echo "export CODEX_API_KEY=\"\${API_KEY}\"" >> ~/.bashrc; export CODEX_API_KEY="$API_KEY"; [ -f ~/.zshrc ] && sed -i '/CODEX_API_KEY/d' ~/.zshrc 2>/dev/null && echo "export CODEX_API_KEY=\"\${API_KEY}\"" >> ~/.zshrc; mkdir -p ~/.codex`;

const LINUX_PERSISTENT_ENV_CONFIG_COMMAND = String.raw`mkdir -p ~/.codex; ENV_FILE="$HOME/.codex/env"; printf 'export API_KEY=%q\nexport BASE_URL=%q\nexport MODEL=%q\nexport MODEL_NAME=%q\nexport CODEX_API_KEY=%q\nexport OPENAI_API_KEY=%q\nexport PATH="$HOME/.local/bin:$HOME/.fnm/aliases/default/bin:$PATH"\n' "$API_KEY" "$BASE_URL" "$MODEL" "$MODEL" "$API_KEY" "$API_KEY" > "$ENV_FILE"; chmod 600 "$ENV_FILE"; for SHELL_RC in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile" "$HOME/.zshrc" "$HOME/.zprofile"; do touch "$SHELL_RC" 2>/dev/null || true; if [ -f "$SHELL_RC" ]; then sed -i '/# OpenCodex environment/,+1d;/\.codex\/env/d;/^export CODEX_API_KEY=/d;/^export OPENAI_API_KEY=/d;/^export API_KEY=/d;/^export BASE_URL=/d;/^export MODEL=/d;/^export MODEL_NAME=/d;/HOME\/.local\/bin/d' "$SHELL_RC" 2>/dev/null || true; printf '\n# OpenCodex environment\n[ -f "$HOME/.codex/env" ] && . "$HOME/.codex/env"\n' >> "$SHELL_RC"; fi; done; . "$ENV_FILE"; echo "环境变量已永久写入: $ENV_FILE"`;

const LINUX_LEGACY_NODE_CHECK_COMMAND = String.raw`has_node() { refresh_path; command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; }`;

const LINUX_STRICT_NODE_CHECK_COMMAND = String.raw`has_node() { refresh_path; command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1 || return 1; NODE_MAJOR="$(node --version 2>/dev/null | sed 's/^v//' | cut -d. -f1)"; NPM_MAJOR="$(npm --version 2>/dev/null | cut -d. -f1)"; [ "\${NODE_MAJOR:-0}" -ge 20 ] && [ "\${NPM_MAJOR:-0}" -ge 9 ]; }`;

const LINUX_LEGACY_CODEX_INSTALL_COMMAND = String.raw`npm install -g --registry "$NPM_REGISTRY" @openai/codex@latest;`;

const LINUX_HARDENED_CODEX_INSTALL_COMMAND = String.raw`npm cache verify >/dev/null 2>&1 || npm cache clean --force; npm install -g --registry "$NPM_REGISTRY" @openai/codex@latest; if [ $? -ne 0 ]; then echo "Codex 安装失败，清理缓存后重试..."; npm cache clean --force; npm install -g --force --registry "$NPM_REGISTRY" @openai/codex@latest; fi;`;

const WINDOWS_COMMAND_TEMPLATE = String.raw`try { Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned -Force -ErrorAction Stop } catch { Write-Host ('[!] 无法永久设置 PowerShell 执行策略: ' + $_) -ForegroundColor Yellow }; Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force; $ErrorActionPreference = 'Continue'; $apiKey = __API_KEY__; $baseUrl = __BASE_URL__; $model = __MODEL__; function Refresh-Path { $env:PATH = [System.Environment]::GetEnvironmentVariable('PATH','Machine') + ';' + [System.Environment]::GetEnvironmentVariable('PATH','User') }; function Test-Url([string]$u) { try { Invoke-WebRequest -Uri $u -Method Head -TimeoutSec 6 -UseBasicParsing -ErrorAction Stop | Out-Null; return $true } catch { return $false } }; function Has-Node { Refresh-Path; return [bool]((Get-Command node -ErrorAction SilentlyContinue) -and (Get-Command npm -ErrorAction SilentlyContinue)) }; $nodeReady = $false; if (Has-Node) { Write-Host ('Node.js 已安装：' + (node --version)) -ForegroundColor Green; $nodeReady = $true } else { Write-Host '未检测到 Node.js，尝试多种方式安装最新版...' -ForegroundColor Yellow; if (-not $nodeReady -and (Get-Command scoop -ErrorAction SilentlyContinue)) { Write-Host '[1/5] Scoop...' -ForegroundColor Cyan; try { scoop install nodejs 2>&1 | Out-Host; if (Has-Node) { $nodeReady = $true; Write-Host '[√] Scoop 成功' -ForegroundColor Green } } catch { Write-Host ('[×] Scoop 失败: ' + $_) -ForegroundColor Red } }; if (-not $nodeReady -and (Get-Command winget -ErrorAction SilentlyContinue)) { Write-Host '[2/5] Winget...' -ForegroundColor Cyan; try { $wout = winget install OpenJS.NodeJS --accept-source-agreements --accept-package-agreements --silent 2>&1; Write-Host $wout; Refresh-Path; if (-not (Has-Node)) { Write-Host '  最新版未成功，尝试 LTS...' -ForegroundColor Yellow; $wout2 = winget install OpenJS.NodeJS.LTS --accept-source-agreements --accept-package-agreements --silent 2>&1; Write-Host $wout2; Refresh-Path }; if (Has-Node) { $nodeReady = $true; Write-Host '[√] Winget 成功' -ForegroundColor Green } } catch { Write-Host ('[×] Winget 失败: ' + $_) -ForegroundColor Red } }; if (-not $nodeReady -and (Get-Command choco -ErrorAction SilentlyContinue)) { Write-Host '[3/5] Chocolatey...' -ForegroundColor Cyan; try { choco install nodejs -y --force 2>&1 | Out-Host; Refresh-Path; if (Has-Node) { $nodeReady = $true; Write-Host '[√] Choco 成功' -ForegroundColor Green } } catch { Write-Host ('[×] Choco 失败: ' + $_) -ForegroundColor Red } }; if (-not $nodeReady -and (Get-Command fnm -ErrorAction SilentlyContinue)) { Write-Host '[4/5] fnm...' -ForegroundColor Cyan; try { fnm install --latest 2>&1 | Out-Host; $defaultVersion = (fnm list | Select-Object -Last 1); if ($defaultVersion) { fnm default $defaultVersion 2>&1 | Out-Host }; Refresh-Path; if (Has-Node) { $nodeReady = $true; Write-Host '[√] fnm 成功' -ForegroundColor Green } } catch { Write-Host ('[×] fnm 失败: ' + $_) -ForegroundColor Red } }; if (-not $nodeReady) { Write-Host '[5/5] 直接下载安装包（兜底）...' -ForegroundColor Cyan; $arch = if ([Environment]::Is64BitOperatingSystem) { 'x64' } else { 'x86' }; $version = $null; @('https://nodejs.org/dist/index.json','https://registry.npmmirror.com/-/binary/node/index.json') | ForEach-Object { if (-not $version) { try { Write-Host ('  查询版本: ' + $_) -ForegroundColor DarkGray; $idx = Invoke-RestMethod -Uri $_ -TimeoutSec 10 -ErrorAction Stop; $version = $idx[0].version; Write-Host ('  最新版本: ' + $version) -ForegroundColor Green } catch { Write-Host ('  API不可用: ' + $_) -ForegroundColor Yellow } } }; if (-not $version) { Write-Host '  [×] 无法获取版本号' -ForegroundColor Red } else { $msiName = 'node-' + $version + '-' + $arch + '.msi'; $canOfficial = Test-Url 'https://nodejs.org'; if ($canOfficial) { Write-Host '  官方可访问' -ForegroundColor Green; $srcs = @(@{N='官方';U='https://nodejs.org/dist/' + $version + '/' + $msiName},@{N='镜像CDN';U='https://cdn.npmmirror.com/binaries/node/' + $version + '/' + $msiName},@{N='镜像Registry';U='https://registry.npmmirror.com/-/binary/node/' + $version + '/' + $msiName}) } else { Write-Host '  官方不可访问，优先镜像' -ForegroundColor Yellow; $srcs = @(@{N='镜像CDN';U='https://cdn.npmmirror.com/binaries/node/' + $version + '/' + $msiName},@{N='镜像Registry';U='https://registry.npmmirror.com/-/binary/node/' + $version + '/' + $msiName},@{N='官方';U='https://nodejs.org/dist/' + $version + '/' + $msiName}) }; $dlOk = $false; foreach ($s in $srcs) { if (-not $dlOk) { try { Write-Host ('  尝试: ' + $s.N + ' -> ' + $s.U) -ForegroundColor DarkGray; $tp = Join-Path $env:TEMP $msiName; Invoke-WebRequest -Uri $s.U -OutFile $tp -UseBasicParsing -TimeoutSec 300 -ErrorAction Stop; Write-Host '  MSI安装中...' -ForegroundColor DarkGray; Start-Process msiexec.exe -ArgumentList @('/i', $tp, '/qn', '/norestart') -Wait -PassThru -ErrorAction Stop | Out-Null; Refresh-Path; if (Test-Path ($env:ProgramFiles + '\nodejs\node.exe')) { $env:PATH += ';' + $env:ProgramFiles + '\nodejs' }; if (Has-Node) { $dlOk = $true; $nodeReady = $true; Write-Host ('[√] ' + $s.N + ' 安装成功') -ForegroundColor Green }; Remove-Item $tp -Force -ErrorAction SilentlyContinue } catch { Write-Host ('  ' + $s.N + ' 失败: ' + $_) -ForegroundColor Red } } } } } }; if (-not $nodeReady) { Write-Host ''; Write-Host '====== 所有方式均失败 ======' -ForegroundColor Red; Write-Host '请手动安装 Node.js：' -ForegroundColor Yellow; Write-Host '  官方: https://nodejs.org/en/download'; Write-Host '  镜像: https://npmmirror.com/mirrors/node/'; Write-Host '  winget install OpenJS.NodeJS / scoop install nodejs / choco install nodejs'; Write-Host '按任意键退出...'; $null = $Host.UI.RawUI.ReadKey('NoEcho,IncludeKeyDown') } else { Write-Host ('Node.js: ' + (node --version)) -ForegroundColor Green; Write-Host ('npm:     ' + (npm --version)) -ForegroundColor Green; Write-Host ''; Write-Host '========== 安装 Codex ==========' -ForegroundColor Cyan; $npmOk = $false; try { npm ping --registry https://registry.npmjs.org 2>&1 | Out-Null; if ($LASTEXITCODE -eq 0) { $npmOk = $true } } catch {}; if ($npmOk) { Write-Host 'npm 官方源可用' -ForegroundColor Green; npm install -g @openai/codex@latest } else { Write-Host 'npm 官方源不可用，切换淘宝镜像...' -ForegroundColor Yellow; npm config set registry https://registry.npmmirror.com; npm install -g @openai/codex@latest }; Write-Host ''; Write-Host '========== 配置 Codex ==========' -ForegroundColor Cyan; [System.Environment]::SetEnvironmentVariable('CODEX_API_KEY', $apiKey, 'User'); $env:CODEX_API_KEY = $apiKey; $codexDir = Join-Path $HOME '.codex'; New-Item -ItemType Directory -Force -Path $codexDir | Out-Null; $utf8NoBom = New-Object System.Text.UTF8Encoding $false; $configObj = @{ model_provider='codex'; model=$model; model_reasoning_effort='high'; disable_response_storage=$true; approval_policy='never'; sandbox_mode='danger-full-access'; web_search='live'; model_providers=@{ codex=@{ name='codex'; base_url=$baseUrl; wire_api='responses'; api_key=$apiKey } }; projects=@{ 'C:/WINDOWS/system32'=@{ trust_level='trusted' }; 'D:/work/wc_project'=@{ trust_level='trusted' } }; notice=@{ model_migrations=@{ 'gpt-5.3-codex'=$model } }; windows=@{ sandbox='elevated' } }; $authObj = @{ OPENAI_API_KEY=$apiKey }; $tomlLines = @('model_provider = "codex"',('model = "' + $model + '"'),'model_reasoning_effort = "high"','disable_response_storage = true','approval_policy = "never"','sandbox_mode = "danger-full-access"','web_search = "live"','','[model_providers.codex]','name = "codex"',('base_url = "' + $baseUrl + '"'),'wire_api = "responses"',('api_key = "' + $apiKey + '"'),'','[projects.''C:\WINDOWS\system32'']','trust_level = "trusted"','','[projects.''D:/work/wc_project'']','trust_level = "trusted"','','[notice.model_migrations]',('"gpt-5.3-codex" = "' + $model + '"'),'','[windows]','sandbox = "elevated"'); [System.IO.File]::WriteAllText((Join-Path $codexDir 'config.json'), ($configObj | ConvertTo-Json -Compress -Depth 10), $utf8NoBom); [System.IO.File]::WriteAllText((Join-Path $codexDir 'auth.json'), ($authObj | ConvertTo-Json -Compress), $utf8NoBom); [System.IO.File]::WriteAllText((Join-Path $codexDir 'config.toml'), ($tomlLines -join [Environment]::NewLine), $utf8NoBom); Write-Host '[√] 配置已写入' -ForegroundColor Green; Write-Host ''; Write-Host '========== 验证 ==========' -ForegroundColor Cyan; Write-Host ('Node.js: ' + (node --version)); Write-Host ('npm:     ' + (npm --version)); try { Write-Host ('Codex:   ' + (codex --version)) } catch { Write-Host 'Codex: 未找到' -ForegroundColor Yellow }; Get-ChildItem $codexDir | Format-Table Name,Length,LastWriteTime -AutoSize; Write-Host ''; Write-Host '========== 启动 Codex ==========' -ForegroundColor Cyan; codex --dangerously-bypass-approvals-and-sandbox }`;

const WINDOWS_LEGACY_REFRESH_PATH_COMMAND = String.raw`function Refresh-Path { $env:PATH = [System.Environment]::GetEnvironmentVariable('PATH','Machine') + ';' + [System.Environment]::GetEnvironmentVariable('PATH','User') };`;

const WINDOWS_HARDENED_REFRESH_PATH_COMMAND = String.raw`function Add-PathEntry([string]$pathEntry) { if ($pathEntry -and (Test-Path $pathEntry)) { $currentPath = @($env:PATH -split ';' | Where-Object { $null -ne $_ -and $_ -ne '' }); if ($currentPath -notcontains $pathEntry) { $env:PATH = $pathEntry + ';' + $env:PATH } } }; function Refresh-Path { $machinePath = [System.Environment]::GetEnvironmentVariable('PATH','Machine'); $userPath = [System.Environment]::GetEnvironmentVariable('PATH','User'); $parts = @($machinePath, $userPath) | Where-Object { $null -ne $_ -and $_ -ne '' }; $env:PATH = ($parts -join ';'); $nodeDirs = @((Join-Path $env:ProgramFiles 'nodejs'), (Join-Path $env:LOCALAPPDATA 'Programs\nodejs'), (Join-Path $env:APPDATA 'npm'), (Join-Path $HOME 'scoop\apps\nodejs\current'), (Join-Path $HOME 'scoop\apps\nodejs-lts\current'), (Join-Path $env:APPDATA 'fnm\aliases\default'), (Join-Path $env:APPDATA 'nvm\current')); $programFilesX86 = [Environment]::GetEnvironmentVariable('ProgramFiles(x86)'); if ($programFilesX86) { $nodeDirs += (Join-Path $programFilesX86 'nodejs') }; foreach ($dir in $nodeDirs) { Add-PathEntry $dir } };`;

const WINDOWS_LEGACY_NODE_CHECK_COMMAND = String.raw`function Has-Node { Refresh-Path; return [bool]((Get-Command node -ErrorAction SilentlyContinue) -and (Get-Command npm -ErrorAction SilentlyContinue)) };`;

const WINDOWS_HARDENED_NODE_CHECK_COMMAND = String.raw`function Test-NodeCommand($nodeCmd, $npmCmd) { if (-not $nodeCmd -or -not $npmCmd) { return $false }; try { $nodeVersion = & $nodeCmd.Source --version 2>$null; $npmVersion = & $npmCmd.Source --version 2>$null; return [bool]($nodeVersion -and $npmVersion) } catch { return $false } }; function Has-Node { Refresh-Path; $nodeCmd = Get-Command node -ErrorAction SilentlyContinue; $npmCmd = Get-Command npm -ErrorAction SilentlyContinue; if (Test-NodeCommand $nodeCmd $npmCmd) { return $true }; $nodeExeCandidates = @((Join-Path $env:ProgramFiles 'nodejs\node.exe'), (Join-Path $env:LOCALAPPDATA 'Programs\nodejs\node.exe')); $programFilesX86 = [Environment]::GetEnvironmentVariable('ProgramFiles(x86)'); if ($programFilesX86) { $nodeExeCandidates += (Join-Path $programFilesX86 'nodejs\node.exe') }; foreach ($nodeExe in $nodeExeCandidates) { if (Test-Path $nodeExe) { Add-PathEntry (Split-Path -Parent $nodeExe) } }; $nodeCmd = Get-Command node -ErrorAction SilentlyContinue; $npmCmd = Get-Command npm -ErrorAction SilentlyContinue; return (Test-NodeCommand $nodeCmd $npmCmd) };`;

const MACOS_COMMAND_TEMPLATE = String.raw`MODEL_NAME="__MODEL__" && BASE_URL="__BASE_URL__" && API_KEY="__API_KEY__" && SHELL_RC="$HOME/.zshrc" && [ ! -f "$SHELL_RC" ] && SHELL_RC="$HOME/.bashrc" && sed -i '' '/CODEX_API_KEY/d;/API_KEY/d;/BASE_URL/d;/MODEL_NAME/d' "$SHELL_RC" 2>/dev/null || true && echo "export MODEL_NAME=\"\$MODEL_NAME\"" >> "$SHELL_RC" && echo "export API_KEY=\"\$API_KEY\"" >> "$SHELL_RC" && echo "export BASE_URL=\"\$BASE_URL\"" >> "$SHELL_RC" && echo "export CODEX_API_KEY=\"\$API_KEY\"" >> "$SHELL_RC" && export MODEL_NAME="$MODEL_NAME" && export API_KEY="$API_KEY" && export BASE_URL="$BASE_URL" && export CODEX_API_KEY="$API_KEY" && test_url() { curl -sfI --connect-timeout 6 "$1" >/dev/null 2>&1; } && refresh_path() { eval "$(/opt/homebrew/bin/brew shellenv 2>/dev/null)" 2>/dev/null || true; eval "$(/usr/local/bin/brew shellenv 2>/dev/null)" 2>/dev/null || true; export PATH="/opt/homebrew/bin:/usr/local/bin:$HOME/.nvm/versions/node/$(ls $HOME/.nvm/versions/node/ 2>/dev/null | tail -1)/bin:$PATH" 2>/dev/null || true; hash -r 2>/dev/null || true; } && has_node() { refresh_path; command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; } && NODE_READY=false && if has_node; then echo "\033[0;32m[√] Node.js 已安装：$(node --version)\033[0m"; NODE_READY=true; else echo "\033[1;33m[!] 未检测到 Node.js，尝试安装最新版...\033[0m"; if ! $NODE_READY; then echo "\033[0;36m[1/4] Homebrew...\033[0m"; if ! command -v brew >/dev/null 2>&1; then echo "  安装 Homebrew..."; if test_url "https://raw.githubusercontent.com"; then /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"; elif test_url "https://gitee.com"; then /bin/bash -c "$(curl -fsSL https://gitee.com/cunkai/HomebrewCN/raw/master/Homebrew.sh)"; fi; refresh_path; fi; if command -v brew >/dev/null 2>&1; then brew update && brew install node && brew link node --force --overwrite 2>/dev/null; refresh_path; has_node && NODE_READY=true && echo "\033[0;32m[√] Homebrew 成功\033[0m" || echo "\033[0;31m[×] Homebrew 失败\033[0m"; fi; fi; if ! $NODE_READY && command -v port >/dev/null 2>&1; then echo "\033[0;36m[2/4] MacPorts...\033[0m"; sudo port install nodejs22 2>/dev/null && refresh_path && has_node && NODE_READY=true && echo "\033[0;32m[√] MacPorts 成功\033[0m"; fi; if ! $NODE_READY; then echo "\033[0;36m[3/4] nvm...\033[0m"; export NVM_DIR="$HOME/.nvm"; if [ ! -s "$NVM_DIR/nvm.sh" ]; then if test_url "https://raw.githubusercontent.com"; then curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash; elif test_url "https://gitee.com"; then curl -o- https://gitee.com/mirrors/nvm/raw/v0.40.3/install.sh | bash; fi; fi; if [ -s "$NVM_DIR/nvm.sh" ]; then . "$NVM_DIR/nvm.sh" && nvm install node && refresh_path && has_node && NODE_READY=true && echo "\033[0;32m[√] nvm 成功\033[0m"; fi; fi; if ! $NODE_READY; then echo "\033[0;36m[4/4] 直接下载（兜底）...\033[0m"; ARCH=$(uname -m); case "$ARCH" in x86_64) NODE_ARCH="darwin-x64";; arm64) NODE_ARCH="darwin-arm64";; *) NODE_ARCH="";; esac; if [ -n "$NODE_ARCH" ]; then FULL_VER=""; for u in "https://nodejs.org/dist/index.json" "https://registry.npmmirror.com/-/binary/node/index.json"; do [ -z "$FULL_VER" ] && FULL_VER=$(curl -s --connect-timeout 10 "$u" 2>/dev/null | grep -oE '"version"\s*:\s*"v[0-9.]+' | head -1 | grep -oE 'v[0-9.]+') || true; done; if [ -n "$FULL_VER" ]; then PKG="node-\${FULL_VER}.pkg"; if test_url "https://nodejs.org"; then SRCS=("https://nodejs.org/dist/\${FULL_VER}/\${PKG}" "https://cdn.npmmirror.com/binaries/node/\${FULL_VER}/\${PKG}"); else SRCS=("https://cdn.npmmirror.com/binaries/node/\${FULL_VER}/\${PKG}" "https://nodejs.org/dist/\${FULL_VER}/\${PKG}"); fi; for s in "\${SRCS[@]}"; do if ! $NODE_READY; then echo "  尝试: $s"; curl -fSL --connect-timeout 15 -o "/tmp/\${PKG}" "$s" 2>/dev/null && sudo installer -pkg "/tmp/\${PKG}" -target / 2>/dev/null && rm -f "/tmp/\${PKG}" && refresh_path && has_node && NODE_READY=true && echo "\033[0;32m[√] PKG 安装成功\033[0m" || echo "  失败"; fi; done; fi; fi; fi; fi && if ! $NODE_READY; then echo "\033[0;31m====== 所有方式均失败 ======\033[0m"; echo "请手动安装: https://nodejs.org 或 brew install node"; else echo "Node.js: $(node --version)"; echo "npm:     $(npm --version)"; echo "========== 安装 Codex =========="; if npm ping --registry https://registry.npmjs.org &>/dev/null; then echo "官方源可用"; npm install -g @openai/codex@latest || sudo npm install -g @openai/codex@latest; else echo "切换淘宝镜像"; npm config set registry https://registry.npmmirror.com || sudo npm config set registry https://registry.npmmirror.com; npm install -g @openai/codex@latest || sudo npm install -g @openai/codex@latest; fi; echo "========== 配置 Codex =========="; mkdir -p ~/.codex; printf '{"model_provider":"codex","model":"%s","model_reasoning_effort":"high","disable_response_storage":true,"approval_policy":"never","sandbox_mode":"danger-full-access","web_search":"live","model_providers":{"codex":{"name":"codex","base_url":"%s","wire_api":"responses","api_key":"%s","env_key":"CODEX_API_KEY"}},"projects":{"'"$HOME"'":{"trust_level":"trusted"}},"notice":{"model_migrations":{"gpt-5.3-codex":"%s"}}}' "$MODEL_NAME" "$BASE_URL" "$API_KEY" "$MODEL_NAME" > ~/.codex/config.json; printf 'model_provider = "codex"\nmodel = "%s"\nmodel_reasoning_effort = "high"\ndisable_response_storage = true\napproval_policy = "never"\nsandbox_mode = "danger-full-access"\nweb_search = "live"\n\n[model_providers.codex]\nname = "codex"\nbase_url = "%s"\nwire_api = "responses"\napi_key = "%s"\nenv_key = "CODEX_API_KEY"\n\n[notice.model_migrations]\n"gpt-5.3-codex" = "%s"\n' "$MODEL_NAME" "$BASE_URL" "$API_KEY" "$MODEL_NAME" > ~/.codex/config.toml; printf '{"OPENAI_API_KEY":"%s"}' "$API_KEY" > ~/.codex/auth.json; echo "[√] 配置已写入"; echo "========== 验证 =========="; node --version; npm --version; codex --version 2>/dev/null || echo "codex 未找到"; ls -la ~/.codex/; echo "========== 启动 Codex =========="; echo "环境变量已永久写入: $SHELL_RC"; echo "MODEL_NAME: $MODEL_NAME"; echo "API_KEY: $(echo $API_KEY | cut -c1-20)..."; echo "BASE_URL: $BASE_URL"; echo "CODEX_API_KEY: $(echo $CODEX_API_KEY | cut -c1-20)..."; codex --dangerously-bypass-approvals-and-sandbox; fi`;

const MACOS_BASH_COMMAND_TEMPLATE = String.raw`bash -c 'BASE_URL="__BASE_URL__" && API_KEY="__API_KEY__" && MODEL_NAME="__MODEL__" && SHELL_RC="$HOME/.zshrc" && [ ! -f "$SHELL_RC" ] && SHELL_RC="$HOME/.bashrc" && sed -i '\'''\'' '\''/CODEX_API_KEY/d;/API_KEY/d;/BASE_URL/d;/MODEL_NAME/d'\'' "$SHELL_RC" 2>/dev/null || true && echo "export MODEL_NAME=\"$MODEL_NAME\"" >> "$SHELL_RC" && echo "export API_KEY=\"$API_KEY\"" >> "$SHELL_RC" && echo "export BASE_URL=\"$BASE_URL\"" >> "$SHELL_RC" && echo "export CODEX_API_KEY=\"$API_KEY\"" >> "$SHELL_RC" && export MODEL_NAME="$MODEL_NAME" && export API_KEY="$API_KEY" && export BASE_URL="$BASE_URL" && export CODEX_API_KEY="$API_KEY" && test_url() { curl -sfI --connect-timeout 6 "$1" >/dev/null 2>&1; } && refresh_path() { eval "$(/opt/homebrew/bin/brew shellenv 2>/dev/null)" 2>/dev/null || true; eval "$(/usr/local/bin/brew shellenv 2>/dev/null)" 2>/dev/null || true; export PATH="/opt/homebrew/bin:/usr/local/bin:$HOME/.nvm/versions/node/$(ls $HOME/.nvm/versions/node/ 2>/dev/null | tail -1)/bin:$PATH" 2>/dev/null || true; hash -r 2>/dev/null || true; } && has_node() { refresh_path; command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; } && NODE_READY=false && if has_node; then echo "\033[0;32m[√] Node.js 已安装：$(node --version)\033[0m"; NODE_READY=true; else echo "\033[1;33m[!] 未检测到 Node.js，尝试安装最新版...\033[0m"; if ! $NODE_READY; then echo "\033[0;36m[1/4] Homebrew...\033[0m"; if ! command -v brew >/dev/null 2>&1; then echo "  安装 Homebrew..."; if test_url "https://raw.githubusercontent.com"; then /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"; elif test_url "https://gitee.com"; then /bin/bash -c "$(curl -fsSL https://gitee.com/cunkai/HomebrewCN/raw/master/Homebrew.sh)"; fi; refresh_path; fi; if command -v brew >/dev/null 2>&1; then brew update && brew install node && brew link node --force --overwrite 2>/dev/null; refresh_path; has_node && NODE_READY=true && echo "\033[0;32m[√] Homebrew 成功\033[0m" || echo "\033[0;31m[×] Homebrew 失败\033[0m"; fi; fi; if ! $NODE_READY && command -v port >/dev/null 2>&1; then echo "\033[0;36m[2/4] MacPorts...\033[0m"; sudo port install nodejs22 2>/dev/null && refresh_path && has_node && NODE_READY=true && echo "\033[0;32m[√] MacPorts 成功\033[0m"; fi; if ! $NODE_READY; then echo "\033[0;36m[3/4] nvm...\033[0m"; export NVM_DIR="$HOME/.nvm"; if [ ! -s "$NVM_DIR/nvm.sh" ]; then if test_url "https://raw.githubusercontent.com"; then curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash; elif test_url "https://gitee.com"; then curl -o- https://gitee.com/mirrors/nvm/raw/v0.40.3/install.sh | bash; fi; fi; if [ -s "$NVM_DIR/nvm.sh" ]; then . "$NVM_DIR/nvm.sh" && nvm install node && refresh_path && has_node && NODE_READY=true && echo "\033[0;32m[√] nvm 成功\033[0m"; fi; fi; if ! $NODE_READY; then echo "\033[0;36m[4/4] 直接下载（兜底）...\033[0m"; ARCH=$(uname -m); case "$ARCH" in x86_64) NODE_ARCH="darwin-x64";; arm64) NODE_ARCH="darwin-arm64";; *) NODE_ARCH="";; esac; if [ -n "$NODE_ARCH" ]; then FULL_VER=""; for u in "https://nodejs.org/dist/index.json" "https://registry.npmmirror.com/-/binary/node/index.json"; do [ -z "$FULL_VER" ] && FULL_VER=$(curl -s --connect-timeout 10 "$u" 2>/dev/null | grep -oE '\''"version"\s*:\s*"v[0-9.]+'\'' | head -1 | grep -oE '\''v[0-9.]+'\'') || true; done; if [ -n "$FULL_VER" ]; then PKG="node-\${FULL_VER}.pkg"; if test_url "https://nodejs.org"; then SRCS=("https://nodejs.org/dist/\${FULL_VER}/\${PKG}" "https://cdn.npmmirror.com/binaries/node/\${FULL_VER}/\${PKG}"); else SRCS=("https://cdn.npmmirror.com/binaries/node/\${FULL_VER}/\${PKG}" "https://nodejs.org/dist/\${FULL_VER}/\${PKG}"); fi; for s in "\${SRCS[@]}"; do if ! $NODE_READY; then echo "  尝试: $s"; curl -fSL --connect-timeout 15 -o "/tmp/\${PKG}" "$s" 2>/dev/null && sudo installer -pkg "/tmp/\${PKG}" -target / 2>/dev/null && rm -f "/tmp/\${PKG}" && refresh_path && has_node && NODE_READY=true && echo "\033[0;32m[√] PKG 安装成功\033[0m" || echo "  失败"; fi; done; fi; fi; fi; fi && if ! $NODE_READY; then echo "\033[0;31m====== 所有方式均失败 ======\033[0m"; echo "请手动安装: https://nodejs.org 或 brew install node"; else echo "Node.js: $(node --version)"; echo "npm:     $(npm --version)"; echo "========== 安装 Codex =========="; if npm ping --registry https://registry.npmjs.org &>/dev/null; then echo "官方源可用"; npm install -g @openai/codex@latest || sudo npm install -g @openai/codex@latest; else echo "切换淘宝镜像"; npm config set registry https://registry.npmmirror.com || sudo npm config set registry https://registry.npmmirror.com; npm install -g @openai/codex@latest || sudo npm install -g @openai/codex@latest; fi; echo "========== 配置 Codex =========="; mkdir -p ~/.codex; printf '\''{"model_provider":"codex","model":"%s","model_reasoning_effort":"high","disable_response_storage":true,"approval_policy":"never","sandbox_mode":"danger-full-access","web_search":"live","model_providers":{"codex":{"name":"codex","base_url":"%s","wire_api":"responses","env_key":"CODEX_API_KEY"}},"projects":{"'\''"$HOME"'\''":{"trust_level":"trusted"}},"notice":{"model_migrations":{"gpt-5.3-codex":"%s"}}}'\'' "$MODEL_NAME" "$BASE_URL" "$MODEL_NAME" > ~/.codex/config.json; printf '\''model_provider = "codex"\nmodel = "%s"\nmodel_reasoning_effort = "high"\ndisable_response_storage = true\napproval_policy = "never"\nsandbox_mode = "danger-full-access"\nweb_search = "live"\n\n[model_providers.codex]\nname = "codex"\nbase_url = "%s"\nwire_api = "responses"\nenv_key = "CODEX_API_KEY"\n\n[notice.model_migrations]\n"gpt-5.3-codex" = "%s"\n'\'' "$MODEL_NAME" "$BASE_URL" "$MODEL_NAME" > ~/.codex/config.toml; printf '\''{"OPENAI_API_KEY":"%s"}'\'' "$API_KEY" > ~/.codex/auth.json; echo "[√] 配置已写入"; echo "========== 验证 =========="; node --version; npm --version; codex --version 2>/dev/null || echo "codex 未找到"; ls -la ~/.codex/; echo "========== 启动 Codex =========="; echo "环境变量已永久写入: $SHELL_RC"; echo "MODEL_NAME: $MODEL_NAME"; echo "API_KEY: $(echo $API_KEY | cut -c1-20)..."; echo "BASE_URL: $BASE_URL"; echo "CODEX_API_KEY: $(echo $CODEX_API_KEY | cut -c1-20)..."; codex --dangerously-bypass-approvals-and-sandbox; fi'`;

function escapeForBashDoubleQuotes(value) {
  return value
    .replaceAll('\\', '\\\\')
    .replaceAll('"', '\\"')
    .replaceAll('$', '\\$')
    .replaceAll('`', '\\`');
}

function quotePowerShell(value) {
  return `'${value.replaceAll("'", "''")}'`;
}

function snakeOrCamel(config, snakeKey, camelKey) {
  if (Object.prototype.hasOwnProperty.call(config, camelKey)) {
    return config[camelKey];
  }
  return config[snakeKey];
}

function safeModels(value, fallback) {
  if (!Array.isArray(value)) {
    return fallback;
  }

  const models = Array.from(
    new Set(
      value
        .map((item) => String(item || '').trim())
        .filter((item) => /^[A-Za-z0-9._:-]{1,80}$/.test(item)),
    ),
  );
  return models.length > 0 ? models : fallback;
}

function safeReasoningEfforts(value, fallback) {
  if (!Array.isArray(value)) {
    return fallback;
  }

  const allowed = new Set(INSTALL_REASONING_EFFORTS.map((item) => item.id));
  const efforts = Array.from(
    new Set(
      value
        .map((item) => String(item || '').trim())
        .filter((item) => allowed.has(item)),
    ),
  );
  return efforts.length > 0 ? efforts : fallback;
}

export function normalizeInstallReasoningEffort(
  value,
  allowedEfforts = DEFAULT_INSTALL_COMMAND_CONFIG.reasoningEfforts,
) {
  const effort = String(value || '').trim();
  return allowedEfforts.includes(effort)
    ? effort
    : allowedEfforts.includes(DEFAULT_INSTALL_REASONING_EFFORT)
      ? DEFAULT_INSTALL_REASONING_EFFORT
      : allowedEfforts[0];
}

export function normalizeInstallCommandConfig(value = {}) {
  const config = value && typeof value === 'object' ? value : {};
  const models = safeModels(
    snakeOrCamel(config, 'models', 'models'),
    DEFAULT_INSTALL_COMMAND_CONFIG.models,
  );
  const requestedDefaultModel = String(
    snakeOrCamel(config, 'default_model', 'defaultModel') || '',
  ).trim();
  const reasoningEfforts = safeReasoningEfforts(
    snakeOrCamel(config, 'reasoning_efforts', 'reasoningEfforts'),
    DEFAULT_INSTALL_COMMAND_CONFIG.reasoningEfforts,
  );
  const requestedDefaultReasoningEffort = String(
    snakeOrCamel(
      config,
      'default_reasoning_effort',
      'defaultReasoningEffort',
    ) || '',
  ).trim();
  const approvalPolicy = String(
    snakeOrCamel(config, 'approval_policy', 'approvalPolicy') || '',
  ).trim();
  const sandboxMode = String(
    snakeOrCamel(config, 'sandbox_mode', 'sandboxMode') || '',
  ).trim();
  const workspaceName = String(
    snakeOrCamel(config, 'workspace_name', 'workspaceName') || '',
  ).trim();
  const windowsProjectPathStyle = String(
    snakeOrCamel(
      config,
      'windows_project_path_style',
      'windowsProjectPathStyle',
    ) || '',
  ).trim();

  return {
    ...DEFAULT_INSTALL_COMMAND_CONFIG,
    models,
    defaultModel: models.includes(requestedDefaultModel)
      ? requestedDefaultModel
      : models.includes(DEFAULT_INSTALL_COMMAND_CONFIG.defaultModel)
        ? DEFAULT_INSTALL_COMMAND_CONFIG.defaultModel
        : models[0],
    reasoningEfforts,
    defaultReasoningEffort: reasoningEfforts.includes(
      requestedDefaultReasoningEffort,
    )
      ? requestedDefaultReasoningEffort
      : reasoningEfforts.includes(DEFAULT_INSTALL_REASONING_EFFORT)
        ? DEFAULT_INSTALL_REASONING_EFFORT
        : reasoningEfforts[0],
    approvalPolicy: ['never', 'on-request', 'on-failure', 'untrusted'].includes(
      approvalPolicy,
    )
      ? approvalPolicy
      : DEFAULT_INSTALL_COMMAND_CONFIG.approvalPolicy,
    sandboxMode: [
      'danger-full-access',
      'workspace-write',
      'read-only',
    ].includes(sandboxMode)
      ? sandboxMode
      : DEFAULT_INSTALL_COMMAND_CONFIG.sandboxMode,
    supportsWebsockets: Boolean(
      snakeOrCamel(config, 'supports_websockets', 'supportsWebsockets'),
    ),
    workspaceName: /^[A-Za-z0-9._-]{1,64}$/.test(workspaceName)
      ? workspaceName
      : DEFAULT_INSTALL_COMMAND_CONFIG.workspaceName,
    windowsProjectPathStyle:
      windowsProjectPathStyle === 'escaped-backslash'
        ? 'escaped-backslash'
        : DEFAULT_INSTALL_COMMAND_CONFIG.windowsProjectPathStyle,
    launchBypassFlag: Boolean(
      snakeOrCamel(config, 'launch_bypass_flag', 'launchBypassFlag'),
    ),
  };
}

export function installModelsFromConfig(config) {
  return normalizeInstallCommandConfig(config).models;
}

export function installReasoningEffortsFromConfig(config) {
  const normalized = normalizeInstallCommandConfig(config);
  return normalized.reasoningEfforts.map((id) => ({
    id,
    label:
      INSTALL_REASONING_EFFORTS.find((item) => item.id === id)?.label || id,
  }));
}

export function buildInstallCommandModels(config) {
  return installModelsFromConfig(config);
}

export function normalizeCodexBaseUrl(value) {
  const normalized = value.trim().replace(/\/+$/, '');
  if (!normalized) {
    return 'https://api.opencodex.uk/v1';
  }

  try {
    const url = new URL(normalized);
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
    return normalized;
  }
}

export function defaultBaseUrlFromStatus() {
  try {
    const status = JSON.parse(localStorage.getItem('status') || '{}');
    const serverAddress =
      status.server_address ||
      status.data?.server_address ||
      window.location.origin;
    return normalizeCodexBaseUrl(serverAddress);
  } catch {
    return normalizeCodexBaseUrl(window.location.origin);
  }
}

function renderTemplate(template, replacements) {
  return Object.entries(replacements).reduce(
    (current, [key, value]) => current.replaceAll(key, value),
    template,
  );
}

function forceResponsesHttpConfig(template) {
  return template
    .replaceAll(
      ',"env_key":"CODEX_API_KEY"',
      ',"env_key":"CODEX_API_KEY","supports_websockets":false',
    )
    .replaceAll(
      '\\nenv_key = "CODEX_API_KEY"\\n',
      '\\nenv_key = "CODEX_API_KEY"\\nsupports_websockets = false\\n',
    )
    .replaceAll(' --dangerously-bypass-approvals-and-sandbox', '')
    .replaceAll(
      ',"supports_websockets":false,"supports_websockets":false',
      ',"supports_websockets":false',
    )
    .replaceAll(
      '\\nsupports_websockets = false\\nsupports_websockets = false\\n',
      '\\nsupports_websockets = false\\n',
    );
}

function applyInstallCommandConfig(command, config, platform, reasoningEffort) {
  const normalizedConfig = normalizeInstallCommandConfig(config);
  const normalizedReasoningEffort =
    normalizeInstallReasoningEffort(
      reasoningEffort,
      normalizedConfig.reasoningEfforts,
    );
  let result = command
    .replaceAll(
      '"model_reasoning_effort":"high"',
      `"model_reasoning_effort":"${normalizedReasoningEffort}"`,
    )
    .replaceAll(
      "model_reasoning_effort='high'",
      `model_reasoning_effort='${normalizedReasoningEffort}'`,
    )
    .replaceAll(
      'model_reasoning_effort = "high"',
      `model_reasoning_effort = "${normalizedReasoningEffort}"`,
    )
    .replaceAll(
      '"approval_policy":"never"',
      `"approval_policy":"${normalizedConfig.approvalPolicy}"`,
    )
    .replaceAll(
      "approval_policy='never'",
      `approval_policy='${normalizedConfig.approvalPolicy}'`,
    )
    .replaceAll(
      'approval_policy = "never"',
      `approval_policy = "${normalizedConfig.approvalPolicy}"`,
    )
    .replaceAll(
      '"sandbox_mode":"danger-full-access"',
      `"sandbox_mode":"${normalizedConfig.sandboxMode}"`,
    )
    .replaceAll(
      "sandbox_mode='danger-full-access'",
      `sandbox_mode='${normalizedConfig.sandboxMode}'`,
    )
    .replaceAll(
      'sandbox_mode = "danger-full-access"',
      `sandbox_mode = "${normalizedConfig.sandboxMode}"`,
    )
    .replaceAll(
      '"supports_websockets":false',
      `"supports_websockets":${normalizedConfig.supportsWebsockets ? 'true' : 'false'}`,
    )
    .replaceAll(
      'supports_websockets=$false',
      `supports_websockets=$${normalizedConfig.supportsWebsockets ? 'true' : 'false'}`,
    )
    .replaceAll(
      'supports_websockets = false',
      `supports_websockets = ${normalizedConfig.supportsWebsockets ? 'true' : 'false'}`,
    )
    .replaceAll('opencodex-workspace', normalizedConfig.workspaceName);

  if (
    platform === 'windows' &&
    normalizedConfig.windowsProjectPathStyle === 'escaped-backslash'
  ) {
    result = result
      .replaceAll(
        "$workDir.Replace('\\\\','/')",
        "$workDir.Replace('\\\\','\\\\\\\\')",
      )
      .replaceAll(
        "$workDir.Replace('\\','/')",
        "$workDir.Replace('\\','\\\\')",
      );
  }

  if (normalizedConfig.launchBypassFlag) {
    result = result
      .replaceAll(
        '"$CODEX_BIN"',
        '"$CODEX_BIN" --dangerously-bypass-approvals-and-sandbox',
      )
      .replaceAll(
        '& $codexCmd.Source',
        '& $codexCmd.Source --dangerously-bypass-approvals-and-sandbox',
      )
      .replaceAll(
        'codex --version 2>/dev/null || echo "codex 未找到";',
        'codex --version 2>/dev/null || echo "codex 未找到"; codex --dangerously-bypass-approvals-and-sandbox;',
      );
  }

  return result;
}

const MACOS_LEGACY_ENV_SETUP_COMMAND = String.raw`SHELL_RC="$HOME/.zshrc" && [ ! -f "$SHELL_RC" ] && SHELL_RC="$HOME/.bashrc" && sed -i '\'''\'' '\''/CODEX_API_KEY/d;/API_KEY/d;/BASE_URL/d;/MODEL_NAME/d'\'' "$SHELL_RC" 2>/dev/null || true && echo "export MODEL_NAME=\"$MODEL_NAME\"" >> "$SHELL_RC" && echo "export API_KEY=\"$API_KEY\"" >> "$SHELL_RC" && echo "export BASE_URL=\"$BASE_URL\"" >> "$SHELL_RC" && echo "export CODEX_API_KEY=\"$API_KEY\"" >> "$SHELL_RC" && export MODEL_NAME="$MODEL_NAME" && export API_KEY="$API_KEY" && export BASE_URL="$BASE_URL" && export CODEX_API_KEY="$API_KEY"`;

const MACOS_PERSISTENT_ENV_SETUP_COMMAND = String.raw`mkdir -p "$HOME/.codex" && ENV_FILE="$HOME/.codex/env" && printf '\''export MODEL_NAME=%q\nexport API_KEY=%q\nexport BASE_URL=%q\nexport CODEX_API_KEY=%q\nexport OPENAI_API_KEY=%q\nexport PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"\n'\'' "$MODEL_NAME" "$API_KEY" "$BASE_URL" "$API_KEY" "$API_KEY" > "$ENV_FILE" && chmod 600 "$ENV_FILE" && for SHELL_RC in "$HOME/.zshrc" "$HOME/.zprofile" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile"; do touch "$SHELL_RC" 2>/dev/null || true; if [ -f "$SHELL_RC" ]; then sed -i '\'''\'' '\''/# OpenCodex environment/d;/\.codex\/env/d;/^export MODEL_NAME=/d;/^export API_KEY=/d;/^export BASE_URL=/d;/^export CODEX_API_KEY=/d;/^export OPENAI_API_KEY=/d'\'' "$SHELL_RC" 2>/dev/null || true; printf '\''\n# OpenCodex environment\n[ -f "$HOME/.codex/env" ] && . "$HOME/.codex/env"\n'\'' >> "$SHELL_RC"; fi; done && . "$ENV_FILE"`;

const MACOS_GLOBAL_NPM_INSTALL_COMMAND = String.raw`npm install -g @openai/codex@latest || sudo npm install -g @openai/codex@latest`;

const MACOS_USER_NPM_INSTALL_COMMAND = String.raw`NPM_PREFIX="$HOME/.local" && mkdir -p "$NPM_PREFIX/bin" && npm config set prefix "$NPM_PREFIX" && export PATH="$NPM_PREFIX/bin:$PATH" && npm install -g @openai/codex@latest`;

function hardenMacosCommandTemplate(template) {
  return forceResponsesHttpConfig(
    template
      .replace(
        MACOS_LEGACY_ENV_SETUP_COMMAND,
        MACOS_PERSISTENT_ENV_SETUP_COMMAND,
      )
      .replaceAll(
        MACOS_GLOBAL_NPM_INSTALL_COMMAND,
        MACOS_USER_NPM_INSTALL_COMMAND,
      )
      .replace(
        'echo "环境变量已永久写入: $SHELL_RC";',
        'echo "环境变量已永久写入: $ENV_FILE";',
      )
      .replace(
        'echo "========== 配置 Codex =========="; mkdir -p ~/.codex;',
        'echo "========== 配置 Codex =========="; WORK_DIR="$HOME/opencodex-workspace"; mkdir -p ~/.codex "$WORK_DIR";',
      )
      .replace(
        `"projects":{"'"$HOME"'":{"trust_level":"trusted"}}`,
        `"projects":{"'"$WORK_DIR"'":{"trust_level":"trusted"}}`,
      )
      .replace(
        '\\n[notice.model_migrations]\\n"gpt-5.3-codex" = "%s"\\n',
        '\\n[projects."%s"]\\ntrust_level = "trusted"\\n\\n[notice.model_migrations]\\n"gpt-5.3-codex" = "%s"\\n',
      )
      .replace(
        `" "$MODEL_NAME" "$BASE_URL" "$MODEL_NAME" > ~/.codex/config.toml;`,
        `" "$MODEL_NAME" "$BASE_URL" "$WORK_DIR" "$MODEL_NAME" > ~/.codex/config.toml;`,
      )
      .replace(
        'codex --dangerously-bypass-approvals-and-sandbox; fi',
        'cd "$WORK_DIR" && codex; fi',
      ),
  );
}

function hardenLinuxCommandTemplate(template) {
  return forceResponsesHttpConfig(
    template
      .replace(LINUX_LEGACY_NODE_CHECK_COMMAND, LINUX_STRICT_NODE_CHECK_COMMAND)
      .replace(
        '未检测到 Node.js，尝试多种方式安装最新版...',
        '未检测到可用 Node.js/npm，或版本过旧，尝试安装最新版...',
      )
      .replace(
        'Node.js 已安装：$(node --version)',
        'Node.js/npm 版本可用：$(node --version) / $(npm --version)',
      )
      .replace(
        LINUX_LEGACY_CODEX_INSTALL_COMMAND,
        LINUX_HARDENED_CODEX_INSTALL_COMMAND,
      )
      .replace(
        LINUX_LEGACY_ENV_CONFIG_COMMAND,
        LINUX_PERSISTENT_ENV_CONFIG_COMMAND,
      ),
  );
}

function hardenWindowsCommandTemplate(template) {
  return template
    .replace(
      '$model = __MODEL__; function Refresh-Path',
      "$model = __MODEL__; $isAdmin = $false; try { $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent()); $isAdmin = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) } catch {}; if ($isAdmin) { Write-Host '当前 PowerShell: 管理员模式' -ForegroundColor Yellow; Write-Host '提示: 将写入当前管理员用户的 npm/Codex 配置，普通账户需要在自己的 PowerShell 中重新运行' -ForegroundColor Yellow } else { Write-Host '当前 PowerShell: 普通用户模式' -ForegroundColor Green }; Write-Host ('当前用户: ' + [Environment]::UserName + ' / HOME: ' + $HOME) -ForegroundColor DarkGray; function Refresh-Path",
    )
    .replace(
      WINDOWS_LEGACY_REFRESH_PATH_COMMAND,
      WINDOWS_HARDENED_REFRESH_PATH_COMMAND,
    )
    .replace(
      WINDOWS_LEGACY_NODE_CHECK_COMMAND,
      WINDOWS_HARDENED_NODE_CHECK_COMMAND,
    )
    .replace(
      'winget install OpenJS.NodeJS --accept-source-agreements --accept-package-agreements --silent',
      'winget install OpenJS.NodeJS.LTS --accept-source-agreements --accept-package-agreements --silent',
    )
    .replace(
      "Write-Host '  最新版未成功，尝试 LTS...' -ForegroundColor Yellow; $wout2 = winget install OpenJS.NodeJS.LTS --accept-source-agreements --accept-package-agreements --silent 2>&1;",
      "Write-Host '  LTS 未成功，尝试最新版...' -ForegroundColor Yellow; $wout2 = winget install OpenJS.NodeJS --accept-source-agreements --accept-package-agreements --silent 2>&1;",
    )
    .replace(
      "$version = $idx[0].version; Write-Host ('  最新版本: ' + $version)",
      "$lts = @($idx | Where-Object { $_.lts } | Select-Object -First 1); if ($lts) { $version = $lts.version } else { $version = $idx[0].version }; Write-Host ('  选择版本: ' + $version)",
    )
    .replace(
      "Write-Host ''; Write-Host '========== 安装 Codex ==========' -ForegroundColor Cyan; $npmOk = $false;",
      "Write-Host ''; Write-Host '========== 安装 Codex ==========' -ForegroundColor Cyan; $runningCodex = Get-Process codex -ErrorAction SilentlyContinue; if ($runningCodex) { Write-Host '检测到 Codex 正在运行，先关闭以避免 codex.exe 被占用...' -ForegroundColor Yellow; $runningCodex | Stop-Process -Force -ErrorAction SilentlyContinue; Start-Sleep -Seconds 2 }; if (Get-Command winget -ErrorAction SilentlyContinue) { try { Write-Host '安装/修复 VC++ Runtime...' -ForegroundColor Cyan; winget install Microsoft.VCRedist.2015+.x64 --accept-source-agreements --accept-package-agreements --silent 2>&1 | Out-Host } catch { Write-Host ('VC++ Runtime 安装跳过: ' + $_) -ForegroundColor Yellow } }; $npmOk = $false;",
    )
    .replaceAll(
      'npm install -g @openai/codex@latest',
      'npm install -g @openai/codex@latest --force',
    )
    .replace(
      "}; if ($npmOk) { Write-Host 'npm 官方源可用' -ForegroundColor Green; npm install -g @openai/codex@latest --force } else { Write-Host 'npm 官方源不可用，切换淘宝镜像...' -ForegroundColor Yellow; npm config set registry https://registry.npmmirror.com; npm install -g @openai/codex@latest --force };",
      "}; $npmRegistry = if ($npmOk) { 'https://registry.npmjs.org' } else { 'https://registry.npmmirror.com' }; if ($npmOk) { Write-Host 'npm 官方源可用' -ForegroundColor Green } else { Write-Host 'npm 官方源不可用，切换淘宝镜像...' -ForegroundColor Yellow; npm config set registry $npmRegistry }; npm uninstall -g @openai/codex 2>&1 | Out-Host; npm cache verify 2>&1 | Out-Host; npm install -g @openai/codex@latest --force --include=optional --registry $npmRegistry; if ($LASTEXITCODE -ne 0) { Write-Host 'latest 安装失败，尝试固定 Windows x64 native 包...' -ForegroundColor Yellow; npm install -g @openai/codex@0.130.0 '@openai/codex-win32-x64@npm:@openai/codex@0.130.0-win32-x64' --force --include=optional --registry $npmRegistry };",
    )
    .replace(
      "}; Write-Host ''; Write-Host '========== 配置 Codex ==========' -ForegroundColor Cyan;",
      "}; if ($LASTEXITCODE -ne 0) { Write-Host '[×] Codex 安装失败，请检查 npm 输出' -ForegroundColor Red; exit 1 }; Refresh-Path; $codexCmd = Get-Command codex -ErrorAction SilentlyContinue; if (-not $codexCmd) { Write-Host '[×] codex 命令未进入 PATH，请新开 PowerShell 后重试' -ForegroundColor Red; exit 1 }; $npmRoot = $null; try { $npmRoot = (npm root -g 2>$null | Select-Object -First 1).Trim() } catch {}; $candidateRoots = @(); if ($npmRoot) { $candidateRoots += $npmRoot }; if ($codexCmd.Source) { $candidateRoots += (Split-Path -Parent $codexCmd.Source) }; $candidateRoots += (Join-Path (Join-Path $env:APPDATA 'npm') 'node_modules'); $candidateRoots = $candidateRoots | Where-Object { $_ -and (Test-Path $_) } | Select-Object -Unique; $nativeExe = $null; foreach ($root in $candidateRoots) { if (-not $nativeExe) { $nativeExe = Get-ChildItem -Path $root -Filter codex.exe -Recurse -ErrorAction SilentlyContinue | Where-Object { $_.FullName -match 'codex-win32|pc-windows-msvc' } | Select-Object -First 1 } }; if (-not $nativeExe) { Write-Host '[!] 未在常见 npm 全局目录找到 Windows native codex.exe，跳过强制路径校验并直接启动 Codex' -ForegroundColor Yellow }; Write-Host ('Codex binary: ' + $codexCmd.Source) -ForegroundColor Green; if ($nativeExe) { Write-Host ('Native exe:   ' + $nativeExe.FullName) -ForegroundColor Green }; Write-Host ''; Write-Host '========== 配置 Codex ==========' -ForegroundColor Cyan;",
    )
    .replace(
      "codex=@{ name='codex'; base_url=$baseUrl; wire_api='responses'; api_key=$apiKey }",
      "codex=@{ name='codex'; base_url=$baseUrl; wire_api='responses'; env_key='CODEX_API_KEY'; supports_websockets=$false }",
    )
    .replace(
      "$codexDir = Join-Path $HOME '.codex'; New-Item -ItemType Directory -Force -Path $codexDir | Out-Null;",
      "$codexDir = Join-Path $HOME '.codex'; $workDir = Join-Path $HOME 'opencodex-workspace'; New-Item -ItemType Directory -Force -Path $codexDir | Out-Null; New-Item -ItemType Directory -Force -Path $workDir | Out-Null;",
    )
    .replace(
      "projects=@{ 'C:/WINDOWS/system32'=@{ trust_level='trusted' }; 'D:/work/wc_project'=@{ trust_level='trusted' } }",
      "projects=@{ ($workDir.Replace('\\\\','/'))=@{ trust_level='trusted' } }",
    )
    .replace(
      "'D:/work/wc_project'=@{ trust_level='trusted' }",
      "'D:/work/wc_project'=@{ trust_level='trusted' }; 'C:/Users/Admin'=@{ trust_level='trusted' }",
    )
    .replace(
      "'[projects.''D:/work/wc_project'']','trust_level = \"trusted\"','','[notice.model_migrations]'",
      "'[projects.''D:/work/wc_project'']','trust_level = \"trusted\"','','[projects.''C:/Users/Admin'']','trust_level = \"trusted\"','','[notice.model_migrations]'",
    )
    .replace(
      "'','[projects.''C:\\WINDOWS\\system32'']','trust_level = \"trusted\"','','[projects.''D:/work/wc_project'']','trust_level = \"trusted\"','','[projects.''C:/Users/Admin'']','trust_level = \"trusted\"','','[notice.model_migrations]'",
      "'',('[projects.\"' + $workDir.Replace('\\','/') + '\"]'),'trust_level = \"trusted\"','','[notice.model_migrations]'",
    )
    .replace(
      "('api_key = \"' + $apiKey + '\"'),''",
      "'env_key = \"CODEX_API_KEY\"','supports_websockets = false',''",
    )
    .replace(
      "Write-Host '[√] 配置已写入' -ForegroundColor Green",
      "Write-Host '[√] 配置已写入：全自动模式' -ForegroundColor Green",
    )
    .replace("; windows=@{ sandbox='elevated' }", '')
    .replace(",'','[windows]','sandbox = \"elevated\"'", '')
    .replace(
      "Write-Host ''; Write-Host '========== 启动 Codex ==========' -ForegroundColor Cyan; codex --dangerously-bypass-approvals-and-sandbox",
      "Write-Host ''; Write-Host '========== 启动 Codex ==========' -ForegroundColor Cyan; Set-Location $workDir; & $codexCmd.Source",
    );
}

function buildLinuxCommand(apiKey, baseUrl, model, reasoningEffort, config) {
  return applyInstallCommandConfig(
    renderTemplate(forceResponsesHttpConfig(LINUX_CURRENT_COMMAND_TEMPLATE), {
      __API_KEY__: escapeForBashDoubleQuotes(apiKey.trim() || 'YOUR_KEY'),
      __BASE_URL__: escapeForBashDoubleQuotes(
        baseUrl.trim() || 'https://api.example.com/v1',
      ),
      __MODEL__: escapeForBashDoubleQuotes(
        model.trim() || DEFAULT_INSTALL_COMMAND_CONFIG.defaultModel,
      ),
    }),
    config,
    'linux',
    reasoningEffort,
  );
}

function buildMacosCommand(apiKey, baseUrl, model, reasoningEffort, config) {
  return applyInstallCommandConfig(
    renderTemplate(hardenMacosCommandTemplate(MACOS_BASH_COMMAND_TEMPLATE), {
      __API_KEY__: escapeForBashDoubleQuotes(apiKey.trim() || 'YOUR_KEY'),
      __BASE_URL__: escapeForBashDoubleQuotes(
        baseUrl.trim() || 'https://api.example.com/v1',
      ),
      __MODEL__: escapeForBashDoubleQuotes(
        model.trim() || DEFAULT_INSTALL_COMMAND_CONFIG.defaultModel,
      ),
    }),
    config,
    'macos',
    reasoningEffort,
  );
}

function buildWindowsCommand(apiKey, baseUrl, model, reasoningEffort, config) {
  return applyInstallCommandConfig(
    renderTemplate(hardenWindowsCommandTemplate(WINDOWS_COMMAND_TEMPLATE), {
      __API_KEY__: quotePowerShell(apiKey.trim() || 'YOUR_KEY'),
      __BASE_URL__: quotePowerShell(
        baseUrl.trim() || 'https://api.example.com/v1',
      ),
      __MODEL__: quotePowerShell(
        model.trim() || DEFAULT_INSTALL_COMMAND_CONFIG.defaultModel,
      ),
    }),
    config,
    'windows',
    reasoningEffort,
  );
}

export function buildInstallCommand(
  os,
  apiKey,
  baseUrl,
  model,
  reasoningEffort = DEFAULT_INSTALL_REASONING_EFFORT,
  config = {},
) {
  const codexBaseUrl = normalizeCodexBaseUrl(baseUrl);
  const normalizedConfig = normalizeInstallCommandConfig(config);
  const selectedModel = normalizedConfig.models.includes(model)
    ? model
    : normalizedConfig.defaultModel;

  if (os === 'windows') {
    return buildWindowsCommand(
      apiKey,
      codexBaseUrl,
      selectedModel,
      reasoningEffort,
      normalizedConfig,
    );
  }

  if (os === 'macos') {
    return buildMacosCommand(
      apiKey,
      codexBaseUrl,
      selectedModel,
      reasoningEffort,
      normalizedConfig,
    );
  }

  return buildLinuxCommand(
    apiKey,
    codexBaseUrl,
    selectedModel,
    reasoningEffort,
    normalizedConfig,
  );
}
