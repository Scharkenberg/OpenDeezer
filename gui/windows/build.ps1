#requires -Version 5.1
# Build the Purple Disc Windows app end-to-end:
#   1. compile the Go engine to a C-ABI shared library (lib\libdeezercore.dll)
#   2. dotnet publish the C# WinUI 3 app (Native AOT, win-x64)
#   3. copy libdeezercore.dll next to PurpleDisc.exe in the publish dir
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
$root   = (Resolve-Path (Join-Path $here '..\..')).Path   # repo root: module github.com/Scharkenberg/PurpleDisc/v4
$libDir = Join-Path $here 'lib'
New-Item -ItemType Directory -Force -Path $libDir | Out-Null

Write-Host '==> [1/3] building Go c-shared DLL (lib\libdeezercore.dll), alongside step 2' -ForegroundColor Cyan
# cgo is required (c-shared). go-env.ps1 picks the C toolchain and the static-
# runtime link flag, so only libdeezercore.dll has to ship (no VC++ redist).
# The two builds share nothing until the copy in step 3, so they run side by side.
# corelib\default.pgo is the profile go build reads by default (the Makefile and the
# release copy it to cmd\purpledisc). It was made, after . gui\windows\go-env.ps1, from
# the benchmarks of the Go code that runs per block (resampler, gain, fades, mixing, EQ,
# stripe decryption); the decoders are C:
#   go test -run=^$ -bench="ResampleRead|New$" -benchtime=10s -cpuprofile=a.pprof ./internal/audio
#   go test -run=^$ -bench=StripeFeed -benchtime=10s -cpuprofile=d.pprof ./internal/deezer
#   go tool pprof -proto a.pprof d.pprof > corelib/default.pgo
$goBuild = Start-Job -ArgumentList $here, $root -ScriptBlock {
  param($here, $root)
  . (Join-Path $here 'go-env.ps1')
  $env:GOOS = 'windows'
  $env:GOARCH = 'amd64'
  Set-Location $root
  & go build -buildmode=c-shared -trimpath -ldflags "-s -w -extldflags=$PurpleDiscExtLdFlags" -o 'gui/windows/lib/libdeezercore.dll' ./corelib 2>&1
  if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" }
}

Write-Host '==> [2/3] publishing C# WinUI 3 app (dotnet publish)' -ForegroundColor Cyan
# Published straight to the folder that is kept: bin\<configuration>\<framework>\win-x64\publish.
$csproj = Join-Path $here 'PurpleDisc.csproj'
$framework = ([xml](Get-Content $csproj)).Project.PropertyGroup.TargetFramework
$bin = Join-Path $here 'bin'
$outDir = Join-Path $bin "$Configuration\$framework\win-x64\publish"
if (Test-Path $outDir) { Remove-Item -Recurse -Force $outDir }
& dotnet publish $csproj -c $Configuration -r win-x64 --self-contained -p:WindowsPackageType=None -o $outDir
$publishExit = $LASTEXITCODE
Receive-Job $goBuild -Wait -AutoRemoveJob
if ($publishExit -ne 0) { throw "dotnet publish failed ($publishExit)" }

Write-Host '==> [3/3] copying libdeezercore.dll into the publish dir' -ForegroundColor Cyan
# Without the compiled XAML and the resource index the app does not start (see the csproj);
# without the font it draws in Segoe UI.
foreach ($name in 'App.xbf', 'PurpleDisc.pri', 'Fonts\InterVariable.ttf') {
  if (-not (Test-Path (Join-Path $outDir $name))) { throw "$name is missing from the publish folder." }
}
Copy-Item (Join-Path $libDir 'libdeezercore.dll') $outDir -Force

# Only the published app stays: obj and the rest of bin go, each level beside the path to it.
Remove-Item -Recurse -Force (Join-Path $here 'obj')
for ($path = $outDir; $path -ne $bin; $path = Split-Path $path) {
  Get-ChildItem (Split-Path $path) -Force | Where-Object { $_.FullName -ne $path } | Remove-Item -Recurse -Force
}

Write-Host "==> done -> $(Join-Path $outDir 'PurpleDisc.exe')" -ForegroundColor Green
