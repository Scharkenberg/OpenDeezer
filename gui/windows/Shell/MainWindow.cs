// The window: title bar, navigation pane, view host with the queue pane and toasts,
// and the player bar.

using System;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;

namespace PurpleDisc;

public sealed partial class MainWindow : Window
{
    private static Player Player => App.Player;
    private static Session Session => App.Session;

    private readonly Grid _root = Ui.Rows("auto * auto");
    private readonly AppTitleBar _titleBar;
    private readonly SearchBox _search;
    private readonly Button _account, _settingsButton;
    private readonly NavigationView _nav;
    private readonly NavigationViewItem _home, _liked, _playlists, _recent, _offline, _flow, _charts;
    private readonly Navigator _navigator = App.Navigator;
    private readonly PaneGrip _grip;
    private readonly QueuePane _queue;
    private readonly PlayerBar _bar;
    private readonly Tray _tray;
    private readonly WindowBounds _bounds;
    private readonly IntPtr _hwnd;
    private MediaControls? _media;
    // Setting SelectedItem inside ItemInvoked re-raises ItemInvoked under UI Automation.
    private bool _inNavInvoked;
    private bool _quitting;
    // An installer sets it to replace this copy: the app saves and quits as from the tray. Manual
    // reset: the installed terminal app waits on the same event.
    private readonly System.Threading.EventWaitHandle _quitSignal = new(false, System.Threading.EventResetMode.ManualReset, Installer.QuitEvent);

