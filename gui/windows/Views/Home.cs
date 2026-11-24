// Home: a greeting, what the account pinned to Home, what was played recently on this PC, the
// user's playlists, the top tracks and the top albums.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class HomeView : View
{
    private readonly TextBlock _greeting = Ui.Text("", TextKind.PageTitle);
    private readonly TileGrid<Track> _recent;
    private List<Track> _recentTracks = new();
    internal readonly TileGrid<Playlist> _playlists = TileGrids.Playlists(Tiles.OwnPlaylist);
    internal readonly TrackList _top = new() { Context = new("tops_track", "0") };
    // Pins open where their kind does and play from their tile: an album, playlist or podcast its
    // tracks, a song itself, an artist its radio.
    internal readonly TileGrid<SearchItem> _pinned = new(Tiles.Pinned, SearchView.OpenItem, PlayPinned, PinMenu);
    private readonly Section _pinnedSection;
    private readonly TileGrid<Album> _albums = TileGrids.Albums();
    private readonly Section _topSection, _albumsSection;
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public HomeView()
    {
        _recent = new TileGrid<Track>(Tiles.Track, t => Player.Play(_recentTracks, _recentTracks.IndexOf(t), new("history_song", "")),
            menu: (items, t) => Menus.Track(items, t, new TrackMenu()));
        // "Show all" opens the page that holds all of them.
        var recentSection = Section.Tiles("Section_RecentlyPlayed", _recent, Count.TileRows, () => Nav.Push<RecentView>());
        var playlistsSection = Section.Tiles("Section_YourPlaylists", _playlists, Count.TileRows, () => Nav.Push<PlaylistsView>());
        _topSection = new Section("Section_TopTracks", _top.Root, () => Nav.Push<ChartsView>());
        _albumsSection = Section.Tiles("Section_TopAlbums", _albums, Count.TileRows, () => Nav.Push<ChartsView>());
        _pinnedSection = Section.Tiles("Section_Pinned", _pinned, Count.TileRows);
        _pinnedSection.Visible = false;
        _state = new StateView(Section.Stack(_pinnedSection.Root, recentSection.Root, playlistsSection.Root, _topSection.Root, _albumsSection.Root), SkeletonShape.Page, inset: false);
        _scroller = Ui.Page(Ui.PageTitle(_greeting), _state.Root);
    }

    public override async void Show(object? arg, bool isBack)
    {
        int hour = DateTime.Now.Hour;
        _greeting.Text = Loc.S(hour < Timing.MorningEndsHour ? "Greeting_Morning" : hour < Timing.AfternoonEndsHour ? "Greeting_Afternoon" : "Greeting_Evening");
        var ct = Ct;
        if (Library.Home is { } cached) Fill(cached);
        else _state.ShowLoading();
        var history = Task.Run(() => DeezerCore.HistoryRecent(Count.History));
        var pinned = Library.GetAsync<PinnedReply>(Library.PinnedKey);
        var home = await Library.GetAsync<HomeReply>(Library.HomeKey);
        if (ct.IsCancellationRequested) return;
        // A failed refresh keeps what an earlier run painted.
        if (home.Error.Length > 0 && Library.Home is { } shown) home = shown;
        Fill(home);
        FillPinned(await pinned);
        _state.ShowResult(home.TopTracks.Count == 0 && home.Playlists.Count == 0, home.Error, () => Show(null, false), Icons.Home, Loc.S("Empty_Home"));
        var recent = await history;
        if (ct.IsCancellationRequested) return;
        FillRecent(recent);
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    protected override async void OnLibraryChanged(string key)
    {
        if (key == Library.HomeKey && Library.Home is { } home) KeepScroll(() => Fill(home));
        if (key != Library.PinnedKey) return;
        var ct = Ct;
        var pinned = await Library.GetAsync<PinnedReply>(Library.PinnedKey);
        if (!ct.IsCancellationRequested) KeepScroll(() => FillPinned(pinned));
    }

    // The row shows while something is pinned; a failed read keeps what it showed.
    private void FillPinned(PinnedReply pinned)
    {
        if (pinned.Error.Length > 0) return;
        _pinned.SetItems(pinned.Items);
        _pinnedSection.Visible = pinned.Items.Count > 0;
    }

    // A pinned album, playlist or podcast as a collection; null for other kinds.
    private static Collection? PinnedCollection(SearchItem p) => p.Type switch
    {
        "Album" => new Collection(CollectionKind.Album, p.Id, p.Title, p.Subtitle, p.Picture),
        "Playlist" => new Collection(CollectionKind.Playlist, p.Id, p.Title, p.Subtitle, p.Picture) { OwnerId = App.Library.OwnerOf(p.Id) },
        "Podcast" => new Collection(CollectionKind.Podcast, p.Id, p.Title, "", p.Picture),
        _ => null,
    };

    private static Track PinnedTrack(SearchItem p) => new() { Id = p.Id, Name = p.Title, ArtistLine = p.Subtitle, ArtworkUrl = p.Picture };

    // A pin's tile has its kind's menu, which holds Unpin.
    internal static void PinMenu(IList<MenuFlyoutItemBase> items, SearchItem p)
    {
        if (PinnedCollection(p) is { } c) Menus.Collection(items, c);
        else if (p.Type == "Track") Menus.Track(items, PinnedTrack(p), new TrackMenu());
        else if (p.Type == "Artist") Menus.Artist(items, new ArtistInfo { Id = p.Id, Name = p.Title, ArtworkUrl = p.Picture });
        else items.Add(Menus.Pin(p));
    }

    private static void PlayPinned(SearchItem p)
    {
        if (PinnedCollection(p) is { } c) Menus.PlayCollection(c);
        else if (p.Type == "Track") App.Player.Play(new[] { PinnedTrack(p) }, 0, ListenContext.None);
        else if (p.Type == "Artist") { if (!App.Offline.Mode) Menus.StartArtistRadio(p.Id); }
        else SearchView.OpenItem(p);
    }

    private void Fill(HomeReply home)
    {
        _state.ShowContent();
        if (Same(home)) return;
        _playlists.SetItems(home.Playlists);
        _top.SetItems(home.TopTracks, Count.TopTracks);
        _topSection.Visible = home.TopTracks.Count > 0;
        _albums.SetItems(home.TopAlbums);
        _albumsSection.Visible = home.TopAlbums.Count > 0;
    }

    private void FillRecent(IReadOnlyList<Track> history)
    {
        var seen = new HashSet<string>();
        var tracks = new List<Track>();
        foreach (var t in Library.Enrich(history))
            if (tracks.Count < Count.RecentTiles && seen.Add(t.Id)) tracks.Add(t);
        if (tracks.Select(t => t.Id).SequenceEqual(_recentTracks.Select(t => t.Id))) return;
        _recentTracks = tracks;
        _recent.SetItems(tracks);
    }
}
