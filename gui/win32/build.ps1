# Build the classic Win32 GUI as a 32-bit Windows executable. It is not packed: a UPX-packed
# exe unpacks itself into private memory (measured: 47 MB idle instead of 5 MB).
# gui\win32\dist receives the exe and nothing else.
[CmdletBinding()]
param([string]$Out = (Join-Path $PSScriptRoot 'dist\PurpleDisc-win32-x86.exe'))
$ErrorActionPreference = 'Stop'

$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$outPath = [IO.Path]::GetFullPath($(if ([IO.Path]::IsPathRooted($Out)) { $Out } else { Join-Path $root $Out }))
$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
$vs = & $vswhere -prerelease -latest -products * -requires Microsoft.VisualStudio.Component.VC.Llvm.Clang -property installationPath
if (-not $vs) { throw 'Install the Visual Studio C++ Clang Compiler component.' }
$sdk = Get-ChildItem (Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin') -Directory | Sort-Object Name -Descending |
  Where-Object { Test-Path (Join-Path $_.FullName 'x64\rc.exe') } | Select-Object -First 1
if (-not $sdk) { throw 'Windows 10 SDK resource compiler not found.' }
$msvc = Get-ChildItem (Join-Path $vs 'VC\Tools\MSVC') -Directory | Sort-Object Name -Descending | Select-Object -First 1
$cvtres = Join-Path $msvc.FullName 'bin\Hostx64\x86\cvtres.exe'

# The version resource takes the app version from internal/version.
$v = [regex]::Match((Get-Content -Raw (Join-Path $root 'internal\version\version.go')), 'Number = "(\d+\.\d+\.\d+)"').Groups[1].Value
if (-not $v) { throw 'internal\version\version.go: version not found.' }
$resource = Join-Path ([IO.Path]::GetTempPath()) "purpledisc-win32-$PID.res"
$syso = Join-Path $PSScriptRoot 'rsrc_windows_386.syso'
$names = 'Path', 'GOOS', 'GOARCH', 'GO386', 'CGO_ENABLED', 'CC', 'CGO_CFLAGS', 'CGO_LDFLAGS'
$old = @{}
foreach ($name in $names) { $old[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
try {
  Push-Location $PSScriptRoot
  try { & (Join-Path $sdk.FullName 'x64\rc.exe') /nologo /d "PD_VERSION=$($v -replace '\.', ','),0" /d "PD_VERSION_TEXT=$v" /fo $resource app.rc }
  finally { Pop-Location }
  if ($LASTEXITCODE -ne 0) { throw "Resource compilation failed ($LASTEXITCODE)" }
  & $cvtres /nologo /MACHINE:X86 "/OUT:$syso" $resource
  if ($LASTEXITCODE -ne 0) { throw "Resource conversion failed ($LASTEXITCODE)" }
  $env:Path = "$(Join-Path $vs 'VC\Tools\Llvm\x64\bin');$env:Path"
  $env:GOOS = 'windows'
  $env:GOARCH = '386'
  $env:GO386 = 'sse2'
  $env:CGO_ENABLED = '1'
  $env:CC = 'clang --target=i686-pc-windows-msvc'
  $env:CGO_CFLAGS = '-O2 -fms-runtime-lib=static'
  # Go's 386 objects carry no SafeSEH table; the CRT's console entry with a GUI subsystem (no console window).
  $env:CGO_LDFLAGS = '-Wl,/SAFESEH:NO -Wl,/SUBSYSTEM:WINDOWS -Wl,/ENTRY:mainCRTStartup'
  New-Item -ItemType Directory -Force -Path (Split-Path -Parent $outPath) | Out-Null
  Push-Location $root
  try { & go build -trimpath '-pgo=corelib/default.pgo' -ldflags '-s -w -H=windowsgui -extldflags=-fms-runtime-lib=static' -o $outPath ./gui/win32 }
  finally { Pop-Location }
  if ($LASTEXITCODE -ne 0) { throw "Win32 build failed ($LASTEXITCODE)" }
} finally {
  Remove-Item -LiteralPath $syso, $resource -Force -ErrorAction SilentlyContinue
  foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $old[$name], 'Process') }
}
