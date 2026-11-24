// One collection as a page: a hero over its track list. The list has a search and a sort, and a
// playlist of one's own reorders by drag while it shows in its own order. The hero follows the
// collection (heart, More) and, for Flow, offers its moods.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;

namespace OpenDeezer;

internal sealed class CollectionView : View
{
    internal readonly TrackList _list = new(ListMode.Full);
    internal readonly Hero _hero;
    private readonly Button _download, _more;
    private readonly FavoriteButton _favorite = new();
    // "Available offline" for an album or playlist, with the fetch's progress beside it.
    internal readonly ToggleSwitch _offline;
    internal readonly TextBlock _offlineProgress = Ui.Text("", TextKind.SecondaryCaption);
    internal readonly StackPanel _offlineRow;
    private readonly ListControls<Track> _controls = new("tracks", t => t.Name + " " + t.ArtistLine + " " + t.AlbumName,
        new("Sort_Default", null), new("Sort_TitleAz", t => t.Name), new("Sort_ArtistAz", t => t.ArtistLine), new("Sort_AlbumAz", t => t.AlbumName));
    private readonly StackPanel _moods = Ui.Row(Space.S);
    private readonly StateView _state;
    private Collection _c = Collection.Liked;
    // The collection's tracks as they came; the list shows them through the search and sort.
    private IReadOnlyList<Track> _tracks = [];
    // The hero shows this collection: a rename or another collection with the same reply refills.
    private Collection? _filledFor;
    private bool _own;
    private bool _downloading;

    public override FrameworkElement Root => _state.Root;
    public override ScrollViewer? Scroller => _list.Scroller;

    public static void Open(Collection c) => App.Navigator.Go<CollectionView>(c);

    public CollectionView()
    {
        _download = Ui.IconButton(Icons.Download, "Menu_Download", Download);

        _more = Menus.MoreButton(FillMore);
        _offline = Ui.Switch(false, "Offline_Available", on => Menus.SetOffline(OfflineKind, _c.Id, on));
        _offlineRow = Ui.LabeledSwitch(_offline, "Offline_Available", _offlineProgress);
        // The switch on a line of its own under the buttons, so a narrow window keeps them whole.
        _hero = new Hero(ArtShape.Large, Size.HeroArt, Ui.Stack(Space.S,
            Ui.Row(Space.S,
                Ui.Button(Loc.S("Btn_Play"), () => Play(false), Icons.Play, accent: true),
                Ui.Button(Loc.S("Btn_ShufflePlay"), () => Play(true), Icons.Shuffle),
                _favorite.Root, _download, _more),
            _offlineRow,
            _moods));
        _controls.Changed += () => ShowTracks();
        _controls.Root.Margin = Pad.Header;
        _list.Header = Ui.Stack(0, _hero.Root, _controls.Root, _list.ColumnHeader());
        _list.Reorder = Reorder;
        Ui.PagePadding(_list.Root, p => _list.Padding = p);
        _state = new StateView(_list.Root, SkeletonShape.Rows);
        _list.Root.Loaded += (_, _) =>
        {
            RenderOffline();
            App.Offline.Changed += RenderOffline;
            App.Offline.Progress += RenderOffline;
        };
        _list.Root.Unloaded += (_, _) =>
        {
            App.Offline.Changed -= RenderOffline;
            App.Offline.Progress -= RenderOffline;
        };
    }

    private string OfflineKind => _c.Kind == CollectionKind.Album ? "album" : "playlist";

    // The favorite kind of what the page shows ("" for none: liked songs, Flow, a mix, one's own playlist).
    private string FavoriteKind => _c.Kind switch
    {
        CollectionKind.Album => "album",
        CollectionKind.Playlist when !_own => "playlist",
        CollectionKind.Podcast => "podcast",
        _ => "",
    };

    // Pinned or not, and how far its fetch has come. Turned off while fetching, the switch unpins,
    // which stops the fetch; a fetch that stopped (a full disk, canceled) shows off with its count,
    // and on again pins anew, which fetches the rest. While a pin or unpin is on its way it shows the
    // value asked for, disabled.
    internal void RenderOffline()
    {
        // Download needs a paid plan and, as in the menus, is disabled in offline mode and while
        // one runs.
        _download.IsEnabled = Session.CanDownload && !App.Offline.Mode && !_downloading;
        Ui.Name(_download, Loc.S(Session.CanDownload ? "Menu_Download" : "Menu_DownloadRequiresPremium"));
        foreach (var mood in _moods.Children.OfType<Control>()) mood.IsEnabled = !App.Offline.Mode;
        bool pinnable = _c.Kind is CollectionKind.Album or CollectionKind.Playlist;
        _offlineRow.Visibility = Ui.Show(pinnable);
        if (!pinnable) return;
        var job = App.Offline.Job(OfflineKind, _c.Id);
        bool? requested = App.Offline.Requested(OfflineKind, _c.Id);
        _offline.IsEnabled = Session.CanDownload && requested == null;
        Ui.Quietly(_offline, () => _offline.IsOn = requested ?? (App.Offline.IsPinned(OfflineKind, _c.Id) && job is not { Error.Length: > 0 }));
        string progress = job is { Total: > 0 } ? Loc.Format("Offline_ProgressFormat", job.Done, job.Total) : "";
        Ui.SetText(_offlineProgress, progress);
        _offlineProgress.Visibility = Ui.Show(progress.Length > 0);
    }

