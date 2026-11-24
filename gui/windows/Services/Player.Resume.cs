// The queue kept across launches (queue.json): what plays, where, shuffle and repeat, each track
// with the list it was played from. Saved shortly after each change and at exit; restored at
// sign-in for the same account, stopped at the saved position, which the first play resumes.

using System;
using System.Collections.Generic;
using System.IO;
using System.Text.Json;
using System.Threading.Tasks;
using Microsoft.UI.Dispatching;

namespace OpenDeezer;

internal sealed class SavedQueue
{
    public string UserId { get; set; } = "";
    public List<SavedTrack> Tracks { get; set; } = new();
    public int Index { get; set; } = -1;
    public long PositionMs { get; set; }
    public bool Shuffle { get; set; }
    public int Repeat { get; set; }
    // With shuffle on: the play-order rows in their natural order.
    public List<int>? Natural { get; set; }
}

internal sealed class SavedTrack
{
    public Track Track { get; set; } = new();
    public bool Episode { get; set; }
    public string ListenType { get; set; } = "";
    public string ListenId { get; set; } = "";
}

internal sealed partial class Player
{
    // The account whose queue is kept; "" until one is restored, and after a reset.
    private string _queueOwner = "";
    private DispatcherQueueTimer? _saveTimer;
    // The restored track and where its first play starts.
    private Track? _resumeTrack;
    private long _resumeMs;
    // The queue writes, one after another.
    private Task _queueWrite = Task.CompletedTask;

    private static string QueuePath() => Path.Combine(Config.ConfigDir(), "queue.json");

    // Restores the account's saved queue when nothing is queued yet; from then on it is kept.
    public void Restore(string userId)
    {
        if (userId.Length == 0 || _queueOwner == userId) return;
        _queueOwner = userId;
        _saveTimer ??= CreateSaveTimer();
        if (_queue.Count > 0) return;
        SavedQueue? saved = null;
        try
        {
            if (File.Exists(QueuePath())) saved = JsonSerializer.Deserialize(File.ReadAllText(QueuePath()), WireJson.Default.SavedQueue);
        }
        catch (Exception ex) { AppLog.Write("read queue: " + ex.Message); }
        if (saved == null || saved.UserId != userId || saved.Index < 0 || saved.Index >= saved.Tracks.Count) return;

        var tracks = new List<Track>(saved.Tracks.Count);
        foreach (var s in saved.Tracks)
        {
            var t = s.Episode ? AsEpisode(s.Track) : s.Track;
            _listens.AddOrUpdate(t, new ListenContext(s.ListenType, s.ListenId));
            tracks.Add(t);
        }
        _queue = tracks;
        _queueIndex = saved.Index;
        Shuffle = saved.Shuffle;
        _natural = saved.Shuffle && saved.Natural is { } nat && nat.Count == tracks.Count ? nat.ConvertAll(i => tracks[i]) : null;
        if (Shuffle) SendTransport(() => DeezerCore.DZSetShuffle(1));
        if (saved.Repeat != (int)Repeat) SetRepeat((RepeatMode)Math.Clamp(saved.Repeat, 0, 2));
        SyncEngineQueue();
        var cur = _queue[_queueIndex];
        SetCurrent(cur, fromStart: true);
        // An episode resumes from its bookmark in the engine.
        if (!cur.IsEpisode && saved.PositionMs > 0 && (cur.DurationMs <= 0 || saved.PositionMs < cur.DurationMs))
        {
            _resumeTrack = cur;
            _resumeMs = saved.PositionMs;
            PositionMs = saved.PositionMs;
            SetPositionBase(PositionMs, Environment.TickCount64);
            PositionChanged?.Invoke();
        }
        QueueChanged?.Invoke(new(QueueChangeKind.Reset));
        StateChanged?.Invoke();
    }

    private static Track AsEpisode(Track t) => new()
    {
        Id = t.Id, Name = t.Name, ArtistId = t.ArtistId, ArtistLine = t.ArtistLine, AlbumName = t.AlbumName,
        ArtworkUrl = t.ArtworkUrl, DurationMs = t.DurationMs, IsExplicit = t.IsExplicit, IsEpisode = true,
    };

    private DispatcherQueueTimer CreateSaveTimer()
    {
        var timer = _dq.CreateTimer();
        timer.Interval = TimeSpan.FromMilliseconds(Timing.QueueSaveDebounceMs);
        timer.IsRepeating = false;
        timer.Tick += (_, _) => SaveQueue();
        QueueChanged += _ => SaveQueueSoon();
        TrackChanged += SaveQueueSoon;
        StateChanged += SaveQueueSoon;
        return timer;
    }

    private void SaveQueueSoon()
    {
        if (_queueOwner.Length == 0 || _saveTimer == null) return;
        _saveTimer.Stop();
        _saveTimer.Start();
    }

    // UI thread: at the end of the debounce and at exit. The snapshot is taken here and written off
    // the UI thread, in order; at exit (wait) before it returns.
    internal void SaveQueue(bool wait = false)
    {
        if (_queueOwner.Length == 0 || Config.ConfigDir().Length == 0 || IsCasting) return;
        var q = new SavedQueue
        {
            UserId = _queueOwner,
            Index = _queueIndex,
            PositionMs = _resumeTrack != null ? _resumeMs : PositionMs,
            Shuffle = Shuffle,
            Repeat = (int)Repeat,
        };
        var rows = new Dictionary<Track, int>(ReferenceEqualityComparer.Instance);
        foreach (var t in _queue)
        {
            var listen = ContextOf(t);
            rows.TryAdd(t, q.Tracks.Count);
            q.Tracks.Add(new SavedTrack { Track = t, Episode = t.IsEpisode, ListenType = listen.Type, ListenId = listen.Id });
        }
        if (_natural != null && _natural.Count == _queue.Count) q.Natural = _natural.ConvertAll(t => rows.GetValueOrDefault(t, 0));
        _queueWrite = _queueWrite.ContinueWith(_ =>
        {
            try
            {
                Directory.CreateDirectory(Config.ConfigDir());
                Config.WriteAtomic(QueuePath(), JsonSerializer.Serialize(q, WireJson.Default.SavedQueue));
            }
            catch (Exception ex) { AppLog.Write("save queue: " + ex.Message); }
        }, TaskScheduler.Default);
        if (wait) _queueWrite.Wait();
    }
}
