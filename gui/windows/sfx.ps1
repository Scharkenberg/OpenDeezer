#requires -Version 5.1
# Build the Windows installer, PurpleDisc-<version>-windows-x64.exe, from the published app:
#   1. a copy of sfx\7zSD.sfx gets Purple Disc's icon, version details and asInvoker manifest
#      (sfx\stub.rc and sfx\stub.manifest compiled with the Windows SDK's rc.exe)
#   2. the publish folder and the terminal app (purpledisc-tui.exe, packed with UPX first) go into
#      a 7z archive (LZMA2 64 MB dictionary + BCJ2, sfx\7zr.exe)
#   3. stub + sfx\config.txt + archive are joined (the release workflow signs the result)
# The installer unpacks to a temp folder behind a progress window, then runs "PurpleDisc.exe --install"
# there, which asks for the desktop app, the terminal app or both.
#
# Usage:  gui\windows\sfx.ps1 -Tui <purpledisc-tui.exe> [-Publish <publish folder>] [-Out <folder>]
#   -Publish defaults to build.ps1's publish folder, -Out to the folder that holds it.
# UPX (https://upx.github.io) is upx.exe on PATH or the file UPX_EXE names (not UPX: upx reads that variable as options).
[CmdletBinding()]
param([Parameter(Mandatory)][string]$Tui, [string]$Publish = '', [string]$Out = '')
$ErrorActionPreference = 'Stop'

$here = $PSScriptRoot
$sfx = Join-Path $here 'sfx'
$csproj = [xml](Get-Content (Join-Path $here 'PurpleDisc.csproj'))
$version = $csproj.Project.PropertyGroup.Version
$framework = $csproj.Project.PropertyGroup.TargetFramework
if (-not $Publish) { $Publish = Join-Path $here "bin\Release\$framework\win-x64\publish" }
$Publish = (Resolve-Path $Publish).Path
if (-not (Test-Path (Join-Path $Publish 'PurpleDisc.exe'))) { throw "PurpleDisc.exe is not in $Publish (run build.ps1 first)." }
$Tui = (Resolve-Path $Tui).Path
if ((Split-Path $Tui -Leaf) -ne 'purpledisc-tui.exe') { throw "-Tui must name purpledisc-tui.exe (the installer looks for that name)." }
$upx = if ($env:UPX_EXE) { $env:UPX_EXE } else { (Get-Command upx -ErrorAction SilentlyContinue).Source }
if (-not $upx -or -not (Test-Path $upx)) { throw 'UPX not found: put upx.exe on PATH or set UPX_EXE to its path.' }
if (-not $Out) { $Out = Split-Path $Publish }
New-Item -ItemType Directory -Force -Path $Out | Out-Null
$exe = Join-Path (Resolve-Path $Out).Path "PurpleDisc-$version-windows-x64.exe"

# The 4-part number Win32 resources take ("4.0.0" -> 4.0.0.0; a "-beta" suffix is dropped).
$v = [version]($version.Split('-')[0])
$v4 = '{0}.{1}.{2}.{3}' -f $v.Major, $v.Minor, [Math]::Max(0, $v.Build), [Math]::Max(0, $v.Revision)

# rc.exe of the Windows SDK build tools the app restores (its PackageReference).
$toolsVersion = ($csproj.Project.ItemGroup.PackageReference | Where-Object { $_.Include -eq 'Microsoft.Windows.SDK.BuildTools' }).Version
$packages = if ($env:NUGET_PACKAGES) { $env:NUGET_PACKAGES } else { Join-Path $env:USERPROFILE '.nuget\packages' }
$rcExe = Resolve-Path (Join-Path $packages "microsoft.windows.sdk.buildtools\$toolsVersion\bin\*\x64\rc.exe") | Select-Object -First 1

