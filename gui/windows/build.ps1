#requires -Version 5.1
# Build the OpenDeezer Windows app end-to-end:
#   1. compile the Go engine to a C-ABI shared library (lib\libdeezercore.dll)
#   2. dotnet publish the C# WinUI 3 app (self-contained, win-x64)
#   3. copy libdeezercore.dll next to OpenDeezer.exe in the publish dir
#
# P/Invoke loads libdeezercore.dll by name at runtime, so no import lib is needed;
# `dotnet` drives the whole .NET build. The C toolchain for cgo comes from
# go-env.ps1: Visual Studio's clang (Go 1.27+), else MinGW-w64 gcc (CI).
#
# Usage:  gui\windows\build.ps1
# Prereqs: .NET 11 SDK and Go, plus either Go 1.27+ with the Visual Studio component
# "C++ Clang Compiler for Windows" (no MinGW needed), or MinGW-w64 gcc on PATH.
[CmdletBinding()]
param([string]$Configuration = 'Release')
$ErrorActionPreference = 'Stop'

$here   = $PSScriptRoot
$root   = (Resolve-Path (Join-Path $here '..\..')).Path   # repo root: module github.com/Cycl0o0/OpenDeezer
$libDir = Join-Path $here 'lib'
New-Item -ItemType Directory -Force -Path $libDir | Out-Null

Write-Host '==> [1/3] building Go c-shared DLL (lib\libdeezercore.dll), alongside step 2' -ForegroundColor Cyan
# cgo is required (c-shared). go-env.ps1 picks the C toolchain and the static-
# runtime link flag, so only libdeezercore.dll has to ship (no VC++ redist).
# The two builds share nothing until the copy in step 3, so they run side by side.
$goBuild = Start-Job -ArgumentList $here, $root -ScriptBlock {
  param($here, $root)
  . (Join-Path $here 'go-env.ps1')
  $env:GOOS = 'windows'
  $env:GOARCH = 'amd64'
  Set-Location $root
  & go build -buildmode=c-shared -trimpath -ldflags "-s -w -extldflags=$OpenDeezerExtLdFlags" -o 'gui/windows/lib/libdeezercore.dll' ./corelib 2>&1
  if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" }
}

Write-Host '==> [2/3] publishing C# WinUI 3 app (dotnet publish)' -ForegroundColor Cyan
& dotnet publish (Join-Path $here 'OpenDeezer.csproj') -c $Configuration -r win-x64 --self-contained -p:WindowsPackageType=None
$publishExit = $LASTEXITCODE
Receive-Job $goBuild -Wait -AutoRemoveJob
if ($publishExit -ne 0) { throw "dotnet publish failed ($publishExit)" }

Write-Host '==> [3/3] copying libdeezercore.dll into the publish dir' -ForegroundColor Cyan
# The publish dir: the folder of OpenDeezer.exe under bin.
$exe = Get-ChildItem -Path (Join-Path $here 'bin') -Filter 'OpenDeezer.exe' -Recurse -File -ErrorAction SilentlyContinue |
       Where-Object { $_.FullName -match '\\publish\\' } |
       Select-Object -First 1
if (-not $exe) {
  $exe = Get-ChildItem -Path (Join-Path $here 'bin') -Filter 'OpenDeezer.exe' -Recurse -File -ErrorAction SilentlyContinue |
         Select-Object -First 1
}
if (-not $exe) { throw 'OpenDeezer.exe not found under bin after publish.' }
$outDir = $exe.Directory.FullName
Copy-Item (Join-Path $libDir 'libdeezercore.dll') $outDir -Force

Write-Host "==> done -> $(Join-Path $outDir 'OpenDeezer.exe')" -ForegroundColor Green
