<#
.SYNOPSIS
  Single-command install for the director on Windows.

    irm https://raw.githubusercontent.com/mkhanal/work-director/main/scripts/install.ps1 | iex

  Downloads the static wd.exe for this machine from the latest GitHub release,
  verifies its sha256 against the published checksums file, and puts it on PATH.
  The binary has no runtime dependencies; `wd doctor` afterwards reports which
  runner CLIs are detected.

  Pass a version to install something other than the latest:

    ./install.ps1 v1.2.0
#>
[CmdletBinding()]
param(
  [string]$Version = 'latest',
  # Where to put the binary. Default is the per-user bin directory, which needs
  # no administrator rights.
  [string]$InstallDir = "$env:LOCALAPPDATA\Programs\wd"
)

$ErrorActionPreference = 'Stop'
$repo = 'mkhanal/work-director'

function Get-Arch {
  switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    'x86'   { throw 'install: 32-bit Windows is not supported' }
    default { throw "install: unsupported architecture $env:PROCESSOR_ARCHITECTURE" }
  }
}

$arch = Get-Arch
$os = 'windows'

if ($Version -eq 'latest') {
  Write-Host "resolving the latest $repo release"
  $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
  $Version = $release.tag_name
}

$base = "https://github.com/$repo/releases/download/$Version"
$asset = "wd-$os-$arch.zip"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp -Force | Out-Null

try {
  $zip = Join-Path $tmp $asset
  $sums = Join-Path $tmp 'checksums.txt'
  Write-Host "downloading $asset ($Version)"
  Invoke-WebRequest "$base/$asset" -OutFile $zip
  Invoke-WebRequest "$base/checksums.txt" -OutFile $sums

  # The checksum is verified against the same file every asset is listed in, so
  # one download covers the whole release and a tampered tarball is caught before
  # anything is executed.
  $want = (Get-Content $sums | Where-Object { $_ -match "\s$([regex]::Escape($asset))\s*$" })
  if (-not $want) { throw "install: $asset is not listed in the release checksums" }
  $got = (Get-FileHash -Algorithm SHA256 $zip).Hash.ToLower()
  if ($got -ne ($want -split '\s+')[0]) {
    throw "install: $asset does not match its published sha256"
  }
  Write-Host "verified $asset"

  Expand-Archive -Path $zip -DestinationPath $tmp -Force
  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
  Copy-Item (Join-Path $tmp 'wd.exe') (Join-Path $InstallDir 'wd.exe') -Force
  Write-Host "installed $InstallDir\wd.exe"

  # PATH only helps the next shell, so it is written into the user environment
  # as well — the alternative is an install that works once and then not at all.
  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if ($userPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$InstallDir", 'User')
    Write-Host "added $InstallDir to your PATH (open a new terminal to pick it up)"
  }
  Write-Host 'next: wd doctor'
}
finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
