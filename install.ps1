<#
.SYNOPSIS
  Feidex one-line installer for Windows (PowerShell).

.DESCRIPTION
  Downloads the release binary for your architecture, verifies its SHA-256
  against the release's sha256sums.txt, installs it to a bin directory, adds
  that directory to your user PATH, and prints the next steps. No build
  toolchain required.

  One-liner:
    irm https://raw.githubusercontent.com/yuhuan417/feidex/main/install.ps1 | iex

  With options (download first, then run):
    .\install.ps1 -Dev
    .\install.ps1 -Version v0.222.0 -BinDir 'C:\tools\feidex'

.PARAMETER Version  Install a specific version (default: latest release).
.PARAMETER Dev      Install the latest dev build (dev-latest prerelease).
.PARAMETER BinDir   Install directory (default: %LOCALAPPDATA%\Programs\feidex).
.PARAMETER Repo     GitHub repo to install from (default: yuhuan417/feidex).
.PARAMETER NoModifyPath  Do not add the bin dir to your user PATH.

  Environment overrides: FEIDEX_VERSION, FEIDEX_REPO, FEIDEX_BIN_DIR
#>
[CmdletBinding()]
param(
  [string]$Version = $env:FEIDEX_VERSION,
  [switch]$Dev,
  [string]$BinDir = $env:FEIDEX_BIN_DIR,
  [string]$Repo = $(if ($env:FEIDEX_REPO) { $env:FEIDEX_REPO } else { 'yuhuan417/feidex' }),
  [switch]$NoModifyPath
)

$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Info($m) { Write-Host "==> $m" -ForegroundColor Cyan }
function Ok($m)   { Write-Host "OK  $m" -ForegroundColor Green }
function Warn($m) { Write-Warning $m }
function Die($m)  { Write-Error $m; exit 1 }

# ---- platform detection -------------------------------------------------------
$archRaw = $env:PROCESSOR_ARCHITECTURE
switch ($archRaw) {
  'AMD64' { $goarch = 'amd64' }
  'ARM64' { $goarch = 'arm64' }
  'x86'   { Die 'unsupported architecture: x86 (need 64-bit Windows)' }
  default { Die "unsupported architecture: $archRaw" }
}
$asset = "feidex-windows-$goarch.exe"

# ---- resolve version ----------------------------------------------------------
if ($Dev) {
  $tag = 'dev-latest'
} elseif ($Version) {
  $tag = $Version
} else {
  Info "Resolving latest release of $Repo..."
  try {
    $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" `
      -Headers @{ 'User-Agent' = 'feidex-install' }
    $tag = $rel.tag_name
  } catch {
    Die "could not resolve latest release tag: $($_.Exception.Message)"
  }
  if (-not $tag) { Die 'could not resolve latest release tag' }
}

$base = "https://github.com/$Repo/releases/download/$tag"
Info "Installing feidex $tag (windows/$goarch) - asset $asset"

# ---- download + verify --------------------------------------------------------
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("feidex-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
  $binTmp = Join-Path $tmp 'feidex.exe'
  Info 'Downloading binary...'
  try {
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $binTmp -UseBasicParsing
  } catch {
    Die "download failed: $base/$asset (does tag $tag exist?)"
  }

  Info 'Verifying SHA-256...'
  $sumsPath = Join-Path $tmp 'sha256sums.txt'
  $haveSums = $false
  try {
    Invoke-WebRequest -Uri "$base/sha256sums.txt" -OutFile $sumsPath -UseBasicParsing
    $haveSums = $true
  } catch {
    Warn "sha256sums.txt not available for $tag; skipping verification"
  }
  # Verification failures below are intentionally fatal (not wrapped in catch).
  if ($haveSums) {
    $expected = $null
    foreach ($line in Get-Content $sumsPath) {
      $parts = $line -split '\s+', 2
      if ($parts.Count -eq 2 -and ($parts[1] -replace '.*/', '') -eq $asset) {
        $expected = $parts[0].ToLower(); break
      }
    }
    if (-not $expected) { Die "no checksum entry for $asset in sha256sums.txt" }
    $actual = (Get-FileHash -Algorithm SHA256 -Path $binTmp).Hash.ToLower()
    if ($actual -ne $expected) {
      Die "checksum mismatch for $asset`n  expected $expected`n  actual   $actual"
    }
    Ok 'checksum verified'
  }

  # ---- install ----------------------------------------------------------------
  if (-not $BinDir) { $BinDir = Join-Path $env:LOCALAPPDATA 'Programs\feidex' }
  New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
  $dest = Join-Path $BinDir 'feidex.exe'
  Copy-Item -Path $binTmp -Destination $dest -Force
  Ok "Installed to $dest"

  try { & $dest version | Select-Object -First 1 | ForEach-Object { Ok $_ } } catch {}

  # ---- PATH handling ----------------------------------------------------------
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $onPath = ($userPath -split ';') -contains $BinDir
  if (-not $onPath) {
    if (-not $NoModifyPath) {
      $newPath = if ([string]::IsNullOrEmpty($userPath)) { $BinDir } else { "$userPath;$BinDir" }
      [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
      $env:Path = "$env:Path;$BinDir"
      Ok "Added $BinDir to your user PATH (open a new terminal to pick it up)"
    } else {
      Warn "$BinDir is not on your PATH; add it manually."
    }
  }
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "feidex $tag is installed." -ForegroundColor Green
@"
Next steps:
  1. Bind a Feishu/Lark app and write config.toml:
       feidex feishu setup                                  # 飞书（国内版）
       feidex feishu bind --app ID:SECRET --domain lark     # Lark（国际版）
  2. Start it:
       feidex serve --config config.toml

  3. Or run it in the background as a daemon (Task Scheduler, no admin needed):
       feidex daemon install --config config.toml
       feidex daemon status

Note: /upgrade self-upgrade is Linux-only; to update on Windows, re-run this
installer. The daemon runs within your login session (stops at sign-out).

Docs: https://github.com/$Repo#readme
"@ | Write-Host
