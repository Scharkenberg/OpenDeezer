// Search results, filtered by kind on the client, under Deezer's top result. One search runs at a
// time and the newest query waiting runs next. Podcasts are searched the first time that filter is
// chosen. What is opened from the results goes to the account's search history.

using System;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class SearchView : View
{
    private enum Filter { All, Tracks, Artists, Albums, Playlists, Podcasts }

    internal readonly TextBlock _title = Ui.Text("", TextKind.PageTitle);
    internal readonly SelectorBar _filters = new();
    private readonly TrackList _tracks = new();
    private readonly TileGrid<ArtistInfo> _artists = TileGrids.Artists(a => Picked("Artist", a.Id, () => ArtistView.Open(a.Id)));
    private readonly TileGrid<Album> _albums = TileGrids.Albums(a => Picked("Album", a.Id, () => CollectionView.Open(Collection.Of(a))));
    private readonly TileGrid<Playlist> _playlists = TileGrids.Playlists(open: p => Picked("Playlist", p.Id, () => CollectionView.Open(Collection.Of(p))));
    private readonly TileGrid<Podcast> _podcasts = TileGrids.Podcasts(p => Picked("Podcast", p.Id, () => CollectionView.Open(Collection.Of(p))));
    private readonly Section _tracksSection, _bestSection;
    private readonly Button _best = new() { Style = Ui.Styles.Subtle, HorizontalAlignment = HorizontalAlignment.Left, HorizontalContentAlignment = HorizontalAlignment.Left };
    private readonly Art _bestArt = new(ArtShape.Small, Size.BarArt);
    private readonly TextBlock _bestTitle = Ui.Text("", TextKind.Strong), _bestLine = Ui.Text("", TextKind.SecondaryCaption);
    private SearchItem? _bestItem;
    private readonly LatestValuePump<string> _search;
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;
    internal CatalogReply _result = new();
    private PodcastsReply? _podcastResult;
    internal string _query = "";

    public string Query => _query;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public SearchView()
    {
        _search = new LatestValuePump<string>(Fetch);
        foreach (var key in new[] { "Search_FilterAll", "Search_TracksHeader", "Section_Artists", "Section_Albums", "Section_Playlists", "Nav_Podcasts" })
            _filters.Items.Add(new SelectorBarItem { Text = Loc.S(key) });
        _filters.SelectedItem = _filters.Items[0];
        _filters.SelectionChanged += (_, _) => ApplyFilter();
        _tracksSection = new Section("Search_TracksHeader", _tracks.Root, () => Select(Filter.Tracks));
        // A track played from the results is remembered, and plays from all of them.
        _tracks.Activate = i =>
        {
            var t = _tracks.Items[i];
            Picked("Track", t.Id, () => Player.Play(_result.Tracks, i, _tracks.Context));
        };
        var bestLines = Ui.Stack(0, _bestTitle, _bestLine);
        bestLines.VerticalAlignment = VerticalAlignment.Center;
        _best.Content = Ui.Grid("auto *", Space.M, _bestArt.Root, bestLines);
        _best.Click += (_, _) =>
        {
            if (_bestItem is { } b) Picked(b.Type, b.Id, () => OpenItem(b));
        };
        _bestSection = new Section("Search_TopResult", _best);
        // The state covers the results only: the title and the filter bar stay.
        var results = Section.Stack(
            _bestSection.Root,
            _tracksSection.Root,
            Section.Tiles("Section_Artists", _artists, Count.SearchTileRows, () => Select(Filter.Artists)).Root,
            Section.Tiles("Section_Albums", _albums, Count.SearchTileRows, () => Select(Filter.Albums)).Root,
            Section.Tiles("Section_Playlists", _playlists, Count.SearchTileRows, () => Select(Filter.Playlists)).Root,
            Section.Tiles("Nav_Podcasts", _podcasts, 0).Root);
        _state = new StateView(results, SkeletonShape.Page, inset: false);
        _filters.Margin = Pad.Header;
        _scroller = Ui.Page(Ui.PageTitle(_title), _filters, _state.Root);
        _state.ShowEmpty(Icons.Search, Loc.S("Search_Placeholder"));
    }

    private Filter Selected => (Filter)(_filters.SelectedItem is { } item ? _filters.Items.IndexOf(item) : 0);

    public override void Show(object? arg, bool isBack)
    {
        // Back to an entry of another query runs that query again: the page holds the last one.
        if (arg is string q && (!isBack || q != _query)) Run(q, force: true);
    }

    public void Run(string q, bool force)
    {
        if (!force && q == _query) return;
        _query = q;
        _tracks.Context = new("search_track", q);
        _podcastResult = null;
        _title.Text = Loc.Format("Search_TitleFormat", q);
        if (_result.Tracks.Count + _result.Artists.Count + _result.Albums.Count + _result.Playlists.Count == 0) _state.ShowLoading();
        _search.Post(q);
    }

    private async Task Fetch(string q)
    {
        var best = Task.Run(() => DeezerCore.BestResult(q));
        var result = await Task.Run(() => DeezerCore.Search(q));
        if (q != _query) return;
        var b = await best;
        if (q != _query) return;
        _result = result;
        _bestItem = b.Id.Length > 0 ? b : null;
        if (_bestItem is { } item)
        {
            _bestArt.Set(item.Picture);
            Ui.SetText(_bestTitle, item.Title);
            Ui.SetText(_bestLine, string.Join(" · ", new[] { KindLabel(item.Type), item.Subtitle }.Where(s => s.Length > 0)));
            Ui.Name(_best, item.Title);
        }
        ApplyFilter();
        _scroller.ChangeView(null, 0, null, disableAnimation: true);
    }

    private void Select(Filter f) => _filters.SelectedItem = _filters.Items[(int)f];

    // What is opened from a search goes to the account's search history, as Deezer's apps keep it.
    private static void Picked(string type, string id, Action open)
    {
        open();
        _ = Task.Run(() => DeezerCore.DZAddSearchHistory(type, id));
    }

    public static string KindLabel(string type) => type switch
    {
        "Track" => Loc.S("Kind_Track"),
        "Album" => Loc.S("Kind_Album"),
        "Artist" => Loc.S("Kind_Artist"),
        "Playlist" => Loc.S("Kind_Playlist"),
        "Podcast" => Loc.S("Kind_Podcast"),
        "PodcastEpisode" => Loc.S("Kind_Episode"),
        _ => "",
    };

    // Opens a search history entry or a best result where the app shows its kind.
    public static void OpenItem(SearchItem item)
    {
        switch (item.Type)
        {
            case "Track": TrackView.Open(new Track { Id = item.Id, Name = item.Title, ArtistLine = item.Subtitle, ArtworkUrl = item.Picture }); break;
            case "Album": CollectionView.Open(new Collection(CollectionKind.Album, item.Id, item.Title, item.Subtitle, item.Picture)); break;
            case "Playlist": CollectionView.Open(new Collection(CollectionKind.Playlist, item.Id, item.Title, item.Subtitle, item.Picture) { OwnerId = App.Library.OwnerOf(item.Id) }); break;
            case "Podcast": CollectionView.Open(new Collection(CollectionKind.Podcast, item.Id, item.Title, "", item.Picture)); break;
            case "Artist": ArtistView.Open(item.Id); break;
        }
    }

    internal async void ApplyFilter()
    {
        var f = Selected;
        var r = _result;
        bool all = f == Filter.All;
        _bestSection.Visible = all && _bestItem != null;
        int top = all ? Count.SearchTopTracks : int.MaxValue;
        _tracks.SetItems(f is Filter.All or Filter.Tracks ? r.Tracks : [], top);
        _tracksSection.SetShowAll(r.Tracks.Count, top);
        _tracksSection.Visible = f is Filter.All or Filter.Tracks && r.Tracks.Count > 0;
        int rows = all ? Count.SearchTileRows : 0;
        _artists.SetItems(f is Filter.All or Filter.Artists ? r.Artists : [], rows);
        _albums.SetItems(f is Filter.All or Filter.Albums ? r.Albums : [], rows);
        _playlists.SetItems(f is Filter.All or Filter.Playlists ? r.Playlists : [], rows);
        _podcasts.SetItems([]);
        int shown = f switch
        {
            Filter.All => r.Tracks.Count + r.Artists.Count + r.Albums.Count + r.Playlists.Count,
            Filter.Tracks => r.Tracks.Count,
            Filter.Artists => r.Artists.Count,
            Filter.Albums => r.Albums.Count,
            Filter.Playlists => r.Playlists.Count,
            _ => -1,
        };
        if (shown >= 0)
        {
            _state.ShowResult(shown == 0, r.Error, () => Run(_query, force: true), Icons.Search, Loc.Format("Search_NoResultsFormat", _query));
            return;
        }
        string q = _query;
        if (_podcastResult == null)
        {
            _state.ShowLoading();
            var reply = await Task.Run(() => DeezerCore.SearchPodcasts(q));
            if (q != _query || Selected != Filter.Podcasts) return;
            _podcastResult = reply;
        }
        _podcasts.SetItems(_podcastResult.Podcasts);
        _state.ShowResult(_podcastResult.Podcasts.Count == 0, _podcastResult.Error, () => Run(_query, force: true), Icons.Podcast, Loc.Format("Search_NoResultsFormat", _query));
    }
}