    public MainWindow()
    {
        var dq = DispatcherQueue;
        _hwnd = WinRT.Interop.WindowNative.GetWindowHandle(this);
        Ui.Scale = Ui.DpiScale(_hwnd);
        _tray = new Tray(RestoreWindow, Quit);
        _bounds = new WindowBounds(this, _hwnd, Session.Settings);
        Title = "Purple Disc";
        // Mica Alt: the stronger tint of the desktop wallpaper, as in the Windows 11 apps with tabs.
        SystemBackdrop = new MicaBackdrop { Kind = Microsoft.UI.Composition.SystemBackdrops.MicaKind.BaseAlt };

        if (System.IO.File.Exists(Tray.IconPath())) AppWindow.SetIcon(Tray.IconPath());
        _search = new SearchBox(dq, _navigator);
        _account = AccountPanel.TitleBarButton();
        _settingsButton = Ui.IconButton(Icons.Settings, "Nav_Settings", () => _navigator.Go<SettingsView>(), turn: true);

        _home = NavItem("Nav_Home", Icons.Home, () => _navigator.Go<HomeView>());
        _liked = NavItem("Nav_LikedSongs", Icons.Heart, () => _navigator.Go<CollectionView>(Collection.Liked));
        _playlists = NavItem("Nav_Favorites", Icons.Playlists, () => _navigator.Go<PlaylistsView>());
        _recent = NavItem("Nav_Recent", Icons.Recent, () => _navigator.Go<RecentView>());
        _offline = NavItem("Nav_Offline", Icons.Offline, () => _navigator.Go<OfflineView>());
        // Flow plays where the user is: an action, never the selected item.
        _flow = NavItem("Nav_Flow", Icons.Flow, () => Menus.PlayFlow());
        _flow.SelectsOnInvoked = false;
        _charts = NavItem("Nav_Charts", Icons.Charts, () => _navigator.Go<ChartsView>());
        _nav = new NavigationView
        {
            PaneDisplayMode = NavigationViewPaneDisplayMode.Auto,
            IsBackButtonVisible = NavigationViewBackButtonVisible.Collapsed,
            IsPaneToggleButtonVisible = false,
            IsSettingsVisible = true,
            AlwaysShowHeader = false,
            OpenPaneLength = Size.Pane,
            CompactPaneLength = Size.Rail,
        };
        var library = new NavigationViewItemHeader { Content = Loc.S("Nav_GroupLibrary") };
        var discover = new NavigationViewItemHeader { Content = Loc.S("Nav_GroupDiscover") };
        foreach (var item in new object[] { _home, library, _liked, _playlists, _recent, _offline, discover, _flow, _charts })
            _nav.MenuItems.Add(item);
        _nav.ItemInvoked += OnNavInvoked;
        _titleBar = new AppTitleBar(this, () => _navigator.Back(), () => _nav.IsPaneOpen = !_nav.IsPaneOpen, _account, _settingsButton);
        _grip = new PaneGrip(_nav, _root, Session.Settings.PaneWidth, w =>
        {
            Session.Settings.PaneWidth = w;
            Session.SaveSettingsSoon();
        });

        _queue = new QueuePane(_navigator.Host, _root);
        var body = new Grid();
        body.Children.Add(_queue.Root);
        body.Children.Add(Toast.Root);
        _nav.Content = body;
        _bar = new PlayerBar(dq);
        _bar.LyricsToggled += ToggleLyrics;
        _bar.QueueToggled += on => _queue.IsOpen = on;
        _queue.Closed += () => _bar.SetQueueOpen(false);
        QueuePane.OpenRequested += () =>
        {
            _queue.IsOpen = true;
            _bar.SetQueueOpen(true);
        };

        Grid.SetRow(_nav, 1);
        Grid.SetRow(_grip, 1);
        Grid.SetRow(_bar.Root, 2);
        _root.Children.Add(_titleBar.Root);
        _root.Children.Add(_nav);
        _root.Children.Add(_grip);
        _root.Children.Add(_bar.Root);
        if (Loc.IsRtl) _root.FlowDirection = FlowDirection.RightToLeft;
        Content = _root;
        ExtendsContentIntoTitleBar = true;
        AppWindow.TitleBar.PreferredHeightOption = TitleBarHeightOption.Standard;
        SetTitleBar(_titleBar.Root);
        Shortcuts.Attach(_root, _navigator, _search.Focus);

        _navigator.Navigated += OnNavigated;
        Session.SignedInChanged += OnSignedInChanged;
        Session.UpdateFound += () =>
        {
            if (_nav.SettingsItem is NavigationViewItem settings) settings.InfoBadge = new InfoBadge();
        };
        App.Offline.Changed += () => _flow.IsEnabled = !App.Offline.Mode;
        Player.Failed += f => Toast.Show(f.Warning ? InfoBarSeverity.Warning : InfoBarSeverity.Error, f.Title, f.Message);
        Player.SessionExpired += () => Toast.Show(InfoBarSeverity.Warning, Loc.S("Login_ExpiredTitle"), Loc.S("Login_Expired"),
            Ui.Button(Loc.S("Login_SignInAgain"), WebSignInView.SignInAgain), sticky: true);
        Player.TrackChanged += () =>
        {
            var t = Player.Current;
            _tray.SetTip(t == null ? "" : t.ArtistLine.Length == 0 ? t.Name : t.Name + " · " + t.ArtistLine);
        };
        // Minimized or hidden: the position clock stops and the poll slows down.
        VisibilityChanged += (_, e) => SetVisible(e.Visible);
        // Scrolling labels rest while the window is in the background.
        Activated += (_, e) => Marquee.SetPaused(e.WindowActivationState == WindowActivationState.Deactivated);
        AppWindow.Changed += (_, e) =>
        {
            if (e.DidVisibilityChange && AppWindow.IsVisible) _bar.RenderPosition();
            // VisibilityChanged need not fire on minimize: a minimized window counts as hidden.
            if (e.DidPresenterChange)
                SetVisible(AppWindow.IsVisible && AppWindow.Presenter is not OverlappedPresenter { State: OverlappedPresenterState.Minimized });
            if (e.DidPositionChange || e.DidSizeChange || e.DidPresenterChange) _bounds.Capture();        };
        _root.Loaded += (_, _) =>
        {
            Dialogs.Root = _root.XamlRoot;
            Dialogs.ApplyTheme();
            Ui.FollowScale(_root.XamlRoot);
        };
        Activated += OnFirstActivated;
        _bounds.Place();
        ApplySignedIn(false);
        _navigator.Root<SignInView>();
    }

    private static NavigationViewItem NavItem(string key, string glyph, Action go) =>
        new() { Content = Loc.S(key), Icon = Ui.Icon(glyph), Tag = go };

    private void OnFirstActivated(object sender, WindowActivatedEventArgs e)
    {
        Activated -= OnFirstActivated;
        _media = new MediaControls(_hwnd, DispatcherQueue);
        _tray.Add();
        AppWindow.Closing += OnClosing;
        System.Threading.ThreadPool.RegisterWaitForSingleObject(_quitSignal, (_, _) => DispatcherQueue.TryEnqueue(Quit), null, System.Threading.Timeout.Infinite, executeOnlyOnce: true);
        if (Session.Settings.WinMax && AppWindow.Presenter is OverlappedPresenter op) op.Maximize();
        Session.CheckForUpdate();
    }

