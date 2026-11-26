# Compiles purpledisc.rc (icon and version details) into rsrc_windows_amd64.syso, which go
# build links into purpledisc.exe on Windows. Run it after purpledisc.rc or the icon
# changes; it needs the Windows SDK's rc.exe and Visual Studio's cvtres.exe.
$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$rc = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin" -Recurse -Filter rc.exe |
    Where-Object FullName -Match '\\x64\\' | Sort-Object FullName | Select-Object -Last 1
$vs = & "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe" -latest -prerelease -property installationPath
$cvtres = Get-ChildItem "$vs\VC\Tools\MSVC" -Recurse -Filter cvtres.exe |
    Where-Object FullName -Match 'Hostx64\\x64' | Sort-Object FullName | Select-Object -Last 1
if (-not $rc -or -not $cvtres) { throw 'rc.exe (Windows SDK) or cvtres.exe (Visual Studio C++ tools) not found' }
$res = Join-Path ([IO.Path]::GetTempPath()) "purpledisc-$PID.res"
$syso = Join-Path $here 'rsrc_windows_amd64.syso'
try {
    & $rc.FullName /nologo /fo $res (Join-Path $here 'purpledisc.rc')
    if ($LASTEXITCODE) { throw 'rc.exe failed' }
    & $cvtres.FullName /nologo /machine:x64 "/out:$syso" $res
    if ($LASTEXITCODE) { throw 'cvtres.exe failed' }
}
finally { Remove-Item $res -ErrorAction SilentlyContinue }
"wrote $syso"
