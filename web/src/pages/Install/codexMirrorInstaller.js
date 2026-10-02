const BASE = 'https://api.opencodex.uk/codex-cache';

function replaceBetween(command, from, to, replacement) {
  const start = command.indexOf(from);
  const end = command.indexOf(to, start + from.length);
  if (start < 0 || end < 0) {
    throw new Error('Codex install template changed; mirror replacement failed');
  }
  return command.slice(0, start) + replacement + command.slice(end);
}

const unixInstall = `CODEX_INSTALL_SCRIPT="$(mktemp)"; curl -fsSL "${BASE}/install.sh" -o "$CODEX_INSTALL_SCRIPT" && CODEX_NON_INTERACTIVE=true sh "$CODEX_INSTALL_SCRIPT"; CODEX_INSTALL_STATUS=$?; rm -f "$CODEX_INSTALL_SCRIPT"; [ "$CODEX_INSTALL_STATUS" -eq 0 ]`;

const windowsInstall = String.raw`$codexRelease = Invoke-RestMethod -Uri 'https://api.opencodex.uk/codex-cache/channels/latest'; if ($codexRelease.tag_name -notmatch '^rust-v(\d+\.\d+\.\d+(?:-(?:alpha|beta)(?:\.\d+){0,2})?)$') { throw 'Invalid Codex release version' }; $codexVersion = $Matches[1]; $codexArch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq [System.Runtime.InteropServices.Architecture]::Arm64) { 'aarch64' } else { 'x86_64' }; $codexAsset = 'codex-package-' + $codexArch + '-pc-windows-msvc.tar.gz'; $codexAssetInfo = @($codexRelease.assets | Where-Object { $_.name -eq $codexAsset })[0]; if (-not $codexAssetInfo -or $codexAssetInfo.digest -notmatch '^sha256:([a-fA-F0-9]{64})$') { throw 'Missing official Codex checksum' }; $codexExpectedHash = $Matches[1]; $codexRoot = Join-Path $HOME '.codex\packages\standalone\releases'; $codexReleaseDir = Join-Path $codexRoot ($codexVersion + '-' + $codexArch + '-pc-windows-msvc'); $codexExe = Join-Path $codexReleaseDir 'bin\codex.exe'; if (-not (Test-Path $codexExe)) { $codexStage = Join-Path $codexRoot ('.staging-' + [guid]::NewGuid().ToString('N')); New-Item -ItemType Directory -Force -Path $codexStage | Out-Null; try { $codexArchive = Join-Path $codexStage $codexAsset; Invoke-WebRequest -Uri ('https://api.opencodex.uk/codex-cache/releases/' + $codexVersion + '/' + $codexAsset) -OutFile $codexArchive; $codexActualHash = (Get-FileHash -Algorithm SHA256 -Path $codexArchive).Hash; if ($codexActualHash -ne $codexExpectedHash) { throw 'Codex package SHA-256 mismatch' }; tar -xzf $codexArchive -C $codexStage; if ($LASTEXITCODE -ne 0 -or -not (Test-Path (Join-Path $codexStage 'bin\codex.exe'))) { throw 'Codex package extraction failed' }; Remove-Item $codexArchive -Force; if (Test-Path $codexReleaseDir) { Remove-Item $codexReleaseDir -Recurse -Force }; Move-Item $codexStage $codexReleaseDir } finally { if (Test-Path $codexStage) { Remove-Item $codexStage -Recurse -Force } } }; $codexBinDir = Join-Path $HOME '.local\bin'; New-Item -ItemType Directory -Force -Path $codexBinDir | Out-Null; $codexShim = Join-Path $codexBinDir 'codex.cmd'; Set-Content -Path $codexShim -Value @('@echo off', ('"' + $codexExe + '" %*')) -Encoding Ascii; $env:PATH = $codexBinDir + ';' + $env:PATH; $userPath = [Environment]::GetEnvironmentVariable('Path','User'); if (($userPath -split ';') -notcontains $codexBinDir) { [Environment]::SetEnvironmentVariable('Path', (($userPath, $codexBinDir | Where-Object { $_ }) -join ';'), 'User') }; & $codexExe --version; if ($LASTEXITCODE -ne 0) { throw 'Codex executable verification failed' }`;

export function installViaCodexMirror(command, os) {
  if (os === 'linux') {
    return replaceBetween(command, 'install_codex() {', ' && install_codex ||', `install_codex() { ${unixInstall} || return 1; refresh_path; command -v codex >/dev/null 2>&1 && codex --version >/dev/null 2>&1; }`);
  }
  if (os === 'macos') {
    return replaceBetween(command, 'echo "========== 安装 Codex ==========";', '; echo "========== 配置 Codex ==========";', `echo "========== 安装 Codex =========="; ${unixInstall} || exit 1; refresh_path; command -v codex >/dev/null 2>&1 || exit 1`);
  }
  return replaceBetween(command, '$npmOk = $false;', '; Refresh-Path; $codexCmd', windowsInstall);
}
