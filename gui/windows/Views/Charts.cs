// Charts: top tracks, albums, artists and playlists, each with "Show all", for every genre or one.

using System.Collections.Generic;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class ChartsView : View
{
    private readonly TrackList _tracks = new() { Context = new("tops_track", "0") };
    private readonly TileGrid<Album> _albums = TileGrids.Albums();
    private readonly TileGrid<ArtistInfo> _artists = TileGrids.Artists();
    private readonly TileGrid<Playlist> _playlists = TileGrids.Playlists();
    private readonly Section _tracksSection;
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;
    private readonly ComboBox _genre;
    // The genres the picker lists, "All" first; the picked one is not saved.
    private IReadOnlyList<Genre> _genres = new[] { new Genre { Id = "0" } };
    private string _genreId = "0";
    private CatalogReply _charts = new();

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    private string Key => _genreId == "0" ? Library.ChartsKey : Library.Key(Library.ChartsKey, _genreId);

    public ChartsView()
    {
        _tracksSection = new Section("Section_TopTracks", _tracks.Root,
            () => CollectionView.Open(Collection.Of(Loc.S("Nav_Charts"), _charts.Tracks, _tracks.Context)));
        _state = new StateView(Section.Stack(
            _tracksSection.Root,
            Section.Tiles("Section_Albums", _albums, Count.TileRows).Root,
            Section.Tiles("Section_Artists", _artists, Count.TileRows).Root,
            Section.Tiles("Section_Playlists", _playlists, Count.TileRows).Root), SkeletonShape.Page, inset: false);
        _genre = Ui.Combo([Loc.S("Charts_AllGenres")], 0, i =>
        {
            _genreId = _genres[i].Id;
            _tracks.Context = new("tops_track", _genreId);
            Show(null, false);
        });
        Ui.Name(_genre, Loc.S("Charts_Genre"));
        _scroller = Ui.Page(Ui.PageTitle(Loc.S("Nav_Charts"), _genre), _state.Root);
    }

    public override async void Show(object? arg, bool isBack)
    {
        _genre.IsEnabled = !App.Offline.Mode;
        if (_genres.Count == 1 && Session.SignedIn && !App.Offline.Mode) LoadGenres();
        string key = Key;
        if (await LoadAsync<CatalogReply>(key, _state) is not { } charts || key != Key) return;
        if (!Same(charts)) Fill(charts);
        _state.ShowResult(charts.Tracks.Count + charts.Albums.Count + charts.Artists.Count + charts.Playlists.Count == 0,
            charts.Error, () => Show(null, false), Icons.Charts, Loc.S("Empty_Charts"));
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    // The genre list, read once; "All" keeps the app's own word.
    private async void LoadGenres()
    {
        var reply = await Task.Run(DeezerCore.Genres);
        if (reply.Error.Length > 0 || reply.Genres.Count < 2 || _genres.Count > 1) return;
        _genres = reply.Genres;
        for (int i = 1; i < _genres.Count; i++) _genre.Items.Add(_genres[i].Name);
    }

    protected override void OnLibraryChanged(string key)
    {
        if (key == Key && Library.Peek<CatalogReply>(key) is { } charts && !Same(charts)) KeepScroll(() => Fill(charts));
    }

    private void Fill(CatalogReply charts)
    {
        _charts = charts;
        _tracks.SetItems(charts.Tracks, Count.TopTracks);
        _tracksSection.SetShowAll(charts.Tracks.Count, Count.TopTracks);
        _albums.SetItems(charts.Albums);
        _artists.SetItems(charts.Artists);
        _playlists.SetItems(charts.Playlists);
    }
}
