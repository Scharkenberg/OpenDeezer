# Runs every check side by side, each in its own process, and prints the results lane by lane.
# Exits 1 when any lane reports a failure (CI runs it as is).
#   .\check.ps1          app build + tests, harness, go vet, go test
#   .\check.ps1 -Race    go test with the race detector (slower)
#   .\check.ps1 -Only app,go
param([switch]$Race, [string[]]$Only = @())

$here = $PSScriptRoot
$root = (Resolve-Path (Join-Path $here '..\..')).Path
$pkgs = './internal/...', './corelib', './sdk/...', './cmd/...', './mobile/...'

# The Go lanes run with a throwaway profile; the build cache stays the real one, or every run compiles cold.
$goSetup = {
    param($here, $root)
    . (Join-Path $here 'go-env.ps1') | Out-Null
    $env:GOCACHE = go env GOCACHE
    $tmp = Join-Path ([IO.Path]::GetTempPath()) "od-check-$PID"
    New-Item -ItemType Directory -Force $tmp | Out-Null
    $env:APPDATA = $tmp; $env:LOCALAPPDATA = $tmp
    Set-Location $root
}

$lanes = [ordered]@{
    app = {
        param($here)
        dotnet build (Join-Path $here 'OpenDeezer.csproj') -c Debug -p:Platform=x64 --nologo -v q 2>&1 |
            Select-String ' error | warning |Warning\(s\)|Error\(s\)' | Select-Object -Unique | ForEach-Object { $_.Line.Trim() }
        dotnet test (Join-Path $here '..\windows.tests\OpenDeezer.Tests.csproj') -c Debug -p:Platform=x64 --nologo -v q 2>&1 |
            Select-String ' error |Passed!|Failed' | ForEach-Object { $_.Line.Trim() }
    }
    harness = { param($here) & (Join-Path $here '..\windows.harness\run.ps1') }
    vet = [scriptblock]::Create("param(`$here, `$root, `$pkgs, `$race) . {$goSetup} `$here `$root; go vet @pkgs 2>&1; 'vet exit ' + `$LASTEXITCODE")
    go = [scriptblock]::Create("param(`$here, `$root, `$pkgs, `$race) . {$goSetup} `$here `$root; `$a = @('test'); if (`$race) { `$a += '-race' }; go @a @pkgs 2>&1 | Where-Object { `$_ -notmatch '^(ok|\?) ' }; 'test exit ' + `$LASTEXITCODE")
}

$jobs = foreach ($name in $lanes.Keys) {
    if ($Only -and $name -notin $Only) { continue }
    Start-Job -Name $name -ScriptBlock $lanes[$name] -ArgumentList $here, $root, $pkgs, $Race.IsPresent
}
# What a lane prints when it failed: a build error or warning, a failed test, a harness line that
# misses its mark, a non-zero exit.
$failure = ' error |[1-9]\d* (Warning|Error)\(s\)|Failed!|Failed: +[1-9]|WRONG|FAILED|FATAL|CLIPPED|DIFFER|REVERTED|SQUARE|HIDDEN|STALE|BLURRED|CUT OFF|STUCK|UNHANDLED|MADE AGAIN|APPLOG bad |timed out|build failed|(vet|test) exit [^0]'
$failed = $false
$clock = [Diagnostics.Stopwatch]::StartNew()
foreach ($job in $jobs) {
    $text = Receive-Job $job -Wait -AutoRemoveJob 2>&1
    "=== $($job.Name) ($([int]$clock.Elapsed.TotalSeconds) s)"
    $text
    if ($text | Where-Object { "$_" -cmatch $failure }) { $failed = $true }
}
if ($failed) { 'check failed'; exit 1 }
