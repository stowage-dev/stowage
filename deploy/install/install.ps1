# stowage downloader for Windows PowerShell.
#
# If Docker is installed and the daemon is reachable, runs the published
# OCI image. Otherwise downloads stowage.exe into the current directory,
# verifies its SHA256 checksum, and runs it. Nothing is added to PATH or
# any system location.
#
#   irm https://stowage.dev/install.ps1 | iex
#   & ([scriptblock]::Create((irm https://stowage.dev/install.ps1))) serve --config my.yaml
#
# Environment overrides (set before piping):
#   $env:STOWAGE_VERSION       Tag to fetch (default: latest)
#   $env:STOWAGE_REPO          GitHub owner/name (default: stowage-dev/stowage)
#   $env:STOWAGE_RELEASE_BASE  Full base URL for binary downloads. Overrides REPO/VERSION.
#   $env:STOWAGE_NO_RUN        If '1', download and verify but do not run.
#   $env:STOWAGE_NO_DOCKER     If '1', skip Docker detection and use the binary.
#   $env:STOWAGE_DOCKER_IMAGE  Override the OCI image reference (default: ghcr.io/<repo>:<version>).

[CmdletBinding()]
param(
  [Parameter(ValueFromRemainingArguments = $true)]
  [string[]] $Arguments
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Invoke-WebRequest is dramatically slower with the progress bar enabled
# (it repaints on every chunk). Disable it for a clean, fast download.
$ProgressPreference = 'SilentlyContinue'

# Match the OCI image's default CMD so `irm | iex` with no args does
# something useful instead of printing the binary's help.
if (-not $Arguments) { $Arguments = @('quickstart') }

function Fail([string]$msg) {
  Write-Error "stowage-install: $msg"
  exit 1
}

function Info([string]$msg) {
  Write-Host "==> $msg"
}

$repo    = if ($env:STOWAGE_REPO)    { $env:STOWAGE_REPO }    else { 'stowage-dev/stowage' }
$version = if ($env:STOWAGE_VERSION) { $env:STOWAGE_VERSION } else { 'latest' }

if ($env:STOWAGE_RELEASE_BASE) {
  $base = $env:STOWAGE_RELEASE_BASE
} elseif ($version -eq 'latest') {
  $base = "https://github.com/$repo/releases/latest/download"
} else {
  $base = "https://github.com/$repo/releases/download/$version"
}

# Prefer the published OCI image if Docker is installed and the daemon is
# reachable. `docker info` confirms reachability — being on PATH isn't enough
# (Docker Desktop on Windows can be installed but stopped).
function Test-DockerAvailable {
  if ($env:STOWAGE_NO_DOCKER -eq '1') { return $false }
  if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { return $false }
  & docker info *>$null
  return ($LASTEXITCODE -eq 0)
}

if (Test-DockerAvailable) {
  if ($env:STOWAGE_DOCKER_IMAGE) {
    $image = $env:STOWAGE_DOCKER_IMAGE
  } else {
    $tag = if ($version -eq 'latest') { 'latest' } else { $version }
    $image = "ghcr.io/${repo}:${tag}"
  }
  Info "docker detected; running $image"
  Info "running: docker run --rm -i -p 8080:8080 -p 9000:9000 -p 9001:9001 -v stowage-data:/data $image $($Arguments -join ' ')"
  & docker run --rm -i `
    -p 8080:8080 -p 9000:9000 -p 9001:9001 `
    -v stowage-data:/data `
    $image @Arguments
  exit $LASTEXITCODE
}

# Architecture detection. We only ship windows-amd64 today; surface a clear
# error for arm64 hosts.
$archRaw = $env:PROCESSOR_ARCHITECTURE
switch -Regex ($archRaw) {
  '^(AMD64|x86_64)$' { $arch = 'amd64' }
  '^ARM64$'          { Fail "windows/arm64 binaries are not yet published" }
  default            { Fail "unsupported architecture: $archRaw" }
}

$asset    = "stowage-windows-$arch.exe"
$url      = "$base/$asset"
$sumsUrl  = "$base/SHA256SUMS"
$target   = Join-Path (Get-Location) 'stowage.exe'

# Use TLS 1.2+ on older Windows.
try { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 } catch { }

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("stowage-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
try {
  $tmpBin  = Join-Path $tmpDir $asset
  $tmpSums = Join-Path $tmpDir 'SHA256SUMS'

  Info "downloading $asset from $base"
  Invoke-WebRequest -Uri $url     -OutFile $tmpBin  -UseBasicParsing
  Invoke-WebRequest -Uri $sumsUrl -OutFile $tmpSums -UseBasicParsing

  $expected = $null
  foreach ($line in Get-Content -LiteralPath $tmpSums) {
    $parts = $line -split '\s+', 2
    if ($parts.Length -eq 2) {
      $name = $parts[1].Trim().TrimStart('*')
      if ($name -eq $asset) {
        $expected = $parts[0].ToLowerInvariant()
        break
      }
    }
  }
  if (-not $expected) {
    Fail "$asset not present in SHA256SUMS at $sumsUrl"
  }

  $actual = (Get-FileHash -LiteralPath $tmpBin -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($expected -ne $actual) {
    Fail "checksum mismatch for $asset (expected $expected, got $actual)"
  }
  Info 'checksum ok'

  Move-Item -LiteralPath $tmpBin -Destination $target -Force
  Info "downloaded to $target"
}
finally {
  Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
}

if ($env:STOWAGE_NO_RUN -eq '1') {
  Info 'STOWAGE_NO_RUN=1 set; skipping run'
  exit 0
}

Info "running: $target $($Arguments -join ' ')"
& $target @Arguments
exit $LASTEXITCODE
