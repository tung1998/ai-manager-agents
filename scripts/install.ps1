# Sets up agent-office on Windows through WSL2: installs WSL + Ubuntu when
# missing, clones this repo into the Ubuntu home and runs scripts/install.sh
# there (office runs on Linux; the browser on Windows opens localhost:2704).
#   powershell -ExecutionPolicy Bypass -File scripts\install.ps1          check, install, build
#   powershell -ExecutionPolicy Bypass -File scripts\install.ps1 -Run     ... then start the office
param([switch]$Run, [string]$Distro = 'Ubuntu')
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot\..").Path

function Say($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Wsl { & wsl.exe -d $Distro @args; if ($LASTEXITCODE -ne 0) { throw "wsl $args failed ($LASTEXITCODE)" } }

if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
  throw 'wsl.exe not found: WSL2 needs Windows 10 2004 or later, or Windows 11'
}

# wsl -l prints UTF-16, strip the NULs before comparing names
$distros = @(& wsl.exe -l -q 2>$null | ForEach-Object { ($_ -replace "`0", '').Trim() } | Where-Object { $_ })
if ($distros -notcontains $Distro) {
  $admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)
  if (-not $admin) { throw "WSL2 / $Distro is not installed yet: run PowerShell as administrator for this first step" }
  Say "Installing WSL2 + $Distro"
  & wsl.exe --install -d $Distro
  Write-Host ''
  Write-Host "Restart Windows if asked, open $Distro once to create your Linux user, then run this script again."
  exit 0
}

$linuxHome = (& wsl.exe -d $Distro -e sh -c 'echo $HOME').Trim()
$src = (& wsl.exe -d $Distro -e wslpath -a ($repo -replace '\\', '/')).Trim()
$dst = "$linuxHome/agent-office"

# Build inside the Linux filesystem: faster than /mnt/c and keeps LF line endings
& wsl.exe -d $Distro -e test -d "$dst/.git"
if ($LASTEXITCODE -eq 0) {
  Say "Updating $dst in $Distro"
  Wsl -e git -C $dst pull --ff-only
} else {
  Say "Cloning the repo into $dst in $Distro"
  Wsl -e git -c 'safe.directory=*' clone $src $dst
}

$flags = @()
if ($Run) { $flags += '--run' }
Wsl --cd $dst -e env SHELL=/bin/bash bash scripts/install.sh @flags

if (-not $Run) {
  Say "Start the office: wsl -d $Distro --cd $dst ./bin/office run   then open http://localhost:2704"
}
