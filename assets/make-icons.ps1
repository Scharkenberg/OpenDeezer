# Draws the Purple Disc mark and writes every raster icon in the repository.
# The mark lives in a 120-unit box: an open ring with a dot in its gap, a groove arc
# and a disc with a play triangle cut out. Sizes up to 24 px use a cut without the groove.
# Run: pwsh -NoProfile -File assets/make-icons.ps1

Add-Type -AssemblyName System.Drawing
$root = Split-Path $PSScriptRoot -Parent
$purple = [Drawing.Color]::FromArgb(0x8B, 0x2C, 0xF5)
$lilac = [Drawing.Color]::FromArgb(0x9A, 0x45, 0xFF)

function White([double]$alpha = 1) { [Drawing.Color]::FromArgb([int](255 * $alpha), 255, 255, 255) }

function Arc($g, [double]$r, [double]$width, [double]$start, [double]$sweep, [double]$alpha) {
    $pen = [Drawing.Pen]::new((White $alpha), $width)
    $pen.StartCap = $pen.EndCap = [Drawing.Drawing2D.LineCap]::Round
    $g.DrawArc($pen, [float](60 - $r), [float](60 - $r), [float](2 * $r), [float](2 * $r), [float]$start, [float]$sweep)
    $pen.Dispose()
}

# A quadratic corner as the cubic GDI+ wants.
function Corner($path, [double[]]$from, [double[]]$via, [double[]]$to) {
    $c1 = ($from[0] + 2 / 3 * ($via[0] - $from[0])), ($from[1] + 2 / 3 * ($via[1] - $from[1]))
    $c2 = ($to[0] + 2 / 3 * ($via[0] - $to[0])), ($to[1] + 2 / 3 * ($via[1] - $to[1]))
    $path.AddBezier([float]$from[0], [float]$from[1], [float]$c1[0], [float]$c1[1], [float]$c2[0], [float]$c2[1], [float]$to[0], [float]$to[1])
}

function Mark($g, [bool]$small) {
    if ($small) {
        Arc $g 41 11 -15 310 1
        $dot = 91.41, 33.65, 6, 0.7
        $disc = 23
    } else {
        Arc $g 42 9 -15 310 1
        Arc $g 29.5 5 80 135 0.65
        $dot = 92.17, 33, 5, 0.65
        $disc = 21
    }
    $brush = [Drawing.SolidBrush]::new((White $dot[3]))
    $g.FillEllipse($brush, [float]($dot[0] - $dot[2]), [float]($dot[1] - $dot[2]), [float](2 * $dot[2]), [float](2 * $dot[2]))
    $brush.Dispose()

    $path = [Drawing.Drawing2D.GraphicsPath]::new([Drawing.Drawing2D.FillMode]::Alternate)
    $path.AddEllipse([float](60 - $disc), [float](60 - $disc), [float](2 * $disc), [float](2 * $disc))
    $path.StartFigure()
    $path.AddLine(57.43, 50.06, 70.57, 57.94)
    Corner $path (70.57, 57.94) (74, 60) (70.57, 62.06)
    $path.AddLine(70.57, 62.06, 57.43, 69.94)
    Corner $path (57.43, 69.94) (54, 72) (54, 68)
    $path.AddLine(54, 68, 54, 52)
    Corner $path (54, 52) (54, 48) (57.43, 50.06)
    $path.CloseFigure()
    $white = [Drawing.SolidBrush]::new((White))
    $g.FillPath($white, $path)
    $white.Dispose(); $path.Dispose()
}

function Fill($g, $color, [scriptblock]$shape) {
    $brush = [Drawing.SolidBrush]::new($color)
    & $shape $brush
    $brush.Dispose()
}

