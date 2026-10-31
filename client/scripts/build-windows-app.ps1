[CmdletBinding()]
param(
  [string]$Configuration = "Release",
  [string]$Version = "0.1.0",
  [switch]$SkipTests,
  [switch]$NoZip
)

$ErrorActionPreference = "Stop"
$client = Split-Path -Parent $PSScriptRoot
$repo = Split-Path -Parent $client
$buildRoot = Join-Path $client "build\windows"
$package = Join-Path $buildRoot "ESP32-Monitor"
$helperProject = Join-Path $client "tools\lhm-helper\LhmHelper.csproj"
$frontend = Join-Path $client "frontend"
$assets = Join-Path $client "internal\guiassets\assets"
$iss = Join-Path $client "packaging\windows\ESP32-Monitor.iss"

if ($Version -notmatch '^\d+\.\d+\.\d+') {
  throw "Version must look like 1.2.3 (got '$Version')"
}

function Resolve-Tool([string]$Name, [string[]]$Fallbacks) {
  $command = Get-Command $Name -ErrorAction SilentlyContinue
  if ($null -ne $command) { return $command.Source }
  foreach ($candidate in $Fallbacks) {
    if (Test-Path $candidate) { return $candidate }
  }
  return $null
}

$go = Resolve-Tool "go" @((Join-Path $env:ProgramFiles "Go\bin\go.exe"))
$npm = Resolve-Tool "npm" @((Join-Path $env:ProgramFiles "nodejs\npm.cmd"))
$dotnet = Resolve-Tool "dotnet" @((Join-Path $env:ProgramFiles "dotnet\dotnet.exe"))
$windres = Resolve-Tool "windres" @(
  "C:\msys64\ucrt64\bin\windres.exe",
  "C:\msys64\mingw64\bin\windres.exe",
  "C:\msys64\usr\bin\windres.exe"
)
if (-not $go) { throw "Go toolchain not found. Add Go to PATH." }
if (-not $npm) { throw "npm not found. Install Node.js or add it to PATH." }
if (-not $dotnet) { throw "A .NET SDK is required to build the net472 helper; the shipped helper only requires .NET Framework 4.7.2+." }
$env:PATH = "$(Split-Path $npm);$env:PATH"

