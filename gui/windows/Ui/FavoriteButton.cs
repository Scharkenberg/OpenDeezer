// The heart of an album, artist, playlist or podcast page: whether it is among the account's
// favorites, from the kept favorites when they are loaded, else read when the page shows it;
// toggled there. A refusal puts it back; a failed read leaves it off and disabled.

using System.Threading.Tasks;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;

namespace PurpleDisc;

internal sealed class FavoriteButton
{
    private string _kind = "", _id = "";

    public ToggleButton Root { get; }

    public FavoriteButton() => Root = Ui.IconToggle(Icons.Heart, "Menu_AddFavorite", Set);

    // kind "album", "artist", "playlist" or "podcast"; "" hides it (nothing to keep). In offline mode
    // it shows the kept state, disabled, as the menus do.
    public async void Bind(string kind, string id)
    {
        _kind = kind;
        _id = id;
        Root.Visibility = Ui.Show(_kind.Length > 0);
        Root.IsEnabled = true;
        if (_kind.Length == 0) return;
        if (App.Offline.Mode)
        {
            Show(App.Library.IsFavorite(kind, id) ?? false);
            Root.IsEnabled = false;
            return;
        }
        if (App.Library.IsFavorite(kind, id) is { } kept)
        {
            Show(kept);
            return;
        }
        Show(false);
        // Not clickable until the state is known: a click would race the read.
        Root.IsEnabled = false;
        int fav = await Task.Run(() => DeezerCore.DZIsFavorite(kind, id));
        if (kind != _kind || id != _id) return;
        Show(fav == 1);
        Root.IsEnabled = fav >= 0;
    }

    private void Show(bool on)
    {
        Ui.Quietly(Root, () => Root.IsChecked = on);
        ((FontIcon)Root.Content).Glyph = on ? Icons.HeartFill : Icons.Heart;
        Ui.Name(Root, Loc.S(on ? "Menu_RemoveFavorite" : "Menu_AddFavorite"));
    }

    private async void Set(bool on)
    {
        string kind = _kind, id = _id;
        Show(on);
        Root.IsEnabled = false;
        bool ok = await App.Library.SetFavoriteAsync(kind, id, on);
        if (kind != _kind || id != _id) return;
        Root.IsEnabled = true;
        if (ok) return;
        Show(!on);
        Toast.Error(Loc.S("Like_FailTitle"), Loc.S("Like_FailBody"));
    }
}
