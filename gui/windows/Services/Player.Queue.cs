// The play queue, always in play order: shuffle materializes a shuffled order and keeps
// the natural one aside. The engine records the shuffle flag only (host-owned).

using System;
using System.Collections.Generic;
using System.Runtime.CompilerServices;
using System.Threading;
using System.Threading.Tasks;

namespace OpenDeezer;

internal sealed partial class Player
{
    private readonly Random _rng = new();
    private List<Track> _queue = new();
    private List<Track>? _natural;
    private int _queueIndex = -1;
    // The row after the tracks added with Add to queue since the list was played, as the web player
    // keeps it: the next addition goes there, ahead of the rest of the list. -1 for none.
    private int _addedEnd = -1;

    // DZPlay calls run on one ordered chain; a newer play supersedes a queued one.
    internal Task _playChain = Task.CompletedTask;
    internal int _playDispatchGen;
    private int _playFailures;
    internal int _playOkGen = -1;
    internal int _errHandledGen = -1;
    internal int _playsInFlight;
    private int _finishedAtPlay;

    // Queues without episodes are mirrored to the engine; episode queues advance from DZFinishedCount.
    private readonly EngineQueueMirror _mirror = new();
    private bool _queueSynced;
    private bool _pullingQueue;

    private bool HasCurrent => _queueIndex >= 0 && _queueIndex < _queue.Count;

    // The list each queued track was played from, for Deezer's listen log.
    private readonly ConditionalWeakTable<Track, ListenContext> _listens = new();

    internal ListenContext ContextOf(Track t) => _listens.TryGetValue(t, out var c) ? c : ListenContext.None;

    public void Play(IReadOnlyList<Track> list, int index, ListenContext listen)
    {
        if (index < 0 || index >= list.Count) return;
        foreach (var t in list) _listens.AddOrUpdate(t, listen);
        var start = list[index];
        if (Shuffle)
        {
            _natural = new List<Track>(list);
            _queue = ShuffledWithFirst(_natural, start);
            _queueIndex = 0;
        }
        else
        {
            _natural = null;
            _queue = new List<Track>(list);
            _queueIndex = index;
        }
        _playFailures = 0;
        _addedEnd = -1;
        SyncEngineQueue();
        PlayCurrent();
        QueueChanged?.Invoke(new(QueueChangeKind.Reset));
    }

    public void PlayAt(int index)
    {
        if (index < 0 || index >= _queue.Count || index == _queueIndex) return;
        MoveCursor(index);
        PlayCurrent();
        SyncEngineIndex();
    }

    public void Next()
    {
        if (_queue.Count == 0) return;
        if (_queueIndex + 1 < _queue.Count) MoveCursor(_queueIndex + 1);
        else if (Repeat == RepeatMode.All) MoveCursor(0);
        else return;
        PlayCurrent();
        SyncEngineIndex();
    }

    public void Prev()
    {
        if (_queue.Count == 0) return;
        if (HasCurrent && PositionMs >= Timing.PrevRestartsAfterMs && !IsStoppedOrErrored)
        {
            Seek(0);
            return;
        }
        if (_queueIndex > 0) MoveCursor(_queueIndex - 1);
        PlayCurrent();
        SyncEngineIndex();
    }

    private void MoveCursor(int index)
    {
        _queueIndex = index;
        QueueChanged?.Invoke(new(QueueChangeKind.Cursor));
    }

    private List<Track> ShuffledWithFirst(List<Track> src, Track? first)
    {
        var rest = new List<Track>(src);
        if (first != null) rest.Remove(first);
        for (int i = rest.Count - 1; i > 0; i--)
        {
            int j = _rng.Next(i + 1);
            (rest[i], rest[j]) = (rest[j], rest[i]);
        }
        if (first != null) rest.Insert(0, first);
        return rest;
    }

