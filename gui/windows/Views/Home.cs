// Home: a greeting, what was played recently on this PC, the user's playlists and the top tracks.

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
    internal readonly TileGrid<Playlist> _playlists = new(Tiles.OwnPlaylist, p => CollectionView.Open(Collection.Of(p)), p => Menus.PlayCollection(Collection.Of(p)), Menus.Playlist);
    internal readonly TrackList _top = new();
    private readonly Section _topSection;
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public HomeView()
    {
        _recent = new TileGrid<Track>(Tiles.Track, t => Player.Play(_recentTracks, _recentTracks.IndexOf(t)));
        // "Show all" opens the page that holds all of them.
        var recentSection = Section.Tiles("Section_RecentlyPlayed", _recent, Count.TileRows, () => Nav.Push<RecentView>());
        var playlistsSection = Section.Tiles("Section_YourPlaylists", _playlists, Count.TileRows, () => Nav.Push<PlaylistsView>());
        _topSection = new Section("Section_TopTracks", _top.Root, () => Nav.Push<ChartsView>());
        _state = new StateView(Section.Stack(recentSection.Root, playlistsSection.Root, _topSection.Root), SkeletonShape.Page, inset: false);
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
        var home = await Library.GetAsync<HomeReply>(Library.HomeKey);
        if (ct.IsCancellationRequested) return;
        // A failed refresh keeps what an earlier run painted.
        if (home.Error.Length > 0 && Library.Home is { } shown) home = shown;
        Fill(home);
        _state.ShowResult(home.TopTracks.Count == 0 && home.Playlists.Count == 0, home.Error, () => Show(null, false), Icons.Home, Loc.S("Empty_Home"));
        var recent = await history;
        if (ct.IsCancellationRequested) return;
        FillRecent(recent);
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    protected override void OnLibraryChanged(string key)
    {
        if (key == Library.HomeKey && Library.Home is { } home) KeepScroll(() => Fill(home));
    }

    private void Fill(HomeReply home)
    {
        _state.ShowContent();
        if (Same(home)) return;
        _playlists.SetItems(home.Playlists);
        _top.SetItems(home.TopTracks, Count.TopTracks);
        _topSection.Visible = home.TopTracks.Count > 0;
    }

    private void FillRecent(IReadOnlyList<Track> history)
    {
        var seen = new HashSet<string>();
        var tracks = new List<Track>();
        foreach (var t in Library.Enrich(history))
            if (tracks.Count < Count.RecentRail && seen.Add(t.Id)) tracks.Add(t);
        if (tracks.Select(t => t.Id).SequenceEqual(_recentTracks.Select(t => t.Id))) return;
        _recentTracks = tracks;
        _recent.SetItems(tracks);
    }
}