if (-not ('SfxBrand' -as [type])) {
Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;

public static class SfxBrand
{
    [DllImport("kernel32", CharSet = CharSet.Unicode, SetLastError = true)] static extern IntPtr BeginUpdateResourceW(string file, bool deleteExisting);
    [DllImport("kernel32", SetLastError = true)] static extern bool UpdateResourceW(IntPtr update, IntPtr type, IntPtr name, ushort language, byte[] data, int size);
    [DllImport("kernel32", SetLastError = true)] static extern bool EndUpdateResourceW(IntPtr update, bool discard);
    [DllImport("kernel32", CharSet = CharSet.Unicode, SetLastError = true)] static extern IntPtr LoadLibraryExW(string file, IntPtr handle, uint flags);
    [DllImport("kernel32")] static extern bool FreeLibrary(IntPtr module);
    delegate bool EnumName(IntPtr module, IntPtr type, IntPtr name, IntPtr param);
    delegate bool EnumLang(IntPtr module, IntPtr type, IntPtr name, ushort lang, IntPtr param);
    [DllImport("kernel32")] static extern bool EnumResourceNamesW(IntPtr module, IntPtr type, EnumName callback, IntPtr param);
    [DllImport("kernel32")] static extern bool EnumResourceLanguagesW(IntPtr module, IntPtr type, IntPtr name, EnumLang callback, IntPtr param);

    // Every (type, name, language) of the stub's icons, icon groups, version and manifest.
    static List<Tuple<int, int, ushort>> Existing(string file)
    {
        var found = new List<Tuple<int, int, ushort>>();
        var module = LoadLibraryExW(file, IntPtr.Zero, 2); // LOAD_LIBRARY_AS_DATAFILE
        foreach (var type in new[] { 3, 14, 16, 24 })
            EnumResourceNamesW(module, (IntPtr)type, (m, t, n, p) =>
            {
                EnumResourceLanguagesW(m, t, n, (m2, t2, n2, l, p2) => { found.Add(Tuple.Create(type, (int)n2, l)); return true; }, IntPtr.Zero);
                return true;
            }, IntPtr.Zero);
        FreeLibrary(module);
        return found;
    }

    // Removes those, then adds every resource of the compiled .res.
    public static void Apply(string stub, string res)
    {
        var old = Existing(stub);
        var update = BeginUpdateResourceW(stub, false);
        if (update == IntPtr.Zero) throw new InvalidOperationException("BeginUpdateResource " + Marshal.GetLastWin32Error());
        foreach (var o in old) UpdateResourceW(update, (IntPtr)o.Item1, (IntPtr)o.Item2, o.Item3, null, 0);
        // .res entries: DataSize, HeaderSize, TYPE and NAME (ordinals here), the rest of the header, the data.
        var b = File.ReadAllBytes(res);
        int at = 0;
        while (at + 8 <= b.Length)
        {
            int dataSize = BitConverter.ToInt32(b, at), headerSize = BitConverter.ToInt32(b, at + 4);
            int type = BitConverter.ToUInt16(b, at + 10), name = BitConverter.ToUInt16(b, at + 14);
            ushort lang = BitConverter.ToUInt16(b, at + 22);
            if (dataSize > 0 && BitConverter.ToUInt16(b, at + 8) == 0xFFFF)
            {
                var data = new byte[dataSize];
                Array.Copy(b, at + headerSize, data, 0, dataSize);
                if (!UpdateResourceW(update, (IntPtr)type, (IntPtr)name, lang, data, dataSize)) throw new InvalidOperationException("UpdateResource " + Marshal.GetLastWin32Error());
            }
            at = (at + headerSize + dataSize + 3) & ~3;
        }
        if (!EndUpdateResourceW(update, false)) throw new InvalidOperationException("EndUpdateResource " + Marshal.GetLastWin32Error());
    }
}
'@
}

$work = Join-Path ([IO.Path]::GetTempPath()) "purpledisc-sfx-$PID"
New-Item -ItemType Directory -Force -Path $work | Out-Null
try {
  $utf8 = New-Object Text.UTF8Encoding $false
  Write-Host '==> [1/3] branding the installer stub' -ForegroundColor Cyan
  $manifest = Join-Path $work 'stub.manifest'
  [IO.File]::WriteAllText($manifest, [IO.File]::ReadAllText((Join-Path $sfx 'stub.manifest')).Replace('$VERSION4$', $v4), $utf8)
  $rc = [IO.File]::ReadAllText((Join-Path $sfx 'stub.rc'))
  $rc = $rc.Replace('$ICON$', (Join-Path $here 'purpledisc.ico').Replace('\', '\\')).Replace('$MANIFEST$', $manifest.Replace('\', '\\'))
  $rc = $rc.Replace('$VERSIONCOMMA$', $v4.Replace('.', ',')).Replace('$VERSION$', $version)
  [IO.File]::WriteAllText((Join-Path $work 'stub.rc'), $rc, $utf8)
  & $rcExe.Path /nologo /fo (Join-Path $work 'stub.res') (Join-Path $work 'stub.rc')
  if ($LASTEXITCODE -ne 0) { throw "rc failed ($LASTEXITCODE)" }
  $stub = Join-Path $work 'stub.sfx'
  Copy-Item (Join-Path $sfx '7zSD.sfx') $stub
  [SfxBrand]::Apply($stub, (Join-Path $work 'stub.res'))

  Write-Host '==> [2/3] packing the terminal app (UPX), compressing it with the publish folder' -ForegroundColor Cyan
  # The smallest UPX gives (measured on 4.0.0, built as build.ps1 does): --ultra-brute --lzma.
  # --strip-relocs fails on this exe; --compress-icons=3 would save 512 bytes but leave Explorer
  # without the icon. Unpacking costs about 0.2-0.4 s at every start.
  $packed = Join-Path $work 'purpledisc-tui.exe'
  Copy-Item $Tui $packed
  & $upx -q --ultra-brute --lzma $packed
  if ($LASTEXITCODE -ne 0) { throw "upx failed ($LASTEXITCODE)" }
  $archive = Join-Path $work 'app.7z'
  # A file given by its full path is stored under its name alone, beside PurpleDisc.exe.
  & (Join-Path $sfx '7zr.exe') a -t7z -mx=9 '-m0=lzma2:d=64m:fb=273' -mf=BCJ2 -ms=on -mmt=on -bso0 -bsp0 $archive (Join-Path $Publish '*') $packed
  if ($LASTEXITCODE -ne 0) { throw "7zr failed ($LASTEXITCODE)" }

  Write-Host '==> [3/3] joining stub, config and archive' -ForegroundColor Cyan
  # The stub reads its config as UTF-8 lines; the checkout may have either line ending.
  $config = Join-Path $work 'config.txt'
  [IO.File]::WriteAllText($config, ([IO.File]::ReadAllText((Join-Path $sfx 'config.txt')) -replace '\r?\n', "`r`n"), $utf8)
  $w = [IO.File]::Create($exe)
  try {
    foreach ($part in $stub, $config, $archive) { $r = [IO.File]::OpenRead($part); try { $r.CopyTo($w) } finally { $r.Dispose() } }
  } finally { $w.Dispose() }
} finally { Remove-Item -Recurse -Force $work }

Write-Host "==> done -> $exe ($((Get-Item $exe).Length.ToString('N0', [Globalization.CultureInfo]::InvariantCulture)) bytes)" -ForegroundColor Green