    private void PlayCurrent()
    {
        if (!HasCurrent) return;
        var t = _queue[_queueIndex];
        // The restored track's first play starts where it was left.
        long resumeMs = t == _resumeTrack ? _resumeMs : 0;
        _resumeTrack = null;
        _resumeMs = 0;
        SetCurrent(t, fromStart: true);
        if (resumeMs > 0)
        {
            PositionMs = resumeMs;
            SetPositionBase(resumeMs, Environment.TickCount64);
        }
        var next = PlaybackOptions.Gapless && !t.IsEpisode && HasDeterministicNext(out int n) ? _queue[n] : null;
        DispatchPlay(t, next, resumeMs);
        // Loading at once: the poll shows the engine's own state once the play is under way.
        if (State == PlayState.Loading) return;
        State = PlayState.Loading;
        UpdateClock();
        StateChanged?.Invoke();
    }

    private void SetCurrent(Track t, bool fromStart)
    {
        Current = t;
        if (fromStart)
        {
            SetPositionBase(0, Environment.TickCount64);
            PositionMs = 0;
            DurationMs = t.DurationMs;
        }
        TrackChanged?.Invoke();
        if (fromStart) PositionChanged?.Invoke();
    }

    private void DispatchPlay(Track t, Track? next, long resumeMs = 0)
    {
        int gen = ++_playDispatchGen;
        Interlocked.Increment(ref _playsInFlight);
        Chain(() =>
        {
            try
            {
                if (gen != Volatile.Read(ref _playDispatchGen)) return;
                // The poll watches the play resolve, from idle too.
                Wake();
                var listen = ContextOf(t);
                DeezerCore.DZSetListenContext(listen.Type, listen.Id);
                int r = t.IsEpisode ? DeezerCore.DZPlayEpisode(t.Id, t.DurationMs) : DeezerCore.DZPlay(t.Id, t.DurationMs);
                // Any finish counted up to now belongs to the replaced track.
                Volatile.Write(ref _finishedAtPlay, DeezerCore.DZFinishedCount());
                if (gen != Volatile.Read(ref _playDispatchGen)) return;
                if (r == DeezerCore.PlaySuperseded)
                {
                    // Another play took over: the streak of failed skips ends with it.
                    _dq.TryEnqueue(() => _playFailures = 0);
                    return;
                }
                if (r == 0)
                {
                    var err = DeezerCore.LastError();
                    _dq.TryEnqueue(() => OnPlayFailed(gen, err.Error, err.Network));
                    return;
                }
                Volatile.Write(ref _playOkGen, gen);
                if (resumeMs > 0) DeezerCore.DZSeek(resumeMs);
                if (next != null) PreloadNow(next);
            }
            finally { Interlocked.Decrement(ref _playsInFlight); }
        });
    }

    private void Chain(Action call) => _playChain = _playChain.ContinueWith(_ =>
    {
        try { call(); }
        finally { Wake(); }
    }, TaskScheduler.Default);

    // A shuffle or repeat change waits behind any play still resolving; until it has landed (and for
    // the hold after), the poll would read the engine's old value, so it adopts nothing meanwhile.
    private void SendTransport(Action call)
    {
        _transportPending++;
        Chain(() =>
        {
            try { call(); }
            finally
            {
                _dq.TryEnqueue(() =>
                {
                    _transportPending--;
                    _lastTransportTick = Environment.TickCount64;
                });
            }
        });
    }

    private void ChainIfCurrent(Action call)
    {
        int gen = _playDispatchGen;
        Chain(() =>
        {
            if (gen == Volatile.Read(ref _playDispatchGen)) call();
        });
    }

    // After any play still loading on the chain, so that track cannot start afterwards. Until its
    // DZStop ran, the engine still plays the stopped track.
    private void StopPlayback()
    {
        _playDispatchGen++;
        Interlocked.Increment(ref _stopsInFlight);
        Chain(() =>
        {
            try { DeezerCore.DZStop(); }
            finally { Interlocked.Decrement(ref _stopsInFlight); }
        });
    }

