// The account's favorites, as Deezer's apps group them: playlists (with New playlist), albums,
// artists and podcasts, each a tab of tiles with a search and a sort. Playlists also filter by
// whose they are, when both kinds are there, and lead to Liked songs and Recently played.

using System;
using System.Collections.Generic;
using System.Linq;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class PlaylistsView : View
{
    private enum Tab { Playlists, Albums, Artists, Podcasts }
    private enum Owner { All, Mine, Others }

    private readonly TileGrid<Playlist> _playlists = TileGrids.Playlists(Tiles.OwnPlaylist);
    private readonly TileGrid<Album> _albums = TileGrids.Albums();
    private readonly TileGrid<ArtistInfo> _artists = TileGrids.Artists();
    private readonly TileGrid<Podcast> _podcasts = TileGrids.Podcasts();
    private readonly ListControls<Playlist> _playlistControls = new("playlists", p => p.Name + " " + p.Owner,
        new("Sort_Added", null), new("Sort_TitleAz", p => p.Name));
    private readonly ListControls<Album> _albumControls = new("albums", a => a.Name + " " + a.ArtistLine,
        new("Sort_Added", null), new("Sort_TitleAz", a => a.Name), new("Sort_ArtistAz", a => a.ArtistLine));
    private readonly ListControls<ArtistInfo> _artistControls = new("artists", a => a.Name,
        new("Sort_Added", null), new("Sort_NameAz", a => a.Name));
    private readonly ListControls<Podcast> _podcastControls = new("podcasts", p => p.Name,
        new("Sort_Added", null), new("Sort_TitleAz", p => p.Name));
    internal readonly SelectorBar _tabs = new();
    private readonly SelectorBar _owners = new();
    private readonly Grid _controls = new();
    private readonly Grid _grids = new();
    private readonly Button _newPlaylist;
    private readonly StackPanel _shortcuts;
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public PlaylistsView()
    {
        foreach (var key in new[] { "Section_Playlists", "Section_Albums", "Section_Artists", "Nav_Podcasts" })
            _tabs.Items.Add(new SelectorBarItem { Text = Loc.S(key) });
        _tabs.SelectedItem = _tabs.Items[0];
        _tabs.SelectionChanged += (_, _) => Show(null, false);
        foreach (var key in new[] { "Filter_All", "Filter_ByYou", "Filter_ByOthers" })
            _owners.Items.Add(new SelectorBarItem { Text = Loc.S(key) });
        _owners.SelectedItem = _owners.Items[0];
        _owners.SelectionChanged += (_, _) => Render();
        foreach (var e in new FrameworkElement[] { _playlistControls.Root, _albumControls.Root, _artistControls.Root, _podcastControls.Root })
            _controls.Children.Add(e);
        foreach (var e in new FrameworkElement[] { _playlists.Root, _albums.Root, _artists.Root, _podcasts.Root })
            _grids.Children.Add(e);
        _playlistControls.Changed += Render;
        _albumControls.Changed += Render;
        _artistControls.Changed += Render;
        _podcastControls.Changed += Render;
        _newPlaylist = Ui.Button(Loc.S("Btn_NewPlaylist"), Menus.NewPlaylist, Icons.Add);
        _shortcuts = new StackPanel { Orientation = Orientation.Horizontal, Spacing = Space.S };
        _shortcuts.Children.Add(Ui.Button(Loc.S("Nav_LikedSongs"), () => App.Navigator.Go<CollectionView>(Collection.Liked), Icons.Heart));
        _shortcuts.Children.Add(Ui.Button(Loc.S("Nav_Recent"), () => App.Navigator.Go<RecentView>(), Icons.Recent));
        _state = new StateView(_grids, SkeletonShape.Tiles, inset: false);
        _tabs.Margin = Pad.Header;
        _scroller = Ui.Page(Ui.PageTitle(Loc.S("Nav_Favorites"), _newPlaylist), _tabs, Ui.Stack(Space.S, _shortcuts, _controls, _owners), _state.Root);
    }

    private Tab Selected => (Tab)Math.Max(0, _tabs.Items.IndexOf(_tabs.SelectedItem));

    private static string KeyOf(Tab t) => t switch
    {
        Tab.Playlists => Library.PlaylistsKey,
        Tab.Albums => Library.Key(Library.LibraryKind, "album"),
        Tab.Artists => Library.Key(Library.LibraryKind, "artist"),
        _ => Library.Key(Library.LibraryKind, "podcast"),
    };

    public override async void Show(object? arg, bool isBack)
    {
        var tab = Selected;
        _newPlaylist.Visibility = Ui.Show(tab == Tab.Playlists);
        if (await LoadAsync<Reply>(KeyOf(tab), _state) is not { } reply || tab != Selected) return;
        Render();
        if (reply.Error.Length > 0 && Count(tab) == 0) _state.ShowFailed(reply.Error, () => Show(null, false));
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    protected override void OnLibraryChanged(string key)
    {
        if (key == KeyOf(Selected)) KeepScroll(Render);
    }

    private int Count(Tab t) => t switch
    {
        Tab.Playlists => Library.Playlists.Count,
        _ => Library.Peek<LibraryReply>(KeyOf(t)) is { } r ? r.Albums.Count + r.Artists.Count + r.Podcasts.Count : 0,
    };

    // The tab's tiles through its search and sort; only its own grid and controls show.
    private void Render()
    {
        var tab = Selected;
        for (int i = 0; i < _grids.Children.Count; i++)
        {
            _grids.Children[i].Visibility = Ui.Show(i == (int)tab);
            _controls.Children[i].Visibility = Ui.Show(i == (int)tab);
        }
        var lib = tab == Tab.Playlists ? null : Library.Peek<LibraryReply>(KeyOf(tab));
        int shown = 0;
        switch (tab)
        {
            case Tab.Playlists:
                var all = Library.Playlists;
                // The owner filter only when both kinds are there.
                bool both = all.Any(p => Library.IsOwn(p.OwnerId)) && all.Any(p => !Library.IsOwn(p.OwnerId));
                _owners.Visibility = Ui.Show(both);
                var owner = both ? (Owner)Math.Max(0, _owners.Items.IndexOf(_owners.SelectedItem)) : Owner.All;
                var list = _playlistControls.Apply(owner == Owner.All ? all : all.Where(p => Library.IsOwn(p.OwnerId) == (owner == Owner.Mine)).ToList());
                _playlists.SetItems(list);
                shown = list.Count;
                break;
            case Tab.Albums:
                var albums = _albumControls.Apply(lib?.Albums ?? []);
                _albums.SetItems(albums);
                shown = albums.Count;
                break;
            case Tab.Artists:
                var artists = _artistControls.Apply(lib?.Artists ?? []);
                _artists.SetItems(artists);
                shown = artists.Count;
                break;
            default:
                var podcasts = _podcastControls.Apply(lib?.Podcasts ?? []);
                _podcasts.SetItems(podcasts);
                shown = podcasts.Count;
                break;
        }
        if (tab != Tab.Playlists) _owners.Visibility = Visibility.Collapsed;
        _shortcuts.Visibility = Ui.Show(tab == Tab.Playlists);
        _newPlaylist.IsEnabled = !App.Offline.Mode;
        if (shown > 0) _state.ShowContent();
        else if (tab == Tab.Playlists && Library.Playlists.Count == 0)
            _state.ShowEmpty(Icons.Playlists, Loc.S("Empty_Playlists"), App.Offline.Mode ? null : Loc.S("Btn_NewPlaylist"), App.Offline.Mode ? null : Menus.NewPlaylist);
        else _state.ShowEmpty(Icons.Heart, Loc.S(Count(tab) == 0 ? "Empty_Favorites" : "List_NoMatch"));
    }
}
