// The user's playlists as tiles, with New playlist; each tile's menu renames or deletes.

using System.Collections.Generic;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class PlaylistsView : View
{
    private readonly TileGrid<Playlist> _grid = new(
        Tiles.OwnPlaylist,
        p => CollectionView.Open(Collection.Of(p)),
        p => Menus.PlayCollection(Collection.Of(p)),
        Menus.Playlist);
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public PlaylistsView()
    {
        var newPlaylist = Ui.Button(Loc.S("Btn_NewPlaylist"), Menus.NewPlaylist, Icons.Add);
        _state = new StateView(_grid.Root, SkeletonShape.Tiles, inset: false);
        _scroller = Ui.Page(Ui.PageTitle(Loc.S("Nav_Playlists"), newPlaylist), _state.Root);
    }

    public override async void Show(object? arg, bool isBack)
    {
        if (await LoadAsync<PlaylistsReply>(Library.PlaylistsKey, _state) is not { } reply) return;
        if (!Same(reply)) _grid.SetItems(reply.Playlists);
        Render(reply.Playlists, reply.Error);
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    protected override void OnLibraryChanged(string key)
    {
        if (key != Library.PlaylistsKey) return;
        KeepScroll(() => _grid.Update(Library.Playlists));
        Render(Library.Playlists, "");
    }

    // Empty keeps the page (title and New playlist) with a message in place of the grid.
    private void Render(IReadOnlyList<Playlist> list, string error)
    {
        if (error.Length > 0 && list.Count == 0) _state.ShowFailed(error, () => Show(null, false));
        else if (list.Count == 0) _state.ShowEmpty(Icons.Playlists, Loc.S("Empty_Playlists"), Loc.S("Btn_NewPlaylist"), Menus.NewPlaylist);
        else _state.ShowContent();
    }
}
