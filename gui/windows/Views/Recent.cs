// This PC's listening history: total time, then recent plays, top tracks or top artists.

using System.Collections.Generic;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class RecentView : View
{
    internal readonly TextBlock _total = Ui.Text("", TextKind.SectionTitle);
    private readonly SelectorBar _filters = new();
    internal readonly TrackList _recent = new() { Context = new("history_song", "") }, _top = new() { Context = new("history_song", "") };
    // History has no artist id or picture: initials, and a click searches the name.
    private readonly TileGrid<ArtistStat> _artists = new(
        a => new TileData(a.Artist, Loc.Plural("Plays", a.Plays), "", Round: true),
        a => Nav.Push<SearchView>(a.Artist));
    private readonly ScrollViewer _scroller;
    internal readonly StateView _state;
    private (long Seconds, int Count, string Newest) _shown;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public RecentView()
    {
        foreach (var key in new[] { "Section_RecentlyPlayed", "Section_TopTracks", "Section_TopArtists" })
            _filters.Items.Add(new SelectorBarItem { Text = Loc.S(key) });
        _filters.SelectedItem = _filters.Items[0];
        _filters.SelectionChanged += (_, _) => ApplyFilter();
        var headline = Ui.Stack(0, _total, Ui.Text(Loc.Format("Recent_ListenTimeLabel", Count.StatsDays), TextKind.SecondaryCaption));
        headline.Margin = Pad.Header;
        _filters.Margin = Pad.Header;
        var lists = new Grid();
        lists.Children.Add(_recent.Root);
        lists.Children.Add(_top.Root);
        lists.Children.Add(_artists.Root);
        _state = new StateView(Ui.Stack(0, headline, _filters, lists), SkeletonShape.Rows, inset: false);
        _scroller = Ui.Page(Ui.PageTitle(Loc.S("Nav_Recent")), _state.Root);
    }

    // History grows with every play, so it is read on each visit; the lists refill only when it grew.
    public override async void Show(object? arg, bool isBack)
    {
        var ct = Ct;
        if (_recent.Items.Count == 0) _state.ShowLoading();
        var (recent, stats) = await Task.Run(() => (DeezerCore.HistoryRecent(Count.History), DeezerCore.HistoryStats(Count.StatsDays)));
        if (ct.IsCancellationRequested) return;
        // Every play adds listening time: unchanged time and newest play mean the lists stand.
        var shown = (stats.TotalSeconds, recent.Count, recent.Count > 0 ? recent[0].Id : "");
        if (shown == _shown && _recent.Items.Count > 0)
        {
            ContentReady();
            return;
        }
        _shown = shown;
        long sec = System.Math.Max(0, stats.TotalSeconds);
        _total.Text = Loc.Format("Duration_HoursMinutesFormat", sec / 3600, sec % 3600 / 60);
        // One lookup for both lists: the recent plays, then the top tracks by id.
        var both = new List<Track>(recent.Count + stats.TopTracks.Count);
        both.AddRange(recent);
        foreach (var s in stats.TopTracks) both.Add(new Track { Id = s.TrackId });
        var known = Library.Enrich(both);
        _recent.SetItems(known.GetRange(0, recent.Count));
        var top = new List<Track>(stats.TopTracks.Count);
        for (int i = 0; i < stats.TopTracks.Count; i++)
        {
            var s = stats.TopTracks[i];
            var k = known[recent.Count + i];
            string plays = Loc.Plural("Plays", s.Plays);
            top.Add(new Track
            {
                Id = s.TrackId,
                Name = s.Title,
                ArtistId = k.ArtistId,
                ArtistLine = s.Artist.Length == 0 ? plays : s.Artist + " · " + plays,
                AlbumName = k.AlbumName,
                ArtworkUrl = k.ArtworkUrl,
                DurationMs = k.DurationMs,
                IsExplicit = k.IsExplicit,
            });
        }
        _top.SetItems(top);
        _artists.SetItems(stats.TopArtists);
        if (recent.Count + top.Count + stats.TopArtists.Count == 0) _state.ShowEmpty(Icons.Recent, Loc.S("Empty_Recent"));
        else _state.ShowContent();
        ApplyFilter();
        ContentReady();
    }

    private void ApplyFilter()
    {
        // One list per filter, in the filters' order.
        int f = _filters.SelectedItem is { } item ? _filters.Items.IndexOf(item) : 0;
        var lists = new UIElement[] { _recent.Root, _top.Root, _artists.Root };
        for (int i = 0; i < lists.Length; i++) lists[i].Visibility = Ui.Show(i == f);
    }
}
