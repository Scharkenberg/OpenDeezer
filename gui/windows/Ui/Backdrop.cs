// A page over its own artwork: the picture fills the page, blurred, under a tint and a fine grain
// (as acrylic), so the page takes the artwork's colors (the track and artist pages). Without a
// picture the window's Mica shows through. Nothing here is live: the blur is done once per picture
// on a small decode, which a resize only rescales on the GPU; the grain is made once per process
// and laid on a square as large as all displays, pinned to the page's top-left corner, so a resize
// only shows more or less of it.

using System;
using System.IO;
using System.Runtime.InteropServices.WindowsRuntime;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;
using Windows.Graphics.Imaging;
using Windows.Storage.Streams;

namespace PurpleDisc;

internal sealed class Backdrop
{
    private static WriteableBitmap? s_grain;

    private readonly ImageBrush _brush = new() { Stretch = Stretch.UniformToFill };
    private readonly Grid _shown = new() { Opacity = 0 };
    private readonly Canvas _grain = new() { IsHitTestVisible = false };
    private double _grainScale;
    private string _url = "";

    public Grid Root { get; } = new();

    public Backdrop(UIElement content)
    {
        Ui.Fades(_shown, Timing.BackdropFadeMs);
        _shown.Children.Add(new Border { Background = _brush });
        _shown.Children.Add(new Border { Style = Ui.Styles.Get("BackdropVeil") });
        // A Canvas asks the page for no room (the square never sizes the page), and the page's
        // visual clips to its own bounds, which follow every resize without code.
        _shown.Children.Add(_grain);
        Root.Children.Add(_shown);
        Root.Children.Add(content);
        var visual = Microsoft.UI.Xaml.Hosting.ElementCompositionPreview.GetElementVisual(Root);
        visual.Clip = visual.Compositor.CreateInsetClip();
        Root.Loaded += (_, _) => LayGrain();
    }

    public async void Set(string url)
    {
        if (url == _url) return;
        _url = url;
        var image = url.Length == 0 ? null : await Blurred(url);
        // Not when the page shows another picture by now.
        if (url != _url) return;
        _brush.ImageSource = image;
        _shown.Opacity = image == null ? 0 : 1;
    }

    // The grain tiles, one pixel per physical pixel at the current scale, over the longer side of all
    // displays together (no window gets larger); laid again only when the scale changed.
    private void LayGrain()
    {
        if (_grainScale == Ui.Scale) return;
        _grainScale = Ui.Scale;
        s_grain ??= Noise();
        int px = Math.Max(NativeMethods.GetSystemMetrics(NativeMethods.SM_CXVIRTUALSCREEN), NativeMethods.GetSystemMetrics(NativeMethods.SM_CYVIRTUALSCREEN));
        double tile = Size.GrainTile / Ui.Scale;
        int n = (int)Math.Ceiling(Math.Max(px, 1) / (double)Size.GrainTile);
        _grain.Children.Clear();
        for (int y = 0; y < n; y++)
            for (int x = 0; x < n; x++)
            {
                var image = new Image { Source = s_grain, Width = tile, Height = tile, Stretch = Stretch.Fill };
                Canvas.SetLeft(image, x * tile);
                Canvas.SetTop(image, y * tile);
                _grain.Children.Add(image);
            }
    }

    // White noise at a low alpha, premultiplied.
    private static WriteableBitmap Noise()
    {
        const int side = Size.GrainTile;
        var px = new byte[side * side * 4];
        var random = new Random(1);
        for (int i = 0; i < px.Length; i += 4)
        {
            byte v = (byte)(random.Next(256) * Size.GrainAlpha / 255);
            px[i] = px[i + 1] = px[i + 2] = v;
            px[i + 3] = Size.GrainAlpha;
        }
        var bitmap = new WriteableBitmap(side, side);
        using (var s = bitmap.PixelBuffer.AsStream()) s.Write(px, 0, px.Length);
        bitmap.Invalidate();
        return bitmap;
    }

    private static async Task<WriteableBitmap?> Blurred(string url)
    {
        if (!Uri.TryCreate(Ui.SizedArt(url, Size.CdnArt[0]), UriKind.Absolute, out var uri)) return null;
        try
        {
            // A local picture (a file: URI) is not served by RandomAccessStreamReference.
            using var stream = uri.IsFile
                ? await FileRandomAccessStream.OpenAsync(uri.LocalPath, Windows.Storage.FileAccessMode.Read)
                : await RandomAccessStreamReference.CreateFromUri(uri).OpenReadAsync();
            return await Blur(stream);
        }
        catch (Exception e)
        {
            AppLog.Write("backdrop picture failed: " + e.Message);
            return null;
        }
    }

    // The picture decoded at Size.BackdropPicture and blurred.
    internal static async Task<WriteableBitmap> Blur(IRandomAccessStream stream)
    {
        byte[] px;
        {
            var decoder = await BitmapDecoder.CreateAsync(stream);
            var data = await decoder.GetPixelDataAsync(BitmapPixelFormat.Bgra8, BitmapAlphaMode.Premultiplied,
                new BitmapTransform { ScaledWidth = Size.BackdropPicture, ScaledHeight = Size.BackdropPicture, InterpolationMode = BitmapInterpolationMode.Fant },
                ExifOrientationMode.IgnoreExifOrientation, ColorManagementMode.DoNotColorManage);
            px = data.DetachPixelData();
        }
        var tmp = new byte[px.Length];
        for (int i = 0; i < Count.BackdropBlurPasses; i++)
        {
            BoxBlur(px, tmp, horizontal: true);
            BoxBlur(tmp, px, horizontal: false);
        }
        var bitmap = new WriteableBitmap(Size.BackdropPicture, Size.BackdropPicture);
        using (var s = bitmap.PixelBuffer.AsStream()) s.Write(px, 0, px.Length);
        bitmap.Invalidate();
        return bitmap;
    }

    // One box-blur pass along rows or columns, edges clamped.
    private static void BoxBlur(byte[] src, byte[] dst, bool horizontal)
    {
        const int side = Size.BackdropPicture, radius = Size.BackdropBlurRadius, span = 2 * radius + 1;
        for (int line = 0; line < side; line++)
            for (int i = 0; i < side; i++)
                for (int c = 0; c < 4; c++)
                {
                    int sum = 0;
                    for (int k = -radius; k <= radius; k++)
                    {
                        int j = Math.Clamp(i + k, 0, side - 1);
                        sum += src[(horizontal ? line * side + j : j * side + line) * 4 + c];
                    }
                    dst[(horizontal ? line * side + i : i * side + line) * 4 + c] = (byte)(sum / span);
                }
    }
}
