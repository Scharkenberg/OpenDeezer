// A page over its own artwork: the picture fills the page behind an acrylic layer that blurs
// it, so the page takes the artwork's colors (the track and artist pages). Without a picture
// the window's Mica shows through.

using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;

namespace OpenDeezer;

internal sealed class Backdrop
{
    private readonly ImageBrush _brush = new() { Stretch = Stretch.UniformToFill };
    private readonly Border _picture;
    private readonly Border _veil = new() { Style = Ui.Styles.Get("BackdropVeil"), Visibility = Visibility.Collapsed };
    private string _key = "";

    public Grid Root { get; } = new();

    public Backdrop(UIElement content)
    {
        _picture = new Border { Background = _brush, Opacity = 0 };
        Ui.Fades(_picture, Timing.BackdropFadeMs);
        Root.Children.Add(_picture);
        Root.Children.Add(_veil);
        Root.Children.Add(content);
    }

    public void Set(string url)
    {
        // Blurred: a small decode is all the picture needs.
        string key = Ui.ArtKey(url, Size.BackdropDecode);
        if (key == _key) return;
        _key = key;
        _brush.ImageSource = url.Length == 0 ? null : Ui.Bitmap(url, Size.BackdropDecode);
        _picture.Opacity = url.Length == 0 ? 0 : 1;
        _veil.Visibility = Ui.Show(url.Length > 0);
    }
}
