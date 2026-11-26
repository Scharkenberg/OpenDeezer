// Playback: what is playing, the transport, likes, offline copies, Connect and the
// sleep timer. Members and events are UI-thread only.

using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using Microsoft.UI.Dispatching;

namespace PurpleDisc;

// Values match DZState.
internal enum PlayState { Stopped, Loading, Playing, Paused, Error }

// Values match DZSetRepeat.
internal enum RepeatMode { Off, All, One }

internal enum QueueChangeKind { Reset, Insert, Remove, Move, Cursor }

// Index is the row inserted, removed or moved (To is its new row); -1 for Reset and Cursor.
internal readonly record struct QueueChange(QueueChangeKind Kind, int Index = -1, int To = -1);

internal sealed record PlayerFailure(string Title, string Message, bool Warning = false);

internal readonly record struct SleepStatus(bool Active, bool EndOfTrack, long RemainingMs);

internal sealed partial class Player
{
    private readonly DispatcherQueue _dq;
    private readonly Settings _settings;
    private readonly Library _library;
    internal readonly LatestValuePump<long> _seekPump;
    private readonly LatestValuePump<double> _volumePump;
    private readonly DispatcherQueueTimer _positionTimer;
    private ConnectDevice[] _devices = [];
    private long _lastSeekTick, _lastVolumeTick, _lastTransportTick;
    private int _transportPending;

    public Player(DispatcherQueue dq, Settings settings, Library library)
    {
        _dq = dq;
        _settings = settings;
        _library = library;
        Volume = settings.Volume;
        _seekPump = new(ms => Task.Run(() =>
        {
            DeezerCore.DZSeek(ms);
            Wake();
        }), () => _lastSeekTick = Environment.TickCount64);
        _volumePump = new(v => Task.Run(() =>
        {
            DeezerCore.DZSetVolume(v);
            Wake();
        }), () => _lastVolumeTick = Environment.TickCount64);
        _positionTimer = dq.CreateTimer();
        _positionTimer.Interval = TimeSpan.FromMilliseconds(Timing.PositionTickMs);
        _positionTimer.Tick += (_, _) => AdvancePosition();
    }

    // The queue row, or what the engine reports (a control-API play, the track on a Connect device).
    public Track? Current { get; internal set; }
    public PlayState State { get; internal set; }
    public long PositionMs { get; internal set; }
    public long DurationMs { get; internal set; }
    public double Volume { get; private set; }
    public bool Shuffle { get; private set; }
    public RepeatMode Repeat { get; private set; }
    // Connect device playing instead of this PC ("" when local); the name falls back to the address.
    public string CastAddress { get; private set; } = "";
    public string CastName { get; private set; } = "";
    public bool IsCasting => CastAddress.Length > 0;
    public string Format { get; internal set; } = "";
    public bool IsPreview { get; private set; }
    public IReadOnlyList<Track> Queue => _queue;
    public int QueueIndex => _queueIndex;
    public IReadOnlyList<ConnectDevice> Devices => _devices;
    // What another session of the account (the web player, a phone) last started.
    public Elsewhere Elsewhere { get; internal set; } = new();

    public event Action? TrackChanged;
    // The play state, shuffle, repeat and format.
    public event Action? StateChanged;
    public event Action? VolumeChanged;
    public event Action? CastChanged;
    public event Action? ElsewhereChanged;

    // Deezer refused the account's login since it signed in (read on the slow poll); told once.
    public event Action? SessionExpired;
    private bool _sessionExpired;
    public event Action? PositionChanged;
    public event Action<QueueChange>? QueueChanged;
    public event Action<string>? LikeChanged;
    public event Action<PlayerFailure>? Failed;

    private bool IsStoppedOrErrored => State is PlayState.Stopped or PlayState.Error;

    public async Task StartAsync()
    {
        var (finished, queueVersion) = await Task.Run(() => (DeezerCore.DZFinishedCount(), DeezerCore.DZQueueVersion()));
        _lastFinished = finished;
        _mirror.SetKnownVersion(queueVersion);
        Volume = _settings.Volume;
        VolumeChanged?.Invoke();
        StartPolling();
    }

    public void Reset()
    {
        bool casting = IsCasting;
        // In order on the mirror's chain: leave the device, then empty the engine queue.
        if (casting) _mirror.Run(_ => DeezerCore.DZDisconnectDevice());
        _mirror.Set([], -1, mirrored: false);
        StopPlayback();
        _queue = new();
        _natural = null;
        _queueIndex = -1;
        _queueSynced = false;
        _mirror.Forget();
        _engineNowId = "";
        _sessionExpired = false;
        _queueOwner = "";
        _saveTimer?.Stop();
        _resumeTrack = null;
        _resumeMs = 0;
        Current = null;
        CastAddress = "";
        CastName = "";
        QueueChanged?.Invoke(new(QueueChangeKind.Reset));
        TrackChanged?.Invoke();
        StateChanged?.Invoke();
        CastChanged?.Invoke();
    }