    // Stops queued on the chain whose DZStop has not run yet.
    internal int _stopsInFlight;

    // Playback stops (Don't recommend on the last track).
    public void Stop() => StopPlayback();

    // A manual Next has a track to go to.
    public bool HasNext => _queueIndex + 1 < _queue.Count || (Repeat == RepeatMode.All && _queue.Count > 1);

    private void OnPlayFailed(int gen, string err, bool network = false)
    {
        if (gen != _playDispatchGen) return;
        _errHandledGen = gen;
        // A lost network is not the track's fault: stop on it, keep the cursor and start the next
        // play where the bar stood.
        if (network)
        {
            StopPlayback();
            if (Current != null && !Current.IsEpisode && PositionMs > 0)
            {
                _resumeTrack = Current;
                _resumeMs = PositionMs;
            }
            Failed?.Invoke(new(Loc.S("NoInternet_Title"), Loc.S("NoInternet_Body")));
            return;
        }
        _playFailures++;
        string reason = err.Length > 0 ? err : Loc.S("Playback_FailBody");
        bool canSkip = _queueIndex + 1 < _queue.Count || (Repeat == RepeatMode.All && _queue.Count > 1);
        if (_playFailures >= Count.PlayFailuresBeforeStop || !canSkip)
        {
            _playFailures = 0;
            StopPlayback();
            Failed?.Invoke(new(Loc.S("Playback_FailTitle"), canSkip ? Loc.S("Playback_StoppedBody") : reason));
            return;
        }
        Failed?.Invoke(new(Loc.S("Playback_FailTitle"), reason + "\n" + Loc.S("Playback_Skipping"), Warning: true));
        Next();
    }

    private void Preload(int n)
    {
        var t = _queue[n];
        ChainIfCurrent(() => PreloadNow(t));
    }

    // On the play chain: preloads t, whose start a gapless promote reports with t's list.
    private void PreloadNow(Track t)
    {
        var listen = ContextOf(t);
        DeezerCore.DZSetListenContext(listen.Type, listen.Id);
        DeezerCore.DZPreload(t.Id, t.DurationMs);
    }

    private bool HasDeterministicNext(out int next)
    {
        next = -1;
        if (Repeat == RepeatMode.One || _queue.Count == 0) return false;
        int n;
        if (_queueIndex + 1 < _queue.Count) n = _queueIndex + 1;
        else if (Repeat == RepeatMode.All) n = 0;
        else return false;
        if (_queue[n].IsEpisode) return false;
        next = n;
        return true;
    }

    // The engine already swapped to the preloaded track of a queue it does not own:
    // follow without replaying, then warm the one after.
    private void AdvanceToPreloaded(int n)
    {
        MoveCursor(n);
        var t = _queue[_queueIndex];
        SetCurrent(t, fromStart: true);
        if (PlaybackOptions.Gapless && !t.IsEpisode && HasDeterministicNext(out int n2)) Preload(n2);
        SyncEngineIndex();
    }

    // An edit can change the next track: re-arm the gapless preload, or drop a stale one. On the
    // play chain, after any play in flight (whose start drops a preload); nothing once another play
    // has started.
    private void UpdatePreload()
    {
        if (!PlaybackOptions.Gapless) return;
        int gen = _playDispatchGen;
        Track? next = HasDeterministicNext(out int n) ? _queue[n] : null;
        Chain(() =>
        {
            if (gen != Volatile.Read(ref _playDispatchGen)) return;
            if (next != null) PreloadNow(next);
            else DeezerCore.DZClearPreload();
        });
    }

    public void SetShuffle(bool on)
    {
        if (on == Shuffle) return;
        // The engine only records the flag, for remotes; the order is ours.
        SendTransport(() => DeezerCore.DZSetShuffle(on ? 1 : 0));
        ApplyShuffle(on);
    }