    // Hidden or minimized, the engine and the offline fetch are polled less or not at all.
    private static void SetVisible(bool visible)
    {
        Player.SetVisible(visible);
        App.Offline.SetVisible(visible);
    }

    private void OnSignedInChanged()
    {
        _navigator.Reset();
        _queue.IsOpen = false;
        ApplySignedIn(Session.SignedIn);
        // A session with no network starts on the music kept offline.
        if (Session.IsOffline) _navigator.Root<OfflineView>();
        else if (Session.SignedIn) _navigator.Root<HomeView>();
        else _navigator.Root<SignInView>();
    }

    private void ApplySignedIn(bool on)
    {
        _nav.IsPaneVisible = on;
        _grip.Update();
        _titleBar.PaneToggle.Visibility = Ui.Show(on);
        _titleBar.Search = on ? _search.Root : null;
        _account.Visibility = Ui.Show(on);
        _settingsButton.Visibility = Ui.Show(!on);
        _bar.Root.Visibility = Ui.Show(on);
    }

    private void OnNavInvoked(NavigationView sender, NavigationViewItemInvokedEventArgs args)
    {
        if (_inNavInvoked || !Session.SignedIn) return;
        _inNavInvoked = true;
        try
        {
            if (args.IsSettingsInvoked) _navigator.Go<SettingsView>();
            else if (args.InvokedItemContainer?.Tag is Action go) go();
        }
        finally { _inNavInvoked = false; }
    }

    private void OnNavigated()
    {
        _titleBar.Back.Visibility = Ui.Show(_navigator.CanGoBack);
        _search.Follow(_navigator.Current);
        _bar.SetLyricsShown(_navigator.IsCurrent<LyricsView>());
        // The pane item of the page the user came from: an album opened from Playlists keeps
        // Playlists selected; Search belongs to no item.
        object? item = null;
        foreach (var (view, arg) in _navigator.Trail)
        {
            item = view switch
            {
                HomeView => _home,
                PlaylistsView => _playlists,
                RecentView => _recent,
                OfflineView => _offline,
                ChartsView => _charts,
                SettingsView when Session.SignedIn => _nav.SettingsItem,
                CollectionView when Equals(arg, Collection.Liked) => _liked,
                _ => null,
            };
            if (item != null || view is SearchView) break;
        }
        if (ReferenceEquals(_nav.SelectedItem, item)) return;
        _inNavInvoked = true;
        try { _nav.SelectedItem = item; }
        finally { _inNavInvoked = false; }
    }

    private void ToggleLyrics(bool on)
    {
        if (on && !_navigator.IsCurrent<LyricsView>()) _navigator.Push<LyricsView>();
        else if (!on && _navigator.IsCurrent<LyricsView>()) _navigator.Back();
        _bar.SetLyricsShown(_navigator.IsCurrent<LyricsView>());
    }

    private void OnClosing(AppWindow sender, AppWindowClosingEventArgs args)
    {
        if (_quitting) return;
        var s = Session.Settings;
        if (s.CloseToTray && Session.SignedIn && _tray.Added)
        {
            args.Cancel = true;
            _bounds.Capture();
            AppWindow.Hide();
            if (!s.TrayHintShown)
            {
                s.TrayHintShown = true;
                _tray.Balloon(Loc.S("Tray_HintTitle"), Loc.S("Tray_HintBody"));
            }
            Session.SaveSettingsSoon();
            return;
        }
        PrepareExit();
    }

    internal void RestoreWindow()
    {
        AppWindow.Show();
        Activate();
        NativeMethods.SetForegroundWindow(_hwnd);
    }

    // The window hides first; DZStop then records the listen.
    internal void PrepareExit()
    {
        _quitting = true;
        _bounds.Capture();
        AppWindow.Hide();
        Session.SaveSettingsNow();
        Player.WaitForEngine(Timing.EngineStopWaitMs);
        Player.Shutdown();
        _tray.Remove();
        _media?.Disable();
    }

    internal void CancelExit()
    {
        _quitting = false;
        _tray.Add();
        // PrepareExit stopped the poll and the media controls: a signed-in player works again.
        if (Session.SignedIn) Player.StartPolling();
        _media?.Enable();
        AppWindow.Show();
    }

    private void Quit()
    {
        PrepareExit();
        Application.Current.Exit();
    }

    internal void ShowUnhandledError(Exception ex) =>
        Toast.Show(InfoBarSeverity.Error, Loc.S("Error_Unhandled"), ex.Message, sticky: true);
}
