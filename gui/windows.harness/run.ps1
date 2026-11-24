# Builds and runs the harness; prints its results. The window stays off screen and takes no focus.
#   .\run.ps1                    all tests but the avatar
#   .\run.ps1 -Only bar,states   some tests: bar, states, tiles, rows, fills, stale, transport, chrome, layout,
#                                cards, pane, rail, pages, offline, cost, memory, fixes, avatar
#   .\run.ps1 -Picture <url>     also loads that profile picture (network)
param([string[]]$Only = @(), [string]$Picture = '')

$here = $PSScriptRoot
$out = Join-Path $here 'out'
dotnet build (Join-Path $here 'Harness.csproj') -c Debug -p:Platform=x64 --nologo -v q 2>&1 |
    Select-String -Pattern ' error |Build succeeded' | Select-Object -Unique | ForEach-Object { $_.Line }
# A failed build must not run the last good one: its results would describe other code.
if ($LASTEXITCODE -ne 0) { Write-Error 'harness build failed'; return }
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
    if (-not $p.WaitForExit(240000)) { $p.Kill(); 'timed out' }
}
finally {
    $env:APPDATA = $saved
    Remove-Item Env:HARNESS_OUT, Env:HARNESS_ONLY, Env:HARNESS_PICTURE -ErrorAction SilentlyContinue
}
$lines = Get-Content $result -ErrorAction SilentlyContinue
$lines
# A run that died before its end, or exited with an error, is a failure for the check.
if ($lines -notcontains '=== done') { 'FATAL: the harness ended before "=== done"' }
if ($p -and $p.HasExited -and $p.ExitCode -ne 0) { "FATAL: the harness exited with code $($p.ExitCode)" }