    public override async void Show(object? arg, bool isBack)
    {
        var previous = _c;
        _c = arg as Collection ?? Collection.Liked;
        Session.AccountChanged -= OnAccountChanged;
        Session.AccountChanged += OnAccountChanged;
        _list.Context = _c.Listen;
        _own = _c.Kind == CollectionKind.Playlist && Library.IsOwn(_c.OwnerId);
        _list.MenuContext = new TrackMenu(PlaylistId: _own ? _c.Id : "");
        _list.ShowAlbum = _c.Kind != CollectionKind.Album;
        _download.Visibility = Ui.Show(_c.Kind is CollectionKind.Album or CollectionKind.Playlist);
        // Liked and Pinned songs have nothing for it to offer.
        _more.Visibility = Ui.Show(_c.Kind is not (CollectionKind.Liked or CollectionKind.Pinned));
        // Flow and a mix play as they come: no search or sort.
        _controls.Root.Visibility = Ui.Show(_c.Kind is not (CollectionKind.Flow or CollectionKind.Tracks));
        if (_c != previous) _controls.Use("tracks:" + _c.Kind);
        RenderOffline();
        _favorite.Bind(FavoriteKind, _c.Id);
        RenderMoods();
        // An episode row shows where the account stopped in it.
        if (_c.Kind == CollectionKind.Podcast) _ = Episodes.RefreshAsync();
        if (_c.Tracks != null)
        {
            Render(_c.Tracks, _c.Tracks);
            _state.ShowContent();
        }
        else
        {
            // The placeholder's token is the collection, so the real reply always refills.
            if (Library.Peek<Reply>(_c.Key) == null) Render(_c, []);
            if (await LoadAsync<Reply>(_c.Key, _state) is not { } reply) return;
            Render(reply, _c.TracksOf(reply));
            // An empty collection keeps its hero; the meta line says it is empty.
            if (reply.Error.Length > 0 && _tracks.Count == 0) _state.ShowFailed(reply.Error, () => Show(_c, false));
            else _state.ShowContent();
        }
        if (!isBack) Scroller?.ChangeView(null, 0, null, disableAnimation: true);
        ContentReady();
    }

    // A reply dropped while shown (the offline set or mode changed) is fetched again; the rows stay
    // until the new one is in, and stay as they are when it fails.
    protected override async void OnLibraryChanged(string key)
    {
        if (key != _c.Key) return;
        if (Library.Peek<Reply>(key) is not { } r)
        {
            if (_c.Tracks != null) return;
            var ct = Ct;
            r = await Library.GetAsync<Reply>(key);
            if (ct.IsCancellationRequested || key != _c.Key || r.Error.Length > 0) return;
        }
        KeepScroll(() =>
        {
            Render(r, _c.TracksOf(r), edit: true);
            _state.ShowContent();
        });
    }

    // The list refills for a new reply instance (`edit`: the same list changed, applied as a
    // single-row edit when it is one); the hero also for another or renamed collection.
    private void Render(object data, IReadOnlyList<Track> tracks, bool edit = false)
    {
        // An album opened from where its artist was not named (a search item, a pin, offline) takes
        // it from its tracks when they all name the same one (the album reply has no album block); a
        // compilation gets no artist link.
        if (_c.Kind == CollectionKind.Album && _c.ArtistId.Length == 0
            && tracks.Select(t => t.ArtistId).Where(id => id.Length > 0).Distinct().ToList() is [var only])
            _c = _c with { ArtistId = only };
        bool newList = !Same(data);
        if (newList)
        {
            _tracks = tracks;
            ShowTracks(edit);
        }
        if (newList || _c != _filledFor) FillHero(tracks);
    }

    // The tracks through the search and sort. Reordering by drag only in the list's own order.
    private void ShowTracks(bool edit = false)
    {
        var shown = _controls.Apply(_tracks);
        if (edit) _list.Update(shown);
        else _list.SetItems(shown);
        _list.CanReorder = _own && _controls.IsNatural && !App.Offline.Mode;
    }

