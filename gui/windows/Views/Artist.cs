// An artist: picture, name and fans, popular tracks, albums and related artists.

using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class ArtistView : View
{
    private readonly Hero _hero;
    private readonly Button _radio;
    private readonly FavoriteButton _favorite = new();
    private readonly TrackList _top = new();
    private readonly TileGrid<Album> _albums = TileGrids.Albums();
    private readonly TileGrid<ArtistInfo> _related = TileGrids.Artists();
    private readonly Section _topSection;
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;
    private ArtistProfile _profile = new();
    private string _id = "";

    // The page sits on a blurred backdrop of the artist's picture.
    private readonly Backdrop _backdrop;

    public override FrameworkElement Root => _backdrop.Root;
    public override ScrollViewer? Scroller => _scroller;

    public static void Open(string artistId)
    {
        if (artistId.Length > 0) App.Navigator.Go<ArtistView>(artistId);
    }

    // Show all: the artist's top tracks beyond the profile's few; those few if the read fails.
    private async void ShowAllTop()
    {
        string id = _id, name = _profile.Artist.Name;
        var profileTop = _profile.Top;
        var context = _top.Context;
        var ct = Ct;
        var reply = await Task.Run(() => DeezerCore.ArtistTop(id));
        // The page was left meanwhile: no navigation.
        if (ct.IsCancellationRequested) return;
        CollectionView.Open(Collection.Of(name, reply.Tracks.Count > 0 ? reply.Tracks : profileTop, context));
    }

    public ArtistView()
    {
        _radio = Ui.Button(Loc.S("Menu_StartRadio"), () => Menus.StartArtistRadio(_id), Icons.Flow);
        _hero = new Hero(ArtShape.Circle, Size.ArtistArt,
            Ui.Button(Loc.S("Btn_Play"), () => Player.Play(_profile.Top, 0, _top.Context), Icons.Play, accent: true),
            _radio,
            _favorite.Root,
            Menus.MoreButton(items => Menus.Artist(items, _profile.Artist with { Id = _id })));
        _topSection = new Section("Section_Popular", _top.Root, ShowAllTop);
        // The hero is the artist's title and comes with the data, so it sits inside the state.
        _state = new StateView(Section.Stack(
            _hero.Root,
            _topSection.Root,
            Section.Tiles("Section_Albums", _albums, 0).Root,
            Section.Tiles("Section_RelatedArtists", _related, 0).Root), SkeletonShape.Page, inset: false);
        _scroller = Ui.Page(_state.Root);
        _backdrop = new Backdrop(_scroller);
    }

    public override async void Show(object? arg, bool isBack)
    {
        _id = arg as string ?? "";
        _top.Context = new("artist_top", _id);
        _favorite.Bind("artist", _id);
        // Disabled in offline mode, as in the menus.
        _radio.IsEnabled = !App.Offline.Mode;
        if (await LoadAsync<ArtistProfile>(Library.Key("artist", _id), _state) is not { } profile) return;
        if (!Same(profile)) Fill(profile);
        _state.ShowResult(profile.Artist.Name.Length == 0 && profile.Top.Count == 0, profile.Error, () => Show(_id, false), Icons.Person, Loc.S("Empty_Artist"));
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    protected override void OnLibraryChanged(string key)
    {
        if (key == Library.Key("artist", _id) && Library.Peek<ArtistProfile>(key) is { } p && !Same(p)) KeepScroll(() => Fill(p));
    }

    private void Fill(ArtistProfile p)
    {
        _profile = p;
        _hero.Set("", p.Artist.Name.Length > 0 ? p.Artist.Name : Loc.S("Nav_Artist"), Fmt.Fans(p.Artist.NbFans), p.Artist.ArtworkUrl);
        _backdrop.Set(p.Artist.ArtworkUrl);
        _top.SetItems(p.Top, Count.ArtistTopTracks);
        _topSection.SetShowAll(p.Top.Count, Count.ArtistTopTracks);
        _topSection.Visible = p.Top.Count > 0;
        _albums.SetItems(p.Albums);
        _related.SetItems(p.Related);
    }
}