    // A queued DZStop records the listen; it must reach the engine before a restart.
    public void WaitForEngine(int timeoutMs)
    {
        try { _playChain.Wait(timeoutMs); }
        catch (AggregateException ex) { AppLog.Write("play chain: " + ex.GetBaseException().Message); }
    }

    public void Shutdown()
    {
        _saveTimer?.Stop();
        SaveQueue(wait: true);
        bool started = _pollCts != null;
        StopPolling();
        if (!started) return;
        if (!Task.Run(DeezerCore.DZStop).Wait(Timing.EngineStopWaitMs)) AppLog.Write("engine stop timed out");
    }

    // DZTogglePause only flips Playing and Paused: from Stopped or Error the row restarts.
    public void TogglePlayPause()
    {
        if (IsStoppedOrErrored && HasCurrent) PlayCurrent();
        else Send(DeezerCore.DZTogglePause);
    }

    public void Resume()
    {
        if (IsStoppedOrErrored && HasCurrent) PlayCurrent();
        else Send(DeezerCore.DZResume);
    }

    public void Pause() => Send(DeezerCore.DZPause);

    // A transport command, then a poll to show its effect.
    private void Send(Action call) => Engine.Post(() =>
    {
        call();
        Wake();
    });

    public void Seek(long ms)
    {
        SetPositionBase(ms, Environment.TickCount64);
        PositionMs = ms;
        PositionChanged?.Invoke();
        // The restored track is not loaded yet: its first play starts there.
        if (_resumeTrack != null) _resumeMs = ms;
        else _seekPump.Post(ms);
    }

    public void SetVolume(double volume)
    {
        Volume = volume;
        if (!IsCasting) _settings.Volume = volume;
        VolumeChanged?.Invoke();
        _volumePump.Post(volume);
    }

    public void SetRepeat(RepeatMode mode)
    {
        Repeat = mode;
        // On the play chain: DZSetRepeat drops the engine's preload before UpdatePreload re-arms it.
        SendTransport(() => DeezerCore.DZSetRepeat((int)mode));
        UpdatePreload();
        StateChanged?.Invoke();
    }

    public void SetGapless(bool on)
    {
        Engine.Post(() => DeezerCore.DZSetGapless(on ? 1 : 0));
        if (on) UpdatePreload();
    }

    public bool IsLiked(string id) => _library.IsLiked(id);

    // Callers offer it for tracks only (an id, not an episode).
    public async void Like(Track t, bool like)
    {
        var done = _library.SetLikedAsync(t, like);
        LikeChanged?.Invoke(t.Id);
        if (await done) return;
        LikeChanged?.Invoke(t.Id);
        Failed?.Invoke(new(Loc.S("Like_FailTitle"), Loc.S("Like_FailBody")));
    }

    public void RefreshLikes() => LikeChanged?.Invoke("");

    public async Task<IReadOnlyList<ConnectDevice>> DiscoverAsync()
    {
        var (devices, connected) = await Task.Run(() => (DeezerCore.DiscoverDevices(Timing.DiscoverMs), DeezerCore.ConnectedDevice()));
        _devices = devices;
        SetCast(connected, "");
        return devices;
    }

    // Can block for up to about 15 s. False when the device refused.
    public async Task<bool> ConnectAsync(ConnectDevice device)
    {
        var (ok, connected) = await Task.Run(() => (DeezerCore.DZConnectDevice(device.Addr) != 0, DeezerCore.ConnectedDevice()));
        SetCast(connected, ok ? device.Name : "");
        Wake();
        return ok;
    }

    public async void Disconnect()
    {
        await Task.Run(DeezerCore.DZDisconnectDevice);
        SetCast("", "");
        Wake();
    }

    private void SetCast(string address, string name)
    {
        if (address.Length > 0 && name.Length == 0)
            name = Array.Find(_devices, d => d.Addr == address)?.Name ?? "";
        if (address.Length > 0 && name.Length == 0) name = address;
        if (address == CastAddress && name == CastName) return;
        CastAddress = address;
        CastName = address.Length > 0 ? name : "";
        CastChanged?.Invoke();
    }

    public Task<SleepStatus> SleepStatusAsync() => Task.Run(() => new SleepStatus(
        DeezerCore.DZSleepTimerActive() != 0, DeezerCore.DZSleepTimerEndOfTrack() != 0, DeezerCore.DZSleepTimerRemainingMS()));

    public Task SetSleepTimerAsync(int minutes, bool endOfTrack) =>
        Task.Run(() => DeezerCore.DZSetSleepTimer(minutes, endOfTrack ? 1 : 0));

    public Task CancelSleepTimerAsync() => Task.Run(DeezerCore.DZCancelSleepTimer);
}
