// The music kept for offline use, as tiles: albums, playlists and the songs pinned one by one, with
// the space they take, a shuffle of all of it, and the fetches that stopped, each with a Retry.
// Offline mode is switched here. A session started without a network opens here, with a Retry.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;

using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class OfflineView : View
{
    private static Offline Offline => App.Offline;

    internal readonly TileGrid<OfflineCollection> _grid = new(
        Tiles.Offline,
        o => CollectionView.Open(Collection.Of(o)),
        o => Menus.PlayCollection(Collection.Of(o)),
        // Disabled while its last pin or unpin is on the way, as the hero switch is.
        (items, o) =>
        {
            var remove = Menus.Item("Offline_Remove", Icons.Remove, () => Menus.SetOffline(o.Kind, o.Id, false));
            remove.IsEnabled = !App.Offline.Busy(o.Kind, o.Id);
            items.Add(remove);
        });
    private readonly ToggleSwitch _mode;
    internal readonly InfoBar _offline = new() { IsOpen = false, IsClosable = false, Severity = InfoBarSeverity.Informational };
    private readonly Button _retry, _signInAgain, _shuffle;
    private readonly TextBlock _storage = Ui.Text("", TextKind.SecondaryBody);
    private readonly StackPanel _failed = Ui.Stack(Space.S);
    // What the failure bars show, so a progress tick that changes none of them keeps them.
    private string _failedShown = "";
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    public OfflineView()
    {
        _mode = Ui.Switch(false, "Offline_Mode", on => _ = Offline.SetModeAsync(on));
        _retry = Ui.Button(Loc.S("Action_Retry"), Retry);
        _signInAgain = Ui.Button(Loc.S("Login_SignInAgain"), WebSignInView.SignInAgain);
        _state = new StateView(_grid.Root, SkeletonShape.Tiles, inset: false);
        _shuffle = Ui.Button(Loc.S("Offline_Shuffle"), Shuffle, Icons.Shuffle);
        _storage.VerticalAlignment = VerticalAlignment.Center;
        _scroller = Ui.Page(Ui.PageTitle(Loc.S("Nav_Offline"), Ui.LabeledSwitch(_mode, "Offline_Mode")),
            Ui.Stack(Space.L, _offline, Ui.Row(Space.M, _shuffle, _storage), _failed, _state.Root));
        _scroller.Loaded += (_, _) =>
        {
            Offline.Changed += Render;
            Offline.Progress += RenderFailed;
        };
        _scroller.Unloaded += (_, _) =>
        {
            Offline.Changed -= Render;
            Offline.Progress -= RenderFailed;
        };
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
        // Offline for no network, or because Deezer ended the session.
        _offline.Title = Loc.S(Session.Expired ? "Login_ExpiredTitle" : "Offline_YoureOffline");
        _offline.Message = Loc.S(Session.Expired ? "Offline_ExpiredBody" : "Offline_YoureOfflineBody");
        _offline.ActionButton = Session.Expired ? _signInAgain : _retry;
        if (Session.IsOffline && !Session.Expired) Session.WatchNetwork(Retry);
        Ui.Quietly(_mode, () => _mode.IsOn = Offline.Mode);
        // Offline mode is the engine's own while the session has no network.
        _mode.IsEnabled = !Session.IsOffline;
        KeepScroll(() => _grid.Update(Offline.Collections));
        if (Offline.Collections.Count == 0) _state.ShowEmpty(Icons.Pinned, Loc.S("Offline_Empty"));
        else _state.ShowContent();
        _shuffle.IsEnabled = Offline.Collections.Count > 0;
        RenderFailed();
        RenderStorage();
    }

    // "1.2 GB on this PC", "… MB" until it is known.
    private async void RenderStorage()
    {
        Ui.SetText(_storage, Loc.Format("Offline_StorageFormat", Loc.S("Offline_StorageUnknown")));
        var stats = await Task.Run(DeezerCore.MediaCacheStats);
        Ui.SetText(_storage, Loc.Format("Offline_StorageFormat", Fmt.Bytes(stats.PinnedBytes)));
    }

    // A fetch that stopped (an error, or tracks it could not get), with Retry, which pins it anew.
    private void RenderFailed()
    {
        var failing = Offline.Jobs.Where(j => j.Error.Length > 0 || j.Failed > 0)
            .Select(j => (Job: j, Title: FindCollection(Offline.Collections, j)?.Title ?? Loc.S("Offline_Songs"))).ToList();
        string shown = string.Join("\n", failing.Select(f => $"{f.Job.Kind}:{f.Job.Id}:{f.Job.Error}:{f.Job.Failed}:{f.Title}"));
        if (shown == _failedShown) return;
        _failedShown = shown;
        _failed.Children.Clear();
        foreach (var (job, title) in failing)
        {
            var bar = new InfoBar
            {
                IsOpen = true,
                IsClosable = false,
                Severity = InfoBarSeverity.Warning,
                Title = Loc.Format("Offline_FailedFormat", title),
                Message = job.Error.Length > 0 ? job.Error : Loc.Format("Offline_FailedTracksFormat", job.Failed),
                ActionButton = Ui.Button(Loc.S("Action_Retry"), () => Menus.SetOffline(job.Kind, job.Id, true)),
            };
            _failed.Children.Add(bar);
        }
    }

    private static OfflineCollection? FindCollection(IReadOnlyList<OfflineCollection> all, OfflineJob job)
    {
        foreach (var c in all)
            if (c.Kind == job.Kind && c.Id == job.Id) return c;
        return null;
    }

    // Every track kept offline, shuffled.
    private static async void Shuffle()
    {
        var collections = Offline.Collections;
        var tracks = await Task.Run(() =>
        {
            var seen = new HashSet<string>();
            var list = new List<Track>();
            foreach (var c in collections)
                foreach (var t in DeezerCore.OfflineTracks(c.Kind, c.Id).Tracks)
                    if (seen.Add(t.Id)) list.Add(t);
            return list;
        });
        if (tracks.Count == 0) return;
        if (!Player.Shuffle) Player.SetShuffle(true);
        Player.Play(tracks, Random.Shared.Next(tracks.Count), new ListenContext("downloads_page", ""));
    }

    private async void Retry()
    {
        if (!_retry.IsEnabled) return;
        _retry.IsEnabled = false;
        var r = await Session.RetryAsync();
        _retry.IsEnabled = true;
        if (Session.IsOffline) Toast.Error(Loc.S("Login_ErrorTitle"), Session.FailureText(r));
    }
}
