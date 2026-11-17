// The music kept for offline use, as tiles: albums, playlists and the songs pinned one by one.
// Offline mode is switched here. A session started without a network opens here, with a Retry.

using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class OfflineView : View
{
    private static Offline Offline => App.Offline;

    internal readonly TileGrid<OfflineCollection> _grid = new(
        Tiles.Offline,
        o => CollectionView.Open(Collection.Of(o)),
        o => Menus.PlayCollection(Collection.Of(o)),
        o => Menu(o));
    private readonly ToggleSwitch _mode;
    internal readonly InfoBar _offline = new() { IsOpen = false, IsClosable = false, Severity = InfoBarSeverity.Informational };
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public OfflineView()
    {
        _mode = Ui.Switch(false, "Offline_Mode", on => _ = Offline.SetModeAsync(on));
        var modeLabel = Ui.Text(Loc.S("Offline_Mode"), TextKind.SecondaryBody);
        modeLabel.VerticalAlignment = VerticalAlignment.Center;
        _offline.Title = Loc.S("Offline_YoureOffline");
        _offline.Message = Loc.S("Offline_YoureOfflineBody");
        _offline.ActionButton = Ui.Button(Loc.S("Action_Retry"), Retry);
        _state = new StateView(_grid.Root, SkeletonShape.Tiles, inset: false);
        _scroller = Ui.Page(Ui.PageTitle(Loc.S("Nav_Offline"), Ui.Row(Space.S, modeLabel, _mode)), Ui.Stack(Space.L, _offline, _state.Root));
        _scroller.Loaded += (_, _) => Offline.Changed += Render;
        _scroller.Unloaded += (_, _) => Offline.Changed -= Render;
    }

    public override async void Show(object? arg, bool isBack)
    {
        Render();
        await Offline.LoadAsync();
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    private void Render()
    {
        _offline.IsOpen = Session.IsOffline;
        Ui.Quietly(_mode, () => _mode.IsOn = Offline.Mode);
        // Offline mode is the engine's own while the session has no network.
        _mode.IsEnabled = !Session.IsOffline;
        KeepScroll(() => _grid.Update(Offline.Collections));
        if (Offline.Collections.Count == 0) _state.ShowEmpty(Icons.Pinned, Loc.S("Offline_Empty"));
        else _state.ShowContent();
    }

    private static MenuFlyout Menu(OfflineCollection o)
    {
        var menu = new MenuFlyout();
        menu.Items.Add(Menus.Item("Offline_Remove", Icons.Remove, () => Menus.SetOffline(o.Kind, o.Id, false)));
        return menu;
    }

    private async void Retry()
    {
        _offline.ActionButton.IsEnabled = false;
        var r = await Session.RetryAsync();
        _offline.ActionButton.IsEnabled = true;
        if (Session.IsOffline) Toast.Error(Loc.S("Login_ErrorTitle"), Session.FailureText(r));
    }
}
