// An artist: picture, name and fans, popular tracks, albums and related artists.

using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class ArtistView : View
{
    private readonly Hero _hero;
    private readonly TrackList _top = new();
    private readonly TileGrid<Album> _albums = new(Tiles.Album, a => CollectionView.Open(Collection.Of(a)), a => Menus.PlayCollection(Collection.Of(a)));
    private readonly TileGrid<ArtistInfo> _related = new(Tiles.Artist, a => Open(a.Id));
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

    public ArtistView()
    {
        _hero = new Hero(ArtShape.Circle, Size.ArtistArt,
            Ui.Button(Loc.S("Btn_Play"), () => Player.Play(_profile.Top, 0), Icons.Play, accent: true),
            Ui.Button(Loc.S("Menu_StartRadio"), () => Menus.StartArtistRadio(_id), Icons.Flow));
        _topSection = new Section("Section_Popular", _top.Root,
            () => CollectionView.Open(Collection.Of(_profile.Artist.Name, _profile.Top)));
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
