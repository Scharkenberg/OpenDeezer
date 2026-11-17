// The engine poll: DZ* reads on a background loop into snapshots that the UI thread applies.
// The loop picks its next delay from the snapshot it just read: fast while playing in view, slow
// while playing hidden or loading, idle otherwise; every command wakes it, and a wake is never
// lost. Between snapshots the shown position advances on a UI timer while playing.

using System;
using System.Threading;
using System.Threading.Tasks;

namespace OpenDeezer;

// Slow: the fields after it were read on this tick, else they repeat the last read. NowTrack is the
// last now-playing track read (null when none plays). QueueOpGen is the mirror's OpGen, PlayOkGen the last play that
// started, PlaysInFlight the plays then resolving and DispatchGen the last play dispatched, all read
// before the engine's state.
internal readonly record struct EngineSnap(
    int State, long Pos, long Dur, int Finished, string NowId, Track? NowTrack, int QueueIndex, long QueueVersion, string LastError,
    bool Slow, string Format, bool Preview, RepeatMode Repeat, bool Shuffle, double Volume, string Connected,
    int QueueOpGen, long ReadTick, int PlayOkGen = -1, int PlaysInFlight = 0, int DispatchGen = 0)
{
    // Nothing for the UI thread to do after `sent`: equal but for the position while both play
    // (the UI clock carries it; slow snapshots bring the engine's to compare). An error snapshot
    // is never one: it goes through until it has been handled.
    public bool SameAs(EngineSnap sent)
    {
        if (State == (int)PlayState.Error) return false;
        bool playing = State == (int)PlayState.Playing && sent.State == (int)PlayState.Playing;
        return (playing || Pos == sent.Pos)
            && this with { Slow = false, ReadTick = 0, Pos = 0 } == sent with { Slow = false, ReadTick = 0, Pos = 0 };
    }
}

// The engine reads the poll makes.
internal interface IEngineReads
{
    int FinishedCount();
    int State();
    long PositionMs();
    long DurationMs();
    int QueueIndex();
    long QueueVersion();
    string LastError();
    long NowPlayingVersion();
    byte[] NowPlayingUtf8();
    string Format();
    bool IsPreview();
    RepeatMode Repeat();
    bool Shuffle();
    double Volume();
    string Connected();
}

internal sealed class NativeEngineReads : IEngineReads
{
    public int FinishedCount() => DeezerCore.DZFinishedCount();
    public int State() => DeezerCore.DZState();
    public long PositionMs() => DeezerCore.DZPositionMS();
    public long DurationMs() => DeezerCore.DZDurationMS();
    public int QueueIndex() => DeezerCore.DZQueueIndex();
    public long QueueVersion() => DeezerCore.DZQueueVersion();
    public string LastError() => DeezerCore.LastError();
    public long NowPlayingVersion() => DeezerCore.DZNowPlayingVersion();
    public byte[] NowPlayingUtf8() => DeezerCore.NowPlayingUtf8();
    public string Format() => DeezerCore.Format();
    public bool IsPreview() => DeezerCore.IsPreview();
    public RepeatMode Repeat() => DeezerCore.GetRepeat();
    public bool Shuffle() => DeezerCore.GetShuffle();
    public double Volume() => DeezerCore.DZVolume();
    public string Connected() => DeezerCore.ConnectedDevice();
}

// Reads and parses the now-playing JSON only when its version moved. Poller thread only.
internal sealed class EngineSnapReader(IEngineReads engine)
{
    private long _nowVersion = -1;
    private string _nowId = "";
    private Track? _nowTrack;
    private EngineSnap _last = new() { NowId = "", LastError = "", Format = "", Connected = "" };