# Shape: tile (rounded square), round, bleed (full square), glyph (mark only).
function Icon([int]$size, [string]$shape = 'tile', [double]$inset = 0) {
    $bmp = [Drawing.Bitmap]::new($size, $size, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = 'AntiAlias'; $g.PixelOffsetMode = 'HighQuality'
    $k = $size * (1 - 2 * $inset) / 120
    $g.TranslateTransform([float]($size * $inset), [float]($size * $inset)); $g.ScaleTransform([float]$k, [float]$k)
    switch ($shape) {
        'tile' {
            $r = [Drawing.Drawing2D.GraphicsPath]::new()
            foreach ($c in (0, 0, 180), (64, 0, 270), (64, 64, 0), (0, 64, 90)) { $r.AddArc([float]$c[0], [float]$c[1], 56, 56, [float]$c[2], 90) }
            $r.CloseFigure()
            Fill $g $purple { param($b) $g.FillPath($b, $r) }
            $r.Dispose()
        }
        'round' { Fill $g $purple { param($b) $g.FillEllipse($b, 0, 0, 120, 120) } }
        'bleed' { Fill $g $purple { param($b) $g.FillRectangle($b, -1, -1, 122, 122) } }
    }
    if ($shape -ne 'glyph') { Fill $g $lilac { param($b) $g.FillEllipse($b, 6, 6, 108, 108) } }
    if ($shape -in 'round', 'bleed') { $g.TranslateTransform(12, 12); $g.ScaleTransform(0.8, 0.8) }
    Mark $g ($size -le 24)
    $g.Dispose()
    $bmp
}

function Png($bmp) {
    $ms = [IO.MemoryStream]::new()
    $bmp.Save($ms, [Drawing.Imaging.ImageFormat]::Png)
    $bmp.Dispose()
    , $ms.ToArray()
}

function Save([string]$path, $bmp) {
    $full = Join-Path $root $path
    [IO.Directory]::CreateDirectory((Split-Path $full)) | Out-Null
    [IO.File]::WriteAllBytes($full, (Png $bmp))
}

function BigEndian([int]$n) { $b = [BitConverter]::GetBytes($n); [Array]::Reverse($b); , $b }

# Linux, the Windows in-app logo.
foreach ($p in 'assets/icon.png', 'gui/windows/purpledisc.png', 'gui/gnome/data/purpledisc.png', 'gui/kde/purpledisc.png') { Save $p (Icon 256) }

# Windows: PNG entries, 16 to 256 px.
$sizes = 16, 20, 24, 32, 40, 48, 64, 128, 256
$images = $sizes | ForEach-Object { , (Png (Icon $_)) }
$ico = [IO.MemoryStream]::new(); $w = [IO.BinaryWriter]::new($ico)
$w.Write([uint16]0); $w.Write([uint16]1); $w.Write([uint16]$sizes.Count)
$offset = 6 + 16 * $sizes.Count
for ($i = 0; $i -lt $sizes.Count; $i++) {
    $w.Write([byte]($sizes[$i] % 256)); $w.Write([byte]($sizes[$i] % 256)); $w.Write([uint16]0)
    $w.Write([uint16]1); $w.Write([uint16]32); $w.Write([uint32]$images[$i].Length); $w.Write([uint32]$offset)
    $offset += $images[$i].Length
}
$images | ForEach-Object { $w.Write($_) }
[IO.File]::WriteAllBytes((Join-Path $root 'gui/windows/purpledisc.ico'), $ico.ToArray())

# macOS: the tile sits inside Apple's margin.
$types = [ordered]@{ ic11 = 32; ic12 = 64; ic07 = 128; ic13 = 256; ic08 = 256; ic14 = 512; ic09 = 512; ic10 = 1024 }
$body = [IO.MemoryStream]::new()
foreach ($t in $types.GetEnumerator()) {
    $png = Png (Icon $t.Value 'tile' 0.098)
    $body.Write([Text.Encoding]::ASCII.GetBytes($t.Key), 0, 4); $body.Write((BigEndian ($png.Length + 8)), 0, 4); $body.Write($png, 0, $png.Length)
}
$icns = [IO.MemoryStream]::new()
$icns.Write([Text.Encoding]::ASCII.GetBytes('icns'), 0, 4); $icns.Write((BigEndian ($body.Length + 8)), 0, 4); $body.WriteTo($icns)
[IO.File]::WriteAllBytes((Join-Path $root 'assets/PurpleDisc.icns'), $icns.ToArray())

# iOS: full square, the system rounds it.
Save 'gui/ios/Resources/Assets.xcassets/AppIcon.appiconset/AppIcon.png' (Icon 1024 'bleed')

# Android below API 26 (newer versions use the adaptive vector).
$res = 'gui/android/app/src/main/res'
foreach ($d in @{ mdpi = 48; hdpi = 72; xhdpi = 96; xxhdpi = 144; xxxhdpi = 192 }.GetEnumerator()) {
    Save "$res/mipmap-$($d.Key)/ic_launcher.png" (Icon $d.Value 'tile' 0.04)
    Save "$res/mipmap-$($d.Key)/ic_launcher_round.png" (Icon $d.Value 'round')
}

# Android TV banner, 320x180 dp at xhdpi.
$bmp = [Drawing.Bitmap]::new(640, 360, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
$g = [Drawing.Graphics]::FromImage($bmp)
$g.SmoothingMode = 'AntiAlias'; $g.PixelOffsetMode = 'HighQuality'; $g.TextRenderingHint = 'AntiAlias'
$g.Clear($purple)
Fill $g $lilac { param($b) $g.FillEllipse($b, -140, -230, 520, 820) }
$state = $g.Save()
$g.TranslateTransform(24, 60); $g.ScaleTransform(2, 2)
Mark $g $false
$g.Restore($state)
$font = [Drawing.Font]::new('Inter Display SemiBold', 50, [Drawing.GraphicsUnit]::Pixel)
Fill $g (White) { param($b) $g.DrawString('PurpleDisc', $font, $b, 272, 144) }
$font.Dispose(); $g.Dispose()
Save 'gui/android/app/src/tv/res/drawable-xhdpi/tv_banner.png' $bmp
