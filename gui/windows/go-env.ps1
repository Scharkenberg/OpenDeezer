#requires -Version 5.1
# Go + cgo environment for building / testing the Purple Disc engine on Windows.
#
# Dot-source it in the CURRENT shell (it only changes this process's env, never
# machine/user settings, so other C/C++ projects don't inherit a global CC):
#
#     . gui\windows\go-env.ps1
#     go test ./...
#
# build.ps1 uses it too. What it sets up:
#   * go on PATH (adds %ProgramFiles%\Go\bin if this shell predates the install)
#   * the C toolchain cgo needs (corelib's C exports + the malgo/miniaudio audio
#     backend), in this order:
#       1. $env:CC if you set it yourself
#       2. Visual Studio's clang (component "C++ Clang Compiler for Windows",
#          target x86_64-pc-windows-msvc) -- Go 1.27+ links through it in MSVC
#          mode; the C runtime is linked statically, so the DLL needs no Visual
#          C++ redistributable (it imports only KERNEL32.dll)
#       3. MinGW-w64 gcc if one is on PATH
#   * $env:CGO_ENABLED = 1
#   * $env:GOAMD64 = v3 (AVX2, BMI2, FMA: CPUs from about 2013 on), and -march=x86-64-v3 for C
#   * $PurpleDiscExtLdFlags -- the -extldflags value build.ps1 passes to go build
#
# Go splits CC on spaces, so clang is put on PATH and CC is just "clang".

# (No $ErrorActionPreference change: this file is dot-sourced into the caller's shell.)

function Get-GoMinor {
    $v = (& go env GOVERSION) 2>$null              # e.g. "go1.27.4"
    if ($v -match '^go(\d+)\.(\d+)') { return [int]$Matches[1] * 100 + [int]$Matches[2] }
    return 0
}

# ---- go ------------------------------------------------------------------------
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    $goBin = Join-Path $env:ProgramFiles 'Go\bin'
    if (Test-Path (Join-Path $goBin 'go.exe')) { $env:Path = "$goBin;$env:Path" }
    else { throw 'go not found: install Go (https://go.dev/dl) or put it on PATH.' }
}
$goMinor = Get-GoMinor
# The DLL's floor is x86-64-v3 (GOAMD64 below): the C code gets the same, so dr_flac's SSE4.1 path builds.
$march = if ((& go env GOARCH) -eq 'amd64') { ' -march=x86-64-v3' } else { '' }

# ---- C toolchain ----------------------------------------------------------------
$PurpleDiscExtLdFlags = ''
$toolchain = ''

if ($env:CC) {
    $toolchain = "CC from environment ($env:CC)"
}
else {
    $vsClangBin = $null
    $vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
    if (Test-Path $vswhere) {
        $vs = & $vswhere -prerelease -latest -products * -requires Microsoft.VisualStudio.Component.VC.Llvm.Clang -property installationPath
        if ($vs) {
            $bin = Join-Path $vs 'VC\Tools\Llvm\x64\bin'
            if (Test-Path (Join-Path $bin 'clang.exe')) { $vsClangBin = $bin }
        }
    }

    if ($vsClangBin -and $goMinor -ge 127) {
        $env:Path = "$vsClangBin;$env:Path"
        $env:CC = 'clang'
        $target = (& clang --version) -join "`n"
        if ($target -notmatch 'Target:\s*\S+-msvc') { throw "VS clang does not report an -msvc target:`n$target" }
        # Static C runtime for every C object (runtime/cgo, corelib, miniaudio) and the link.
        if (-not $env:CGO_CFLAGS) { $env:CGO_CFLAGS = '-O2 -g -fms-runtime-lib=static' + $march }
        $PurpleDiscExtLdFlags = '-fms-runtime-lib=static'
        $toolchain = "Visual Studio clang ($vsClangBin)"
    }
    elseif (Get-Command x86_64-w64-mingw32-gcc -ErrorAction SilentlyContinue) {
        $env:CC = 'x86_64-w64-mingw32-gcc'
        if (-not $env:CGO_CFLAGS) { $env:CGO_CFLAGS = '-O2 -g' + $march }
        $PurpleDiscExtLdFlags = '-static'   # fold in libwinpthread/libgcc
        $toolchain = 'MinGW-w64 gcc'
    }
    elseif ($vsClangBin) {
        throw "Go $((& go env GOVERSION)) is older than 1.27; building with Visual Studio's clang needs Go 1.27+ (or set `$env:CC yourself)."
    }
    else {
        throw 'No C compiler for cgo: add the Visual Studio component "C++ Clang Compiler for Windows" (Microsoft.VisualStudio.Component.VC.Llvm.Clang).'
    }
}
if (-not $PurpleDiscExtLdFlags) {
    # CC chosen by the caller: match the static-runtime flags to its target
    # (compile and link must agree, or MSVC objects mix CRTs).
    if (((& $env:CC --version) -join "`n") -match '-msvc') {
        if (-not $env:CGO_CFLAGS) { $env:CGO_CFLAGS = '-O2 -g -fms-runtime-lib=static' + $march }
        $PurpleDiscExtLdFlags = '-fms-runtime-lib=static'
    }
    else { $PurpleDiscExtLdFlags = '-static' }
}

$env:CGO_ENABLED = '1'
$env:GOAMD64 = 'v3'
Write-Host "go-env: $((& go env GOVERSION)), cgo via $toolchain" -ForegroundColor DarkGray
