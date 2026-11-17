// One collection as a page: a hero over its track list.

using System;
using System.Collections.Generic;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class CollectionView : View
{
    internal readonly TrackList _list = new(ListMode.Full);
    internal readonly Hero _hero;
    private readonly Button _download;
    // "Available offline" for an album or playlist, with the fetch's progress beside it.
    internal readonly ToggleSwitch _offline;
    internal readonly TextBlock _offlineProgress = Ui.Text("", TextKind.SecondaryCaption);
    internal readonly StackPanel _offlineRow;
    private readonly StateView _state;
    private Collection _c = Collection.Liked;
    // The hero shows this collection: a rename or another collection with the same reply refills.
    private Collection? _filledFor;

    public override FrameworkElement Root => _state.Root;
    public override ScrollViewer? Scroller => _list.Scroller;

    public static void Open(Collection c) => App.Navigator.Go<CollectionView>(c);

    public CollectionView()
    {
        _download = Ui.IconButton(Icons.Download, "Menu_Download", Download);
        _offline = Ui.Switch(false, "Offline_Available", on => Menus.SetOffline(OfflineKind, _c.Id, on));
        var offlineLabel = Ui.Text(Loc.S("Offline_Available"), TextKind.SecondaryBody);
        foreach (var e in new FrameworkElement[] { offlineLabel, _offline, _offlineProgress }) e.VerticalAlignment = VerticalAlignment.Center;
        _offlineRow = Ui.Row(Space.S, offlineLabel, _offline, _offlineProgress);
        // The switch on a line of its own under the buttons, so a narrow window keeps them whole.
        _hero = new Hero(ArtShape.Large, Size.HeroArt, Ui.Stack(Space.S,
            Ui.Row(Space.S,
                Ui.Button(Loc.S("Btn_Play"), () => Play(false), Icons.Play, accent: true),
                Ui.Button(Loc.S("Btn_ShufflePlay"), () => Play(true), Icons.Shuffle),
                _download),
            _offlineRow));
        _list.Header = Ui.Stack(0, _hero.Root, _list.ColumnHeader());
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

    // Pinned or not, and how far its fetch has come. Turned off while fetching, the switch unpins,
    // which stops the fetch; a fetch that stopped (a full disk, cancelled) shows off with its count,
    // and on again pins anew, which fetches the rest. While a pin or unpin is on its way it shows the
    // value asked for, disabled.
    internal void RenderOffline()
    {
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
        _c = arg as Collection ?? Collection.Liked;
        bool own = _c.Kind == CollectionKind.Playlist && IsOwn(_c);
        _list.MenuContext = new TrackMenu(PlaylistId: own ? _c.Id : "");
        _list.ShowAlbum = _c.Kind != CollectionKind.Album;
        _download.Visibility = Ui.Show(_c.Kind is CollectionKind.Album or CollectionKind.Playlist);
        _download.IsEnabled = Session.CanDownload;
        Ui.Name(_download, Loc.S(Session.CanDownload ? "Menu_Download" : "Menu_DownloadRequiresPremium"));
        RenderOffline();
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
            if (reply.Error.Length > 0 && _list.Items.Count == 0) _state.ShowFailed(reply.Error, () => Show(_c, false));
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
        bool newList = !Same(data);
        if (newList && edit) _list.Update(tracks);
        else if (newList) _list.SetItems(tracks);
        if (newList || _c != _filledFor) FillHero(tracks);
    }

    private void FillHero(IReadOnlyList<Track> tracks)
    {
        _filledFor = _c;
        long ms = 0;
        foreach (var t in tracks) ms += t.DurationMs;
        var meta = new List<string>(3);
        if (_c.Owner.Length > 0) meta.Add(_c.Owner);
        if (tracks.Count > 0) meta.Add(Loc.Plural("Tracks", tracks.Count));
        else meta.Add(Loc.S(_c.Kind switch { CollectionKind.Liked => "Empty_Liked", CollectionKind.Playlist => "Empty_Playlist", _ => "Empty_Tracks" }));
        if (ms > 0) meta.Add(Fmt.Duration(ms));
        string? generated = _c.Kind switch { CollectionKind.Liked => Icons.HeartFill, CollectionKind.Flow => Icons.Flow, _ => null };
        _hero.Set(_c.KindLabel, _c.Title, string.Join(" · ", meta), _c.ArtUrl, generated);
        _hero.Actions.Visibility = Ui.Show(tracks.Count > 0);
    }

    private void Play(bool shuffle)
    {
        var items = _list.Items;
        if (items.Count == 0) return;
        if (shuffle && !Player.Shuffle) Player.SetShuffle(true);
        Player.Play(items, Player.Shuffle ? Random.Shared.Next(items.Count) : 0);
    }

    private async void Download()
    {
        _download.IsEnabled = false;
        await Menus.DownloadCollection(_c);
        _download.IsEnabled = Session.CanDownload;
    }

    // Owned by the account, or (no owner in the payload) in the user's playlists.
    private static bool IsOwn(Collection c)
    {
        if (c.Owner.Length > 0) return Session.AccountKnown && c.Owner == Session.Account.Name;
        foreach (var p in Library.Playlists)
            if (p.Id == c.Id) return true;
        return false;
    }
}