    // `slow`: also read volume, repeat, shuffle, format, preview and the Connect device.
    public EngineSnap Read(int queueOpGen, int playOkGen, int playsInFlight, int dispatchGen, bool slow)
    {
        // Finished before State: a finish between the two reads must not look like a gapless swap.
        int finished = engine.FinishedCount();
        int state = engine.State();
        long pos = engine.PositionMs();
        long readTick = Environment.TickCount64;
        long dur = engine.DurationMs();
        int queueIndex = engine.QueueIndex();
        long queueVersion = engine.QueueVersion();
        // Read once when the error state begins.
        string lastError = state != (int)PlayState.Error ? "" : _last.State == state ? _last.LastError : engine.LastError();
        // The version moves exactly when the now-playing JSON does, a Connect device's included.
        long nowVersion = engine.NowPlayingVersion();
        if (nowVersion != _nowVersion)
        {
            _nowVersion = nowVersion;
            var now = Dto.Read(engine.NowPlayingUtf8(), WireJson.Default.Track);
            _nowId = now.Id;
            _nowTrack = _nowId.Length > 0 ? now : null;
        }
        var s = _last with
        {
            State = state, Pos = pos, Dur = dur, Finished = finished, NowId = _nowId, NowTrack = _nowTrack,
            QueueIndex = queueIndex, QueueVersion = queueVersion, LastError = lastError,
            Slow = slow, QueueOpGen = queueOpGen, ReadTick = readTick, PlayOkGen = playOkGen,
            PlaysInFlight = playsInFlight, DispatchGen = dispatchGen,
        };
        if (slow)
        {
            s = s with
            {
                Format = engine.Format(),
                Preview = (PlayState)state is PlayState.Loading or PlayState.Playing or PlayState.Paused && engine.IsPreview(),
                Repeat = engine.Repeat(),
                Shuffle = engine.Shuffle(),
                Volume = engine.Volume(),
                Connected = engine.Connected(),
            };
        }
        _last = s;
        return s;
    }
}

internal sealed partial class Player
{
    private CancellationTokenSource? _pollCts;
    private int _snapQueued;
    // Released by every command, so the poll reads its effect at once.
    private readonly SemaphoreSlim _wake = new(0, 1);
    private bool _visible = true;
    private int _lastFinished;
    internal string _engineNowId = "";
    private long _posBase, _posBaseTick;

    private void StartPolling()
    {
        if (_pollCts != null) return;
        var cts = new CancellationTokenSource();
        _pollCts = cts;
        Wake();
        _ = Task.Run(() => PollLoop(cts.Token));
    }

    public void StopPolling()
    {
        _pollCts?.Cancel();
        _pollCts = null;
        _positionTimer.Stop();
    }

    public void SetVisible(bool visible)
    {
        _visible = visible;
        UpdateClock();
        Wake();
    }

    private async Task PollLoop(CancellationToken ct)
    {
        var reader = new EngineSnapReader(new NativeEngineReads());
        EngineSnap? sent = null;
        int failures = 0, tick = 0, delay = Timing.PollBackoffMs;
        bool pending = false;
        while (!ct.IsCancellationRequested)
        {
            // A wake or read that met a snapshot still queued for the UI thread is retried shortly.
            int wait = failures >= Timing.PollFailuresBeforeBackoff ? Timing.PollBackoffMs : pending ? Timing.PollFastMs : delay;
            try { await _wake.WaitAsync(wait, ct).ConfigureAwait(false); }
            catch (OperationCanceledException) { return; }
            pending = Volatile.Read(ref _snapQueued) == 1;
            if (pending) continue;
            EngineSnap s;
            bool slow = ReadsSlow(delay, tick++);
            try { s = reader.Read(_mirror.OpGen, Volatile.Read(ref _playOkGen), Volatile.Read(ref _playsInFlight), Volatile.Read(ref _playDispatchGen), slow); }
            catch (Exception ex)
            {
                if (++failures == Timing.PollFailuresBeforeBackoff)
                    AppLog.Write($"poll failing ({ex.Message}); retrying every {Timing.PollBackoffMs} ms");
                continue;
            }
            if (failures >= Timing.PollFailuresBeforeBackoff) AppLog.Write("poll recovered");
            failures = 0;
            // A play in flight shows as Loading, whatever the engine still reports.
            delay = PollDelay(Volatile.Read(ref _playsInFlight) > 0 ? PlayState.Loading : (PlayState)s.State, Volatile.Read(ref _visible));
            // Slow snapshots always go through: they carry the settings a remote may have changed.
            if (!s.Slow && sent is { } last && s.SameAs(last)) continue;
            sent = s;
            Volatile.Write(ref _snapQueued, 1);
            if (!_dq.TryEnqueue(() =>
            {
                Volatile.Write(ref _snapQueued, 0);
                if (!ct.IsCancellationRequested) Apply(s);
            }))
                Volatile.Write(ref _snapQueued, 0);
        }
    }