    // Reorders the queue for the flag; the poll calls this alone for a remote's change.
    private void ApplyShuffle(bool on)
    {
        Shuffle = on;
        _addedEnd = -1;
        Track? cur = HasCurrent ? _queue[_queueIndex] : null;
        if (on)
        {
            _natural = new List<Track>(_queue);
            _queue = ShuffledWithFirst(_queue, cur);
            _queueIndex = cur != null ? 0 : -1;
        }
        else
        {
            _queue = new List<Track>(_natural ?? _queue);
            _natural = null;
            _queueIndex = cur != null ? _queue.IndexOf(cur) : -1;
            if (cur != null && _queueIndex < 0)
            {
                _queue.Insert(0, cur);
                _queueIndex = 0;
            }
        }
        if (_queue.Count > 0)
        {
            SyncEngineQueue();
            UpdatePreload();
        }
        StateChanged?.Invoke();
        QueueChanged?.Invoke(new(QueueChangeKind.Reset));
    }

    public void Move(int from, int to)
    {
        if (from < 0 || from >= _queue.Count || to < 0 || to >= _queue.Count || from == to) return;
        int idxBefore = _queueIndex;
        var t = _queue[from];
        _queue.RemoveAt(from);
        _queue.Insert(to, t);
        if (from == _queueIndex) _queueIndex = to;
        else if (from < _queueIndex && to >= _queueIndex) _queueIndex--;
        else if (from > _queueIndex && to <= _queueIndex) _queueIndex++;
        if (from < _addedEnd && to >= _addedEnd) _addedEnd--;
        else if (from >= _addedEnd && to < _addedEnd && _addedEnd >= 0) _addedEnd++;
        EditEngineQueue(e => e.Move(from, to), idxBefore);
        UpdatePreload();
        QueueChanged?.Invoke(new(QueueChangeKind.Move, from, to));
    }

    // Removing the playing row starts the one that slides into its place. The engine
    // refuses to remove its playing row, so that case is mirrored as a full replace.
    public void RemoveAt(int i)
    {
        if (i < 0 || i >= _queue.Count) return;
        bool wasCurrent = i == _queueIndex;
        int idxBefore = _queueIndex;
        var removed = _queue[i];
        _queue.RemoveAt(i);
        _natural?.Remove(removed);
        if (i < _addedEnd) _addedEnd--;
        if (_queue.Count == 0) _queueIndex = -1;
        else if (wasCurrent && _queueIndex >= _queue.Count) _queueIndex = _queue.Count - 1;
        else if (!wasCurrent && i < _queueIndex) _queueIndex--;
        QueueChanged?.Invoke(new(QueueChangeKind.Remove, i));
        if (!wasCurrent)
        {
            EditEngineQueue(e => e.Remove(i), idxBefore);
            UpdatePreload();
            return;
        }
        SyncEngineQueue();
        if (_queueIndex >= 0) PlayCurrent();
        else StopPlayback();
    }

    public void ClearQueue()
    {
        if (_queue.Count == 0) return;
        _queue = HasCurrent ? new List<Track> { _queue[_queueIndex] } : new List<Track>();
        _queueIndex = _queue.Count - 1;
        _addedEnd = -1;
        _natural = Shuffle ? new List<Track>(_queue) : null;
        SyncEngineQueue();
        UpdatePreload();
        QueueChanged?.Invoke(new(QueueChangeKind.Reset));
    }

    public void PlayNext(Track t) => PlayNext(new[] { t }, ContextOf(t));

    public void Enqueue(Track t) => Enqueue(new[] { t }, ContextOf(t));

    // Inserts the tracks right after the playing one; with nothing queued they play.
    public void PlayNext(IReadOnlyList<Track> tracks, ListenContext listen)
    {
        if (PlayIfIdle(tracks, listen)) return;
        int at = _queueIndex + 1;
        if (_addedEnd >= at) _addedEnd += tracks.Count;
        Insert(at, tracks, listen, _natural?.IndexOf(_queue[_queueIndex]) + 1 ?? 0);
    }

