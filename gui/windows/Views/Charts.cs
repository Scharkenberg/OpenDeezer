// Charts: top tracks, albums, artists and playlists, each with "Show all".

using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class ChartsView : View
{
    private readonly TrackList _tracks = new();
    private readonly TileGrid<Album> _albums = new(Tiles.Album, a => CollectionView.Open(Collection.Of(a)), a => Menus.PlayCollection(Collection.Of(a)));
    private readonly TileGrid<ArtistInfo> _artists = new(Tiles.Artist, a => ArtistView.Open(a.Id));
    private readonly TileGrid<Playlist> _playlists = new(Tiles.Playlist, p => CollectionView.Open(Collection.Of(p)), p => Menus.PlayCollection(Collection.Of(p)));
    private readonly Section _tracksSection;
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;
    private CatalogReply _charts = new();

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public ChartsView()
    {
        _tracksSection = new Section("Section_TopTracks", _tracks.Root,
            () => CollectionView.Open(Collection.Of(Loc.S("Nav_Charts"), _charts.Tracks)));
        _state = new StateView(Section.Stack(
            _tracksSection.Root,
            Section.Tiles("Section_Albums", _albums, Count.TileRows).Root,
            Section.Tiles("Section_Artists", _artists, Count.TileRows).Root,
            Section.Tiles("Section_Playlists", _playlists, Count.TileRows).Root), SkeletonShape.Page, inset: false);
        _scroller = Ui.Page(Ui.PageTitle(Loc.S("Nav_Charts")), _state.Root);
    }

    public override async void Show(object? arg, bool isBack)
    {
        if (await LoadAsync<CatalogReply>(Library.ChartsKey, _state) is not { } charts) return;
        if (!Same(charts)) Fill(charts);
        _state.ShowResult(charts.Tracks.Count + charts.Albums.Count + charts.Artists.Count + charts.Playlists.Count == 0,
            charts.Error, () => Show(null, false), Icons.Charts, Loc.S("Empty_Charts"));
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    protected override void OnLibraryChanged(string key)
    {
        if (key == Library.ChartsKey && Library.Peek<CatalogReply>(key) is { } charts && !Same(charts)) KeepScroll(() => Fill(charts));
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