    // At the idle cadence every poll reads the slow fields, so a remote's changes show while paused.
    internal static bool ReadsSlow(int delay, int tick) => delay == Timing.PollBackoffMs || tick % Timing.SlowPollEvery == 0;

    internal static int PollDelay(PlayState state, bool visible) => state switch
    {
        PlayState.Playing when visible => Timing.PollFastMs,
        PlayState.Playing or PlayState.Loading => Timing.PollSlowMs,
        _ => Timing.PollBackoffMs,
    };

    // The position timer runs only while playing with the window visible.
    private void UpdateClock()
    {
        bool run = State == PlayState.Playing && _visible && _pollCts != null;
        if (run && !_positionTimer.IsRunning) _positionTimer.Start();
        else if (!run) _positionTimer.Stop();
    }

    // Two commands can land before the poll takes the first wake: one is enough.
    private void Wake()
    {
        try { _wake.Release(); }
        catch (SemaphoreFullException) { }
    }

    private void SetPositionBase(long pos, long atTick)
    {
        _posBase = pos;
        _posBaseTick = atTick;
    }

    private void AdvancePosition()
    {
        long pos = _posBase + (Environment.TickCount64 - _posBaseTick);
        if (DurationMs > 0 && pos > DurationMs) pos = DurationMs;
        if (pos == PositionMs) return;
        PositionMs = pos;
        PositionChanged?.Invoke();
    }