    // Inserts the tracks after those added before them (see _addedEnd).
    public void Enqueue(IReadOnlyList<Track> tracks, ListenContext listen)
    {
        if (PlayIfIdle(tracks, listen)) return;
        int at = Math.Clamp(_addedEnd, _queueIndex + 1, _queue.Count);
        _addedEnd = at + tracks.Count;
        Insert(at, tracks, listen, _natural?.Count ?? 0);
    }

    private void Insert(int at, IReadOnlyList<Track> tracks, ListenContext listen, int naturalAt)
    {
        foreach (var t in tracks)
            if (!_listens.TryGetValue(t, out _)) _listens.Add(t, listen);
        _queue.InsertRange(at, tracks);
        _natural?.InsertRange(Math.Clamp(naturalAt, 0, _natural.Count), tracks);
        bool one = tracks.Count == 1 && !tracks[0].IsEpisode;
        // One track crosses to the engine alone; more, or an episode, resend the queue.
        if (one && _queueSynced && at == _queueIndex + 1) _mirror.InsertNext(tracks[0], _queue, _queueIndex, _queueIndex);
        else if (one && _queueSynced && at == _queue.Count - 1) _mirror.Append(tracks[0], () => _dq.TryEnqueue(UpdatePreload));
        else SyncEngineQueue();
        UpdatePreload();
        QueueChanged?.Invoke(one ? new(QueueChangeKind.Insert, at) : new(QueueChangeKind.Reset));
    }

    // True when nothing was queued, so the tracks started a queue of their own (or there are none).
    private bool PlayIfIdle(IReadOnlyList<Track> tracks, ListenContext listen)
    {
        if (tracks.Count == 0) return true;
        if (HasCurrent) return false;
        Play(tracks, 0, listen);
        return true;
    }

    private static bool HasEpisode(List<Track> q) => q.Exists(t => t.IsEpisode);

    private void SyncEngineQueue()
    {
        _queueSynced = _queue.Count > 0 && !HasEpisode(_queue);
        _mirror.Set(_queue, _queueIndex, _queueSynced);
    }

    private void SyncEngineIndex()
    {
        if (_queueSynced) _mirror.SetIndex(_queueIndex);
    }

    private void EditEngineQueue(Action<IEngineQueue> engineEdit, int idxBefore)
    {
        if (_queueSynced) _mirror.Edit(engineEdit, _queue, _queueIndex, idxBefore);
    }

    // A remote controller edited the engine queue: adopt it, keeping local Track objects where ids match.
    private async void PullEngineQueue()
    {
        if (_pullingQueue) return;
        _pullingQueue = true;
        try
        {
            var reply = await Task.Run(DeezerCore.Queue);
            // A local edit raced: the next poll checks again.
            if (_mirror.Pending != 0 || reply.Version < 0) return;
            _mirror.SetKnownVersion(reply.Version);
            if (reply.Tracks.Count == 0 && HasEpisode(_queue)) return;
            var byId = new Dictionary<string, Track>();
            foreach (var t in _queue) byId.TryAdd(t.Id, t);
            var tracks = new List<Track>(reply.Tracks.Count);
            foreach (var t in reply.Tracks) tracks.Add(byId.GetValueOrDefault(t.Id, t));
            _queue = tracks;
            _mirror.Adopted(tracks);
            int idx = reply.Index;
            _queueIndex = idx >= 0 && idx < tracks.Count ? idx : (tracks.Count > 0 ? 0 : -1);
            _natural = Shuffle ? new List<Track>(_queue) : null;
            _addedEnd = -1;
            _queueSynced = tracks.Count > 0;
            UpdatePreload();
            QueueChanged?.Invoke(new(QueueChangeKind.Reset));
        }
        finally { _pullingQueue = false; }
    }
}