    private void FillHero(IReadOnlyList<Track> tracks)
    {
        _filledFor = _c;
        long ms = 0;
        foreach (var t in tracks) ms += t.DurationMs;
        var meta = new List<string>(3);
        if (_c.Owner.Length > 0) meta.Add(_c.Owner);
        if (tracks.Count > 0) meta.Add(Loc.Plural(_c.Kind == CollectionKind.Podcast ? "Episodes" : "Tracks", tracks.Count));
        else meta.Add(Loc.S(_c.Kind switch { CollectionKind.Liked => "Empty_Liked", CollectionKind.Playlist => "Empty_Playlist", _ => "Empty_Tracks" }));
        if (ms > 0) meta.Add(Fmt.Duration(ms));
        string? generated = _c.Kind switch { CollectionKind.Liked => Icons.HeartFill, CollectionKind.Flow => Icons.Flow, _ => null };
        _hero.Set(_c.KindLabel, _c.Title, string.Join(" · ", meta), _c.ArtUrl, generated,
            _c.Kind == CollectionKind.Album && _c.ArtistId.Length > 0 && _c.Owner.Length > 0 && !App.Offline.Mode ? () => ArtistView.Open(_c.ArtistId) : null);
        _hero.Actions.Visibility = Ui.Show(tracks.Count > 0);
    }

    public override void Hide() => Session.AccountChanged -= OnAccountChanged;

    // The account read after the page opened (a cached Home paints first) can make the playlist
    // one's own, or not: the page shows it again. Its plan enables Download and the offline switch.
    private void OnAccountChanged()
    {
        RenderOffline();
        if (_c.Kind == CollectionKind.Playlist && Library.IsOwn(_c.OwnerId) != _own) Show(_c, true);
    }

    // The kind's menu, as on its tile; Flow and a mix offer Save as playlist.
    private void FillMore(IList<MenuFlyoutItemBase> items)
    {
        Menus.Collection(items, _c, _tracks.Count);
        bool savable = _c.Kind is CollectionKind.Flow or CollectionKind.Tracks && _tracks.Count > 0;
        var save = savable ? Menus.Item("Menu_SaveAsPlaylist", Icons.Add, () => Menus.SaveAsPlaylist(_c.Title, _tracks)) : null;
        if (save != null) save.IsEnabled = !App.Offline.Mode;
        Menus.Group(items, save);
    }

    // Flow's moods under its hero, each playing its own Flow.
    private async void RenderMoods()
    {
        _moods.Visibility = Ui.Show(_c.Kind == CollectionKind.Flow);
        if (_c.Kind != CollectionKind.Flow || _moods.Children.Count > 0) return;
        var (moods, _) = await Task.Run(DeezerCore.FlowMoods);
        if (_moods.Children.Count > 0) return;
        foreach (var m in moods)
        {
            var b = Ui.Button(m.Title, () => PlayMood(m));
            b.IsEnabled = !App.Offline.Mode;
            _moods.Children.Add(b);
        }
    }

    private static async void PlayMood(FlowMood m)
    {
        var reply = await Task.Run(() => DeezerCore.Flow(m.Id));
        if (reply.Tracks.Count == 0)
        {
            Toast.Error(Loc.S("Dialog_RadioFailTitle"), reply.Error.Length > 0 ? reply.Error : Loc.S("Dialog_RadioFailBody"));
            return;
        }
        var listen = new ListenContext("dynamic_page_flow_config", m.Id);
        Open(Collection.Of(m.Title, reply.Tracks, listen));
        Player.Play(reply.Tracks, 0, listen);
    }

    // A row dragged in one's own playlist: the new order goes to Deezer, and back on a refusal.
    private async void Reorder(int from, int to)
    {
        var before = _tracks;
        var order = _list.Items.ToList();
        _tracks = order;
        string id = _c.Id;
        var ids = System.Text.Json.JsonSerializer.Serialize<IReadOnlyList<string>>(order.Select(t => t.Id).ToList(), WireJson.Default.IReadOnlyListString);
        if (await Task.Run(() => DeezerCore.DZReorderPlaylist(id, ids) != 0))
        {
            Library.Reordered(id);
            return;
        }
        if (id != _c.Id) return;
        _tracks = before;
        ShowTracks();
        Toast.Error(Loc.S("Playlist_EditFailTitle"), "");
    }

    private void Play(bool shuffle)
    {
        var items = _list.Items;
        if (items.Count == 0) return;
        if (shuffle && !Player.Shuffle) Player.SetShuffle(true);
        Player.Play(items, Player.Shuffle ? Random.Shared.Next(items.Count) : 0, _c.Listen);
    }

    // The batch download (the harness holds it open).
    internal Func<Collection, Task> DownloadBatch = Menus.DownloadCollection;

    private async void Download()
    {
        _downloading = true;
        _download.IsEnabled = false;
        try { await DownloadBatch(_c); }
        finally
        {
            _downloading = false;
            RenderOffline();
        }
    }
}