    internal void Apply(EngineSnap s)
    {
        long now = Environment.TickCount64;
        bool stateChanged = false, positionChanged = false;
        var engineState = (PlayState)s.State;
        // DZPlay keeps the old stream Playing until the new one starts: only a real start clears the streak.
        bool currentPlayStarted = Volatile.Read(ref _playOkGen) == _playDispatchGen;
        if (engineState == PlayState.Playing && currentPlayStarted) _playFailures = 0;
        bool queueFresh = s.QueueOpGen == _mirror.OpGen && _mirror.Pending == 0;

        // DZFinishedCount bumps only for finishes of a queue the engine does not own.
        if (s.Finished != _lastFinished)
        {
            _lastFinished = s.Finished;
            // A finish counted while a user play is loading belongs to the track being replaced.
            bool stale = Volatile.Read(ref _playsInFlight) > 0 || s.Finished <= Volatile.Read(ref _finishedAtPlay);
            if (!stale)
            {
                if (Repeat == RepeatMode.One) PlayCurrent();
                else if (PlaybackOptions.Gapless && engineState == PlayState.Playing && HasDeterministicNext(out int n)) AdvanceToPreloaded(n);
                else Next();
            }
        }

        // While a play is in flight the engine's state, position, length and now-playing are the old
        // stream's (also for the same id played again, and for an intermediate play of quick skips):
        // a play resolving when the snapshot was read or now, or dispatched since the read.
        bool inFlight = s.PlaysInFlight > 0 || s.DispatchGen != _playDispatchGen || Volatile.Read(ref _playsInFlight) > 0;

        // Now playing as the engine sees it: auto-advance, control-API plays, a Connect device. The id
        // counts as seen only once its track is shown.
        if (!inFlight && s.NowId.Length > 0 && s.NowId != _engineNowId)
        {
            var t = s.NowId == Current?.Id ? Current : _queue.Find(x => x.Id == s.NowId) ?? s.NowTrack;
            if (t != null)
            {
                _engineNowId = s.NowId;
                if (t != Current) SetCurrent(t, fromStart: false);
            }
        }

        if (!inFlight && _queueSynced && queueFresh && s.NowId.Length > 0)
        {
            int eidx = s.QueueIndex;
            if (eidx >= 0 && eidx < _queue.Count && eidx != _queueIndex && _queue[eidx].Id == s.NowId)
            {
                MoveCursor(eidx);
                if (PlaybackOptions.Gapless && HasDeterministicNext(out int gn)) Preload(gn);
            }
        }

        // Position and length belong to the title shown only when the engine plays that title.
        bool otherTrack = inFlight || (s.NowId.Length > 0 && s.NowId != Current?.Id);
        var state = inFlight ? PlayState.Loading : engineState;
        if (state != State)
        {
            State = state;
            stateChanged = true;
            UpdateClock();
        }
        if (!otherTrack && s.Dur > 0 && s.Dur != DurationMs)
        {
            DurationMs = s.Dur;
            positionChanged = true;
        }
        // A snapshot is adopted only once no send is pending and it was read after the last one landed.
        // While playing, the clock carries the position: it is re-based only when it has drifted.
        if (!otherTrack && !_seekPump.Busy && s.ReadTick > _lastSeekTick)
        {
            long pos = s.Dur > 0 && s.Pos > s.Dur ? s.Dur : s.Pos;
            long engine = state == PlayState.Playing ? pos + (now - s.ReadTick) : pos;
            if (s.Dur > 0 && engine > s.Dur) engine = s.Dur;
            if (state != PlayState.Playing || stateChanged || Math.Abs(engine - PositionMs) > Timing.PositionDriftMs)
            {
                if (engine != PositionMs) positionChanged = true;
                SetPositionBase(pos, state == PlayState.Playing ? s.ReadTick : now);
                PositionMs = engine;
            }
        }

        long known = _mirror.KnownVersion;
        if (queueFresh && known >= 0 && s.QueueVersion >= 0 && s.QueueVersion != known)
            PullEngineQueue();

        // A stream failure, once per play, after the adoption above so a failed auto-advance skips from
        // its row. It is this play's only when this play had started before the snapshot was read.
        if (state == PlayState.Error && !IsCasting && s.PlayOkGen == _playDispatchGen && _errHandledGen != _playDispatchGen)
        {
            _errHandledGen = _playDispatchGen;
            if (_queueSynced && queueFresh && s.QueueIndex >= 0 && s.QueueIndex < _queue.Count && s.QueueIndex != _queueIndex)
                MoveCursor(s.QueueIndex);
            OnPlayFailed(_playDispatchGen, s.LastError);
        }

        if (s.Slow) stateChanged |= ApplySlow(s);
        if (positionChanged) PositionChanged?.Invoke();
        if (stateChanged) StateChanged?.Invoke();
    }

    internal bool ApplySlow(EngineSnap s)
    {
        bool changed = false;
        if (s.Format != Format)
        {
            Format = s.Format;
            changed = true;
        }
        if (s.Preview != IsPreview)
        {
            IsPreview = s.Preview;
            changed = true;
        }
        if (_transportPending == 0 && s.ReadTick > _lastTransportTick)
        {
            if (s.Repeat != Repeat)
            {
                Repeat = s.Repeat;
                changed = true;
            }
            // The engine only records the flag (host-owned): a remote's change reorders here, unsent.
            if (s.Shuffle != Shuffle && !IsCasting) ApplyShuffle(s.Shuffle);
        }

        if (!_volumePump.Busy && s.ReadTick > _lastVolumeTick && Math.Abs(Volume - s.Volume) > Steps.VolumeSame)
        {
            Volume = s.Volume;
            if (!IsCasting) _settings.Volume = s.Volume;
            VolumeChanged?.Invoke();
        }

        if (s.Connected != CastAddress) SetCast(s.Connected, "");
        return changed;
    }
}
