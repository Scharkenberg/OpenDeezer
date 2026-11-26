// One album, playlist, artist, podcast or track tile: square art (round for
// people), title, subtitle. Albums and playlists reveal a play button on hover.

using System;
using System.Linq;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed record TileData(string Title, string Subtitle, string ArtUrl, bool Round = false);

internal static class Tiles
{
    public static TileData Album(Album a) => new(a.Name, a.ArtistLine, a.ArtworkUrl);
    public static TileData Artist(ArtistInfo a) => new(a.Name, Fmt.Fans(a.NbFans), a.ArtworkUrl, Round: true);
    public static TileData Playlist(Playlist p) => new(p.Name, p.Owner, p.ArtworkUrl);
    // The user's own: the owner goes without saying, the track count does not.
    public static TileData OwnPlaylist(Playlist p) => new(p.Name, Loc.Plural("Tracks", p.TrackCount), p.ArtworkUrl);
    public static TileData Podcast(Podcast p) => new(p.Name, p.EpisodeCount > 0 ? Loc.Plural("Episodes", p.EpisodeCount) : p.Description, p.ArtworkUrl);
    public static TileData Track(Track t) => new(t.Name, t.ArtistLine, t.ArtworkUrl);
    // A Home pin: what it is, and whose, under its title.
    public static TileData Pinned(SearchItem p) => new(p.Title,
        string.Join(" · ", new[] { SearchView.KindLabel(p.Type), p.Subtitle }.Where(s => s.Length > 0)), p.Picture, Round: p.Type == "Artist");
    // How much of it is stored while that is not all of it.
    public static TileData Offline(OfflineCollection o) => new(
        o.Kind == PurpleDisc.Offline.TrackKind ? Loc.S("Offline_Songs") : o.Title,
        o.Fetched < o.TrackCount ? Loc.Format("Offline_ProgressFormat", o.Fetched, o.TrackCount) : Loc.Plural("Tracks", o.TrackCount),
        o.Cover);
}

internal sealed partial class MediaTile : Button
{
    private readonly Art _square = new(ArtShape.Large, reuse: true);
    // Made on the first round tile: most tiles never need it.
    private Art? _round;
    private readonly Grid _art = new();
    private readonly TextBlock _title = Ui.Text("", TextKind.Strong);
    private readonly TextBlock _subtitle = Ui.Text("", TextKind.SecondaryCaption);

    public object? Item { get; private set; }

    public MediaTile(Action<MediaTile> open, Action<MediaTile>? play = null)
    {
        Style = Ui.Styles.Subtle;
        Padding = Pad.Tile;
        _art.Children.Add(_square.Root);
        if (play != null)
        {
            var button = Ui.PlayButton(Ui.Icon(Icons.Play));
            button.Margin = Pad.TileButton;
            button.IsTabStop = false;
            button.HorizontalAlignment = HorizontalAlignment.Right;
            button.VerticalAlignment = VerticalAlignment.Bottom;
            Ui.Name(button, Loc.S("Btn_Play"));
            button.Click += (_, _) => play(this);
            _art.Children.Add(button);
            Ui.Reveal(button, false);
            Ui.Fades(button, Timing.RevealMs);
            PointerEntered += (_, e) => Ui.Reveal(button, e.Pointer.PointerDeviceType != PointerDeviceType.Touch);
            PointerExited += (_, _) => Ui.Reveal(button, false);
            GotFocus += (_, _) => Ui.Reveal(button, true);
            LostFocus += (_, _) => Ui.Reveal(button, false);
        }
        Content = Ui.Stack(Space.S, _art, Ui.Stack(0, _title, _subtitle));
        Click += (_, _) => open(this);
    }

    public void Bind(object item, TileData d)
    {
        Item = item;
        if (d.Round && _round == null)
        {
            _round = new Art(ArtShape.Circle, reuse: true);
            // Under the play button, if there is one.
            _art.Children.Insert(1, _round.Root);
        }
        _square.Root.Visibility = Ui.Show(!d.Round);
        if (_round != null) _round.Root.Visibility = Ui.Show(d.Round);
        // The hidden shape lets its picture go.
        _square.Set(d.Round ? "" : d.ArtUrl);
        _round?.Set(d.Round ? d.ArtUrl : "", d.Round ? d.Title : "");
        Ui.SetText(_title, d.Title);
        Ui.SetText(_subtitle, d.Subtitle);
        _subtitle.Visibility = Ui.Show(d.Subtitle.Length > 0);
        AutomationProperties.SetName(this, d.Subtitle.Length == 0 ? d.Title : d.Title + ", " + d.Subtitle);
    }
}
