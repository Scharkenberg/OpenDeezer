#requires -Version 7
# Builds signed release APKs of the Purple Disc Android app, one per CPU type:
#   1. binds the Go engine when a source changed (env.ps1),
#   2. for each CPU type, assembles the phone flavor's release APK (-Tv: also the Android TV one)
#      with one Gradle call, signs it with apksigner (v2 and v3; Android 7, the oldest supported,
#      reads v2) and verifies it,
#   3. writes them to gui\android\dist (replacing the APKs there) and deletes the build folders
#      once at the end.
#
# Usage:  gui\android\release.ps1 [-Abi arm64-v8a[,x86_64]] [-Tv] [-Keystore <file>]
# The key: -Keystore (password in ANDROID_KEYSTORE_PASSWORD), else ANDROID_KEYSTORE_FILE, else
# the SDK's debug keystore.
param(
    [ValidateSet('arm64-v8a', 'x86_64')][string[]]$Abi = @('arm64-v8a'),
    [switch]$Tv,
    [string]$Keystore
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'env.ps1')

try {
    Enter-AndroidEnv
    if (Test-EngineStale) { Invoke-EngineBind }

    $ks = if ($Keystore) { $Keystore } elseif ($env:ANDROID_KEYSTORE_FILE) { $env:ANDROID_KEYSTORE_FILE } else { $null }
    $sign = if ($ks) {
        @('--ks', $ks, '--ks-pass', 'env:ANDROID_KEYSTORE_PASSWORD')
    }
    else {
        @('--ks', (Join-Path $env:USERPROFILE '.android\debug.keystore'), '--ks-pass', 'pass:android', '--ks-key-alias', 'androiddebugkey')
    }
    $apksigner = Join-Path $env:ANDROID_HOME 'build-tools\37.0.0\apksigner.bat'
    $version = [regex]::Match((Get-Content (Join-Path $AndroidDir 'app\build.gradle.kts') -Raw), 'versionName = "([^"]+)"').Groups[1].Value
    $dist = Join-Path $AndroidDir 'dist'
    New-Item -ItemType Directory -Force $dist | Out-Null
    Get-ChildItem $dist -Filter *.apk | Remove-Item
    $tasks = @('assembleMobileRelease') + $(if ($Tv) { 'assembleTvRelease' } else { @() })
    $flavors = @('mobile') + $(if ($Tv) { 'tv' } else { @() })

    $out = foreach ($a in $Abi) {
        Write-Host "==> Assembling the release APK ($a)" -ForegroundColor Cyan
        # With ANDROID_KEYSTORE_FILE set Gradle signs the APK itself; apksigner signs it below.
        $ksFile = $env:ANDROID_KEYSTORE_FILE
        $env:ANDROID_KEYSTORE_FILE = $null
        Push-Location $AndroidDir
        try {
            # Tools' output goes to the host: only APK paths are collected in $out.
            & .\gradlew.bat --no-daemon @tasks "-Pabi=$a" | Out-Host
            if ($LASTEXITCODE) { throw "gradle failed ($LASTEXITCODE)" }
        }
        finally {
            Pop-Location
            $env:ANDROID_KEYSTORE_FILE = $ksFile
        }

        # Signed before the next CPU type's build overwrites the unsigned APK.
        foreach ($f in $flavors) {
            $unsigned = Join-Path $AndroidDir "app\build\outputs\apk\$f\release\app-$f-release-unsigned.apk"
            $name = if ($f -eq 'tv') { "PurpleDisc-$version-tv-$a.apk" } else { "PurpleDisc-$version-$a.apk" }
            $apk = Join-Path $dist $name
            Write-Host "==> Signing $name" -ForegroundColor Cyan
            & $apksigner sign @sign --v1-signing-enabled false --out $apk $unsigned | Out-Host
            if ($LASTEXITCODE) { throw "apksigner sign failed ($LASTEXITCODE)" }
            & $apksigner verify $apk | Out-Host
            if ($LASTEXITCODE) { throw "apksigner verify failed ($LASTEXITCODE)" }
            Remove-Item "$apk.idsig" -ErrorAction SilentlyContinue
            $apk
        }
    }

    Write-Host '==> Cleaning the build folders' -ForegroundColor Cyan
    foreach ($d in 'app\build', 'build', '.kotlin', '.gradle') { Remove-Item -Recurse -Force (Join-Path $AndroidDir $d) -ErrorAction SilentlyContinue }

    foreach ($apk in $out) { Write-Host ("==> {0} ({1:N1} MB)" -f $apk, ((Get-Item $apk).Length / 1MB)) -ForegroundColor Green }
    Write-Host 'Android refuses to install it over a copy signed with another key (the store''s or upstream''s) until that copy is uninstalled.'
}
finally { Exit-AndroidEnv }
