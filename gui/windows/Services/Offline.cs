// Offline music as the engine keeps it: the pinned tracks and collections, the fetch under way and
// offline mode. Pins belong to the signed-in account. UI thread only, but for Mode.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.UI.Dispatching;

namespace OpenDeezer;

// The engine calls Offline makes.
internal interface IOfflineEngine
{
    OfflineIdsReply Ids();
    OfflineCollectionsReply Collections();
    bool Mode();
    OfflineProgress Progress();
    // "" when the engine took it, else the reason.
    string Pin(string kind, string id, bool on);
    void SetMode(bool on);
}

internal sealed class NativeOfflineEngine : IOfflineEngine
{
    public OfflineIdsReply Ids() => DeezerCore.OfflineIds();
    public OfflineCollectionsReply Collections() => DeezerCore.OfflineCollections();
    public bool Mode() => DeezerCore.DZOfflineMode() == 1;
    public OfflineProgress Progress() => DeezerCore.OfflineProgress();
    public string Pin(string kind, string id, bool on) => DeezerCore.PinOffline(kind, id, on);
    public void SetMode(bool on) => DeezerCore.DZSetOfflineMode(on ? 1 : 0);
}

internal sealed class Offline
{
    // The engine lists single pinned tracks as one collection of this kind, with no id.
    public const string TrackKind = "track";

    private readonly IOfflineEngine _engine;
    private readonly Library _library;
    private readonly DispatcherQueueTimer _poll;
    internal HashSet<string> _ids = new();
    internal IReadOnlyList<OfflineJob> _jobs = [];
    // Stopped fetches already toasted, as kind:id:reason, while they stay listed so.
    internal readonly HashSet<string> _reported = new();
    // Pins and unpins go to the engine one at a time, in order; these (kind:id) are on the way, with
    // the value last asked for and how many calls are still to land.
    internal readonly Dictionary<string, (bool On, int Pending)> _busy = new();
    private Task _pinChain = Task.CompletedTask;
    private volatile bool _mode;
    private bool _active, _visible = true;
    private int _gen;

    public Offline(DispatcherQueue dq, Library library, IOfflineEngine? engine = null)
    {
        _engine = engine ?? new NativeOfflineEngine();
        _library = library;
        _poll = dq.CreateTimer();
        _poll.Interval = TimeSpan.FromMilliseconds(Timing.OfflineProgressMs);
        _poll.Tick += async (_, _) => await PollAsync();
        // Fetched online, a pinned collection is kept in step by the engine: new tracks are fetched.
        library.Stored += async (key, _) =>
        {
            var (kind, id) = Library.Split(key);
            if (!_mode && kind is "playlist" or "album" && IsPinned(kind, id)) await LoadAsync();
        };
    }

    // On while the engine requests nothing (no network at sign-in, or set by the user). Read off
    // the UI thread by the Library.
    public bool Mode => _mode;
    public IReadOnlyList<OfflineCollection> Collections { get; internal set; } = [];

    // The offline track ids, the collections or the mode changed.
    public event Action? Changed;
    // The fetch moved on, or a pin call started or ended.
    public event Action? Progress;

    public bool IsPinned(string trackId) => _ids.Contains(trackId);

    public bool IsPinned(string kind, string id) => Collections.Any(c => c.Kind == kind && c.Id == id);

    public OfflineJob? Job(string kind, string id) => _jobs.FirstOrDefault(j => j.Kind == kind && j.Id == id);

    // A pin or unpin of this item is on its way to the engine.
    public bool Busy(string kind, string id) => _busy.ContainsKey(Library.Key(kind, id));

    // The value last asked for while a pin or unpin of this item is on its way, else null.
    public bool? Requested(string kind, string id) => _busy.TryGetValue(Library.Key(kind, id), out var b) ? b.On : null;

    public async Task LoadAsync()
    {
        int g = ++_gen;
        var (ids, collections, mode, progress) = await Task.Run(() =>
            (_engine.Ids(), _engine.Collections(), _engine.Mode(), _engine.Progress()));
        if (g != _gen) return;
        bool idsChanged = !_ids.SetEquals(ids.Ids), listChanged = !Collections.SequenceEqual(collections.Collections), modeChanged = mode != _mode;
        _ids = new(ids.Ids);
        // An unchanged collection keeps its instance, so a tile grid edits only the tiles that moved.
        var before = Collections;
        Collections = collections.Collections.Select(c => before.FirstOrDefault(b => b == c) ?? c).ToList();
        _mode = mode;
        // The Songs list is built from the single pins; album and playlist replies differ between
        // the modes (online lists, pinned lists).
        if (idsChanged || listChanged) _library.Drop(TrackKind);
        if (modeChanged) _library.Drop("playlist", "album");
        SetJobs(progress);
        if (idsChanged || listChanged || modeChanged) Changed?.Invoke();
    }

    // "" when the engine took it, else the reason.
    public async Task<string> SetPinnedAsync(string kind, string id, bool on)
    {
        string key = Library.Key(kind, id);
        _busy[key] = (on, (_busy.TryGetValue(key, out var b) ? b.Pending : 0) + 1);
        Progress?.Invoke();
        var call = _pinChain.ContinueWith(_ => _engine.Pin(kind, id, on), TaskScheduler.Default);
        _pinChain = call;
        string error = await call;
        var (last, pending) = _busy[key];
        if (pending == 1) _busy.Remove(key);
        else _busy[key] = (last, pending - 1);
        await LoadAsync();
        Progress?.Invoke();
        return error;
    }

    public async Task SetModeAsync(bool on)
    {
        await Task.Run(() => _engine.SetMode(on));
        await LoadAsync();
    }

    // Hidden, the fetch is not polled; shown again, one reload catches up.
    public void SetVisible(bool visible)
    {
        if (visible == _visible) return;
        _visible = visible;
        if (!visible) _poll.Stop();
        else if (_active) _ = LoadAsync();
    }

    // Another account, or none: nothing of the last one shows.
    public void Reset()
    {
        _gen++;
        _active = false;
        _poll.Stop();
        _ids = new();
        _jobs = [];
        _reported.Clear();
        Collections = [];
        _mode = false;
        Changed?.Invoke();
        Progress?.Invoke();
    }

    // While a fetch runs: its progress only; the lists reload when tracks landed or it ended.
    internal async Task PollAsync()
    {
        int g = _gen;
        var progress = await Task.Run(_engine.Progress);
        if (g != _gen) return;
        bool moved = !progress.Active || progress.Jobs.Sum(j => j.Done) != _jobs.Sum(j => j.Done);
        SetJobs(progress);
        if (moved) await LoadAsync();
    }

    private void SetJobs(OfflineProgress progress)
    {
        _jobs = progress.Jobs;
        _active = progress.Active;
        if (_active && _visible && !_poll.IsRunning) _poll.Start();
        else if (!_active || !_visible) _poll.Stop();
        // A fetch that stopped (a full disk) says why, once while it stays listed with that reason.
        var stopped = new HashSet<string>();
        foreach (var j in _jobs)
        {
            if (j.Error.Length == 0) continue;
            string key = j.Kind + ":" + j.Id + ":" + j.Error;
            stopped.Add(key);
            if (_reported.Add(key)) Toast.Error(Loc.S("Offline_FetchStopped"), j.Error);
        }
        _reported.IntersectWith(stopped);
        Progress?.Invoke();
    }
}
