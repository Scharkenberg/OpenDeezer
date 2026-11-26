#requires -Version 7
# Builds the Purple Disc Android app on Windows for development (build.sh's steps):
#   1. binds the Go engine (./mobile) into app\libs\pdmobile.aar when a source changed,
#   2. assembles the debug APK of the phone flavor (-Tv: also the Android TV one) with Gradle.
#
# Usage:  gui\android\build.ps1 [-Tv]
# Requirements and environment: env.ps1. Release builds: release.ps1.
param([switch]$Tv)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'env.ps1')

try {
    Enter-AndroidEnv
    if (Test-EngineStale) { Invoke-EngineBind }

    Write-Host '==> Assembling debug APKs' -ForegroundColor Cyan
    $tasks = @('assembleMobileDebug') + $(if ($Tv) { 'assembleTvDebug' } else { @() })
    Push-Location $AndroidDir
    try {
        & .\gradlew.bat --no-daemon @tasks
        if ($LASTEXITCODE) { throw "gradle failed ($LASTEXITCODE)" }
    }
    finally { Pop-Location }

    Write-Host '==> Done. APKs at:' -ForegroundColor Green
    Write-Host "    $AndroidDir\app\build\outputs\apk\mobile\debug\app-mobile-debug.apk  (phone/tablet)"
    if ($Tv) { Write-Host "    $AndroidDir\app\build\outputs\apk\tv\debug\app-tv-debug.apk          (Android TV)" }
}
finally { Exit-AndroidEnv }
