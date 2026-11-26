# Shared by build.ps1 and release.ps1 (dot-sourced): the Android toolchain's environment and
# the engine's bind.
#
# Requirements: Go 1.27, JDK 21, the Android SDK (platform 37.2, build-tools 37.0.0) and NDK r30.
# ANDROID_HOME, ANDROID_NDK_HOME and JAVA_HOME are used when set, else the paths below.
# Exit-AndroidEnv restores the environment Enter-AndroidEnv found.

$AndroidDir = $PSScriptRoot
$RepoRoot = (Resolve-Path (Join-Path $AndroidDir '..\..')).Path
$EngineAar = Join-Path $AndroidDir 'app\libs\pdmobile.aar'

# CC, CGO_CFLAGS and GOAMD64 come from a shell that dot-sourced gui\windows\go-env.ps1 and
# would reach the NDK's clang: they are cleared for the bind.
$script:savedEnv = @{}
function Enter-AndroidEnv {
    foreach ($n in 'ANDROID_HOME', 'ANDROID_NDK_HOME', 'JAVA_HOME', 'CGO_LDFLAGS', 'GOFLAGS', 'PATH', 'CC', 'CGO_CFLAGS', 'GOAMD64') {
        $script:savedEnv[$n] = [Environment]::GetEnvironmentVariable($n)
    }
    foreach ($n in 'CC', 'CGO_CFLAGS', 'GOAMD64') { [Environment]::SetEnvironmentVariable($n, $null) }
    if (-not $env:ANDROID_HOME) { $env:ANDROID_HOME = 'C:\Program Files (x86)\Android\android-sdk' }
    if (-not $env:ANDROID_NDK_HOME) { $env:ANDROID_NDK_HOME = Join-Path $env:ANDROID_HOME 'ndk\30.0.16248370' }
    # gomobile passes the NDK's clang to Go as CC, and Go splits CC on spaces: the NDK
    # path goes in its 8.3 form ("C:\Program Files (x86)" has spaces).
    $env:ANDROID_NDK_HOME = (New-Object -ComObject Scripting.FileSystemObject).GetFolder($env:ANDROID_NDK_HOME).ShortPath
    if (-not $env:JAVA_HOME) { $env:JAVA_HOME = 'C:\Program Files (x86)\Android\openjdk\jdk-21.0.8' }
}

function Exit-AndroidEnv {
    foreach ($n in $script:savedEnv.Keys) { [Environment]::SetEnvironmentVariable($n, $script:savedEnv[$n]) }
}

# The AAR is older than a file or folder of the engine (sources and what it embeds; a deleted
# file moves its folder's time), than go.mod or go.sum, or than this file (the bind's flags).
function Test-EngineStale {
    if (-not (Test-Path $EngineAar)) { return $true }
    $built = (Get-Item $EngineAar).LastWriteTimeUtc
    $roots = 'mobile', 'internal', 'third_party' | ForEach-Object { Join-Path $RepoRoot $_ }
    $sources = @(Get-Item $roots) + @(Get-ChildItem $roots -Recurse) +
        @(Get-Item (Join-Path $RepoRoot 'go.mod'), (Join-Path $RepoRoot 'go.sum'), (Join-Path $AndroidDir 'env.ps1'))
    return [bool]($sources | Where-Object { $_.LastWriteTimeUtc -gt $built } | Select-Object -First 1)
}

# Binds ./mobile into app\libs\pdmobile.aar for 64-bit ARM phones and x86_64 emulators,
# with 16 KB aligned ELF segments (Android 15+ devices with 16 KB pages require them).
function Invoke-EngineBind {
    $tools = Join-Path ([IO.Path]::GetTempPath()) "purpledisc-mobile-tools-$PID"
    Push-Location $RepoRoot
    try {
        Write-Host '==> Building gomobile and gobind' -ForegroundColor Cyan
        & go build -mod=readonly -trimpath -o (Join-Path $tools 'gomobile.exe') golang.org/x/mobile/cmd/gomobile
        if ($LASTEXITCODE) { throw "go build gomobile failed ($LASTEXITCODE)" }
        & go build -mod=readonly -trimpath -o (Join-Path $tools 'gobind.exe') golang.org/x/mobile/cmd/gobind
        if ($LASTEXITCODE) { throw "go build gobind failed ($LASTEXITCODE)" }
        $env:PATH = "$tools;$env:PATH"

        Write-Host '==> Binding ./mobile -> app\libs\pdmobile.aar' -ForegroundColor Cyan
        New-Item -ItemType Directory -Force (Split-Path $EngineAar) | Out-Null
        # Packed relocations (Android 6+) and the engine's CPU profile (corelib\default.pgo).
        $env:CGO_LDFLAGS = '-Wl,-z,max-page-size=16384 -Wl,--pack-dyn-relocs=android'
        $env:GOFLAGS = "-pgo=$(Join-Path $RepoRoot 'corelib\default.pgo')"
        & gomobile bind -trimpath -ldflags="-s -w" '-target=android/arm64,android/amd64' -androidapi 24 -o gui/android/app/libs/pdmobile.aar ./mobile
        if ($LASTEXITCODE) { throw "gomobile bind failed ($LASTEXITCODE)" }
    }
    finally {
        Pop-Location
        Remove-Item -Recurse -Force $tools -ErrorAction SilentlyContinue
    }
}
