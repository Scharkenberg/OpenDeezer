// Search results, filtered by kind on the client. One search runs at a time and the newest query
// waiting runs next. Podcasts are searched the first time that filter is chosen.

using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class SearchView : View
{
    private enum Filter { All, Tracks, Artists, Albums, Playlists, Podcasts }

    internal readonly TextBlock _title = Ui.Text("", TextKind.PageTitle);
    internal readonly SelectorBar _filters = new();
    private readonly TrackList _tracks = new();
    private readonly TileGrid<ArtistInfo> _artists = new(Tiles.Artist, a => ArtistView.Open(a.Id));
    private readonly TileGrid<Album> _albums = new(Tiles.Album, a => CollectionView.Open(Collection.Of(a)), a => Menus.PlayCollection(Collection.Of(a)));
    private readonly TileGrid<Playlist> _playlists = new(Tiles.Playlist, p => CollectionView.Open(Collection.Of(p)), p => Menus.PlayCollection(Collection.Of(p)));
    private readonly TileGrid<Podcast> _podcasts = new(Tiles.Podcast, p => CollectionView.Open(Collection.Of(p)));
    private readonly Section _tracksSection;
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
        // The state covers the results only: the title and the filter bar stay.
        var results = Section.Stack(
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
        if (arg is string q && !isBack) Run(q, force: true);
    }

    public void Run(string q, bool force)
    {
        if (!force && q == _query) return;
        _query = q;
        _podcastResult = null;
        _title.Text = Loc.Format("Search_TitleFormat", q);
        if (_result.Tracks.Count + _result.Artists.Count + _result.Albums.Count + _result.Playlists.Count == 0) _state.ShowLoading();
        _search.Post(q);
    }

    private async Task Fetch(string q)
    {
        var result = await Task.Run(() => DeezerCore.Search(q));
        if (q != _query) return;
        _result = result;
        ApplyFilter();
        _scroller.ChangeView(null, 0, null, disableAnimation: true);
    }

    private void Select(Filter f) => _filters.SelectedItem = _filters.Items[(int)f];

    internal async void ApplyFilter()
    {
        var f = Selected;
        var r = _result;
        bool all = f == Filter.All;
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
