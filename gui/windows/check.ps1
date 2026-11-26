# Runs the checks, each in its own process (app, harness and go vet side by side, then go test),
# and prints the results lane by lane.
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
    $tmp = Join-Path ([IO.Path]::GetTempPath()) "pd-check-$PID"
    New-Item -ItemType Directory -Force $tmp | Out-Null
    $env:APPDATA = $tmp; $env:LOCALAPPDATA = $tmp
    # Go's telemetry would write into the throwaway profile and keep it busy past the run.
    $env:GOTELEMETRY = 'off'
    Set-Location $root
}

$lanes = [ordered]@{
    app = {
        param($here)
        dotnet build (Join-Path $here 'PurpleDisc.csproj') -c Debug -p:Platform=x64 --nologo -v q 2>&1 |
            Select-String ' error | warning |Warning\(s\)|Error\(s\)' | Select-Object -Unique | ForEach-Object { $_.Line.Trim() }
        dotnet test (Join-Path $here '..\windows.tests\PurpleDisc.Tests.csproj') -c Debug -p:Platform=x64 --nologo -v q 2>&1 |
            Select-String ' error |Passed!|Failed' | ForEach-Object { $_.Line.Trim() }
    }
    harness = { param($here) & (Join-Path $here '..\windows.harness\run.ps1') }
    vet = [scriptblock]::Create("param(`$here, `$root, `$pkgs, `$race) . {$goSetup} `$here `$root; try { go vet @pkgs 2>&1; 'vet exit ' + `$LASTEXITCODE } finally { Remove-Item -Recurse -Force `$tmp -ErrorAction SilentlyContinue }")
    go = [scriptblock]::Create("param(`$here, `$root, `$pkgs, `$race) . {$goSetup} `$here `$root; try { `$a = @('test'); if (`$race) { `$a += '-race' }; go @a @pkgs 2>&1 | Where-Object { `$_ -notmatch '^(ok|\?) ' }; 'test exit ' + `$LASTEXITCODE } finally { Remove-Item -Recurse -Force `$tmp -ErrorAction SilentlyContinue }")
}

$unknown = $Only | Where-Object { $_ -notin $lanes.Keys }
if ($unknown) { "unknown lane: $($unknown -join ', ') (lanes: $($lanes.Keys -join ', '))"; exit 1 }
$names = $lanes.Keys | Where-Object { -not $Only -or $_ -in $Only }
# Go tests run once the other lanes ended: beside them on a busy machine (8 logical CPUs, two
# emulators) a 50-track pin and the discovery replies ran past their tests' time.
$stages = @($names | Where-Object { $_ -ne 'go' }), @($names | Where-Object { $_ -eq 'go' })
# What a lane prints when it failed: a build error or warning, a failed test, a harness line that
# misses its mark, a non-zero exit.
$failure = ' error |[1-9]\d* (Warning|Error)\(s\)|Failed!|Failed: +[1-9]|WRONG|FAILED|FATAL|CLIPPED|DIFFER|REVERTED|SQUARE|HIDDEN|STALE|BLURRED|CUT OFF|STUCK|UNHANDLED|MADE AGAIN|APPLOG bad |timed out|build failed|(vet|test) exit [^0]'
# The line each lane ends with: a lane that lacks it, or whose job did not complete, stopped early.
$ends = @{ app = 'Passed!|Failed!'; harness = '^=== done'; vet = 'vet exit'; go = 'test exit' }
$failed = $false
$clock = [Diagnostics.Stopwatch]::StartNew()
foreach ($stage in $stages) {
    $jobs = foreach ($name in $stage) {
        Start-Job -Name $name -ScriptBlock $lanes[$name] -ArgumentList $here, $root, $pkgs, $Race.IsPresent
    }
    foreach ($job in $jobs) {
        $text = Receive-Job $job -Wait 2>&1
        $state = $job.State
        Remove-Job $job
        "=== $($job.Name) ($([int]$clock.Elapsed.TotalSeconds) s)"
        $text
        if ($text | Where-Object { "$_" -cmatch $failure }) { $failed = $true }
        if ($state -ne 'Completed' -or -not ($text | Where-Object { "$_" -cmatch $ends[$job.Name] })) {
            "$($job.Name) stopped early (job $state)"
            $failed = $true
        }
    }
}
if ($failed) { 'check failed'; exit 1 }