Push-Location $client
try {
  Write-Host "[1/6] Building frontend"
  Push-Location $frontend
  try {
    if (-not (Test-Path node_modules)) {
      & $npm ci
      if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
    }
    & $npm run build
    if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }
  } finally { Pop-Location }

  if (Test-Path $assets) { Remove-Item $assets -Recurse -Force }
  New-Item -ItemType Directory -Force $assets | Out-Null
  Copy-Item (Join-Path $frontend "dist-web\*") $assets -Recurse -Force

  Write-Host "[2/6] Building LibreHardwareMonitor helper (net472)"
  # This is a framework-dependent net472 executable. Do not replace it with
  # dotnet publish or a self-contained modern .NET deployment.
  & $dotnet restore $helperProject --use-lock-file
  if ($LASTEXITCODE -ne 0) { throw "LHM helper restore failed" }
  & $dotnet build $helperProject --no-restore "/p:Configuration=$Configuration" /p:RestoreLockedMode=true /p:DebugType=None /p:DebugSymbols=false
  if ($LASTEXITCODE -ne 0) { throw "LHM helper build failed" }

  Write-Host "[3/6] Running Go tests and vet"
  if (-not $SkipTests) {
    & $go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed" }
    & $go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }
  }

  Write-Host "[4/6] Generating Windows icon and version resources"
  $parts = @($Version.Split('.'))
  while ($parts.Count -lt 4) { $parts += "0" }
  $rcVersion = ($parts[0..3] -join ",")
  $versionHeader = Join-Path $client "packaging\windows\resources\version.h"
  $clientSyso = Join-Path $client "cmd\monitor-client\rsrc_windows_amd64.syso"
  $agentSyso = Join-Path $client "cmd\monitor-agent\rsrc_windows_amd64.syso"
  if ($windres) {
    Set-Content -Path $versionHeader -Encoding ASCII -Value "#define APP_VERSION $rcVersion`r`n#define APP_VERSION_STRING `"$Version`""
    $env:PATH = "$(Split-Path $windres);$env:PATH"
    & $windres -I (Join-Path $client "assets\icons") -I (Join-Path $client "packaging\windows\resources") (Join-Path $client "packaging\windows\resources\monitor-client.rc") -O coff -o $clientSyso
    if ($LASTEXITCODE -ne 0) { throw "windres failed for monitor-client" }
    & $windres -I (Join-Path $client "assets\icons") -I (Join-Path $client "packaging\windows\resources") (Join-Path $client "packaging\windows\resources\monitor-agent.rc") -O coff -o $agentSyso
    if ($LASTEXITCODE -ne 0) { throw "windres failed for monitor-agent" }
  } elseif (-not (Test-Path $clientSyso) -or -not (Test-Path $agentSyso)) {
    throw "windres not found and prebuilt .syso resources are missing; install MSYS2/MinGW windres."
  } else {
    Write-Warning "windres not found; using existing .syso resources (version may differ from $Version)."
  }

  Write-Host "[5/6] Building monitor-client.exe and monitor-agent.exe"
  New-Item -ItemType Directory -Force $buildRoot | Out-Null
  if (Test-Path $package) { Remove-Item $package -Recurse -Force }
  New-Item -ItemType Directory -Force $package | Out-Null

  & $go build -trimpath -ldflags "-H=windowsgui -s -w" -o (Join-Path $package "monitor-client.exe") ./cmd/monitor-client
  if ($LASTEXITCODE -ne 0) { throw "monitor-client build failed" }
  & $go build -trimpath -ldflags "-s -w" -o (Join-Path $package "monitor-agent.exe") ./cmd/monitor-agent
  if ($LASTEXITCODE -ne 0) { throw "monitor-agent build failed" }

  $helperOut = Join-Path $client "tools\lhm-helper\bin\$Configuration\net472"
  $helperDest = Join-Path $package "helpers\lhm-helper"
  New-Item -ItemType Directory -Force $helperDest | Out-Null
  Copy-Item (Join-Path $helperOut "*") $helperDest -Recurse -Force
  Get-ChildItem $helperDest -Recurse -Include *.pdb | Remove-Item -Force

  Copy-Item (Join-Path $client "packaging\windows\README.txt") $package
  Copy-Item (Join-Path $repo "LICENSE") (Join-Path $package "LICENSE.txt")
  Copy-Item (Join-Path $client "packaging\THIRD_PARTY_NOTICES.txt") $package
  Copy-Item (Join-Path $client "third_party\go-keyring\LICENSE") (Join-Path $package "LICENSE-go-keyring.txt")
  Copy-Item (Join-Path $client "frontend\public\fonts\LICENSE-fusion-pixel.txt") (Join-Path $package "LICENSE-fusion-pixel.txt")
  Copy-Item (Join-Path $client "frontend\public\fonts\LICENSE-unscii\unscii.html") (Join-Path $package "LICENSE-unscii.html")
  Copy-Item (Join-Path $client "frontend\public\icons\LICENSE") (Join-Path $package "LICENSE-pixelarticons.txt")
  Copy-Item (Join-Path $client "assets\icons\app-icon.ico") $package
  Set-Content -Path (Join-Path $package "VERSION.txt") -Encoding ASCII -Value $Version

  Write-Host "[6/6] Packaging"
  if (-not $NoZip) {
    $zip = Join-Path $buildRoot "ESP32-Monitor-$Version-windows-amd64.zip"
    if (Test-Path $zip) { Remove-Item $zip -Force }
    Compress-Archive -Path $package -DestinationPath $zip -CompressionLevel Optimal
    Write-Host "Green package: $zip"
  }

  $iscc = Resolve-Tool "ISCC" @(
    (Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6\ISCC.exe"),
    (Join-Path $env:ProgramFiles "Inno Setup 6\ISCC.exe"),
    (Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 6\ISCC.exe")
  )
  if ($iscc) {
    & $iscc "/DMyAppVersion=$Version" $iss
    if ($LASTEXITCODE -ne 0) { throw "Inno Setup compilation failed" }
    Write-Host "Installer: $(Join-Path $buildRoot "ESP32-Monitor-$Version-windows-amd64-setup.exe")"
  } else {
    Write-Warning "Inno Setup (ISCC.exe) not found; skipped installer. Green zip is ready."
  }

  Write-Host "Built $package"
} finally { Pop-Location }
