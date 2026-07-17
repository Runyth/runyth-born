$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$ProjectDirectory = Split-Path -Parent $PSScriptRoot
$OutputDirectory = Join-Path $ProjectDirectory "dist"
$Version = if ($env:VERSION) { $env:VERSION } else { "dev" }

function Build-Target {
  param(
    [Parameter(Mandatory = $true)][string]$GoOS,
    [Parameter(Mandatory = $true)][string]$GoArch,
    [Parameter(Mandatory = $true)][string]$OutputName
  )

  Write-Host "Building $GoOS/$GoArch -> $OutputName"
  $env:CGO_ENABLED = "0"
  $env:GOOS = $GoOS
  $env:GOARCH = $GoArch
  go build `
    -trimpath `
    -ldflags "-s -w -X main.version=$Version" `
    -o (Join-Path $OutputDirectory $OutputName) `
    ./cmd/template-wizard
}

Set-Location $ProjectDirectory
Remove-Item -Recurse -Force $OutputDirectory -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $OutputDirectory | Out-Null

go test ./...

Build-Target -GoOS "linux" -GoArch "amd64" -OutputName "template-wizard-linux-amd64"
Build-Target -GoOS "windows" -GoArch "amd64" -OutputName "template-wizard-windows-amd64.exe"
Build-Target -GoOS "darwin" -GoArch "amd64" -OutputName "template-wizard-macos-amd64"
Build-Target -GoOS "darwin" -GoArch "arm64" -OutputName "template-wizard-macos-arm64"

Write-Host "Built release files in $OutputDirectory"
