# Builds and runs the harness; prints its results. The window stays off screen and takes no focus.
#   .\run.ps1                    all tests but the avatar
#   .\run.ps1 -Only bar,states   some tests: bar, states, tiles, rows, fills, stale, transport, chrome, layout, avatar
#   .\run.ps1 -Picture <url>     also loads that profile picture (network)
param([string[]]$Only = @(), [string]$Picture = '')

$here = $PSScriptRoot
$out = Join-Path $here 'out'
dotnet build (Join-Path $here 'Harness.csproj') -c Debug -p:Platform=x64 --nologo -v q 2>&1 |
    Select-String -Pattern ' error |Build succeeded' | Select-Object -Unique | ForEach-Object { $_.Line }
$exe = Get-ChildItem (Join-Path $here 'bin') -Recurse -Filter OdHarness.exe -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $exe) { Write-Error 'build failed'; return }

# A throwaway profile, so the real settings and login are never read.
New-Item -ItemType Directory -Force (Join-Path $out 'profile\opendeezer') | Out-Null
$result = Join-Path $out 'result.txt'
Remove-Item $result -ErrorAction SilentlyContinue
$saved = $env:APPDATA
try {
    $env:APPDATA = Join-Path $out 'profile'
    $env:HARNESS_OUT = $result
    $env:HARNESS_ONLY = $Only -join ','
    $env:HARNESS_PICTURE = $Picture
    # Its own working directory: Windows would otherwise find libdeezercore.dll in the caller's.
    $p = Start-Process $exe.FullName -PassThru -WorkingDirectory $out
    if (-not $p.WaitForExit(120000)) { $p.Kill(); 'timed out' }
}
finally {
    $env:APPDATA = $saved
    Remove-Item Env:HARNESS_OUT, Env:HARNESS_ONLY, Env:HARNESS_PICTURE -ErrorAction SilentlyContinue
}
Get-Content $result -ErrorAction SilentlyContinue
