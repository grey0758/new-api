const psQuote = (value) => "'" + value.replaceAll("'", "''") + "'";

// A running terminal keeps its inherited environment after a User-scope update.
// Resolve credentials from the private file at launch instead of that snapshot.
const launchScript = String.raw`$ErrorActionPreference = 'Stop'
$codexDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' }
$settings = Get-Content -LiteralPath (Join-Path $codexDir 'opencodex-env.json') -Raw | ConvertFrom-Json
$env:CODEX_HOME = $codexDir
$env:CODEX_API_KEY = $settings.key
$env:BASE_URL = $settings.baseUrl
$env:MODEL_NAME = $settings.model
Remove-Item Env:API_KEY,Env:OPENAI_API_KEY -ErrorAction SilentlyContinue
& $settings.binary @args
exit $LASTEXITCODE`;

const setup = [
  "$ErrorActionPreference = 'Stop'",
  '$nativePath = if ($codexExe) { $codexExe } elseif ($nativeExe) { $nativeExe.FullName } else { $codexCmd.Source }',
  '$envSettings = @{ key=$apiKey; baseUrl=$baseUrl; model=$model; binary=$nativePath }',
  "[System.IO.File]::WriteAllText((Join-Path $codexDir 'opencodex-env.json'), ($envSettings | ConvertTo-Json -Depth 3), $utf8NoBom)",
  "$codexBinDir = Join-Path $HOME '.local\\bin'",
  'New-Item -ItemType Directory -Force -Path $codexBinDir | Out-Null',
  "$launchPath = Join-Path $codexBinDir 'codex.ps1'",
  `[System.IO.File]::WriteAllText($launchPath, ${psQuote(launchScript)}, $utf8NoBom)`,
  "$cmdPath = Join-Path $codexBinDir 'codex.cmd'",
  `[System.IO.File]::WriteAllLines($cmdPath, @('@echo off', ('powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "' + $launchPath + '" %*')), [System.Text.Encoding]::ASCII)`,
  "$userPath = [Environment]::GetEnvironmentVariable('Path','User')",
  "$userParts = @($userPath -split ';' | Where-Object { $_ -and $_.TrimEnd('\\') -ne $codexBinDir.TrimEnd('\\') })",
  "[Environment]::SetEnvironmentVariable('Path', ((@($codexBinDir) + $userParts) -join ';'), 'User')",
  "$env:PATH = $codexBinDir + ';' + $env:PATH",
  "[Environment]::SetEnvironmentVariable('CODEX_HOME', $codexDir, 'User')",
  '$env:CODEX_HOME = $codexDir',
  "foreach ($name in @('API_KEY','OPENAI_API_KEY')) { [Environment]::SetEnvironmentVariable($name, $null, 'User'); Remove-Item ('Env:' + $name) -ErrorAction SilentlyContinue }",
  // File-backed login also updates clients which consult auth.json.
  // Windows PowerShell 5 treats redirected native stderr as an ErrorRecord;
  // Codex writes successful login diagnostics there too. Check its exit code.
  "$savedErrorPreference = $ErrorActionPreference; try { $ErrorActionPreference = 'Continue'; $apiKey | & $nativePath login --with-api-key 2>$null | Out-Null; $loginExit = $LASTEXITCODE } finally { $ErrorActionPreference = $savedErrorPreference }",
  "if ($loginExit -ne 0) { throw 'Codex API-key login failed' }",
  '$currentIdentity = [Security.Principal.WindowsIdentity]::GetCurrent().Name',
  "foreach ($file in @('opencodex-env.json','auth.json','config.toml','config.json')) { $privatePath = Join-Path $codexDir $file; if (Test-Path -LiteralPath $privatePath) { & icacls.exe $privatePath /inheritance:r /grant:r ($currentIdentity + ':F') /Q 2>$null | Out-Null; if ($LASTEXITCODE -ne 0) { throw 'Could not protect Codex credentials' } } }",
  'Set-Alias -Name codex -Value $launchPath -Scope Global',
  '$codexCmd = [pscustomobject]@{ Source = $launchPath }',
].join('; ');

export function withWindowsCredentialReload(command) {
  // The native package is complete; the historical Node/npm prerequisite and
  // recursive npm executable search would select an unrelated old installation.
  const parameters = command.slice(
    command.indexOf('$apiKey ='),
    command.indexOf('function Refresh-Path'),
  );
  const nativeStart = command.indexOf('$codexRelease =');
  const nativeEnd = command.indexOf(
    '; $codexCmd = [pscustomobject]@{ Source = $codexExe };',
    nativeStart,
  );
  const configStart = command.indexOf(
    "Write-Host ''; Write-Host '========== 配置 Codex =========='",
    nativeEnd,
  );
  if (
    nativeStart < 0 ||
    nativeEnd < 0 ||
    configStart < 0 ||
    !command.endsWith(' }')
  ) {
    throw new Error('Windows native installer boundary changed');
  }
  return (
    "Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force; $ErrorActionPreference = 'Stop'; " +
    parameters +
    command.slice(nativeStart, nativeEnd) +
    '; $codexCmd = [pscustomobject]@{ Source = $codexExe }; $nativeExe = $null; ' +
    command.slice(configStart, -2)
  )
    .replace(
      /try \{ Set-ExecutionPolicy -Scope CurrentUser[\s\S]*?; Set-ExecutionPolicy -Scope Process/,
      'Set-ExecutionPolicy -Scope Process',
    )
    .replace(
      /\$runningCodex = Get-Process codex[\s\S]*?; if \(Get-Command winget/,
      'if (Get-Command winget',
    )
    .replace(
      "$codexDir = Join-Path $HOME '.codex';",
      "$codexDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' };",
    )
    .replace(
      "Write-Host ('Node.js: ' + (node --version)); Write-Host ('npm:     ' + (npm --version)); ",
      '',
    )
    .replace(
      "'disable_response_storage = true',",
      '\'cli_auth_credentials_store = "file"\',',
    )
    .replace(
      'disable_response_storage=$true;',
      "cli_auth_credentials_store='file';",
    )
    .replace(
      "$codexRoot = Join-Path $HOME '.codex\\packages\\standalone\\releases'",
      "$codexPackageHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' }; $codexRoot = Join-Path $codexPackageHome 'packages\\standalone\\releases'",
    )
    .replace(
      'Invoke-WebRequest -Uri',
      'Invoke-WebRequest -UseBasicParsing -Uri',
    )
    .replace('当前管理员用户的 npm/Codex 配置', '当前管理员用户的 Codex 配置')
    .replace(
      "Write-Host '[√] 配置已写入：全自动模式' -ForegroundColor Green",
      `${setup}; Write-Host '[√] 配置已写入：全自动模式' -ForegroundColor Green`,
    )
    .replace(
      'Set-Location $workDir; & $codexCmd.Source',
      "Set-Location $workDir; if ($env:OPENCODEX_INSTALL_NO_LAUNCH -ne '1') { & $codexCmd.Source }",
    );
}
