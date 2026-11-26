// Keeps the engine queue identical to the GUI queue through one ordered chain of edits.
// No XAML or DLL dependencies: gui/windows.tests links this file with Track.cs.

using System;
using System.Buffers;
using System.Collections.Generic;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;

namespace PurpleDisc;

/// <summary>The engine queue (DZQueue*). Content edits bump Version, cursor moves don't; Set resets
/// the cursor to 0; removing the current row is refused; InsertNext inserts after the cursor;
/// Move keeps the cursor on its track.</summary>
internal interface IEngineQueue
{
    long Version();
    int Index();
    /// <summary>`json`: a NUL-terminated UTF-8 JSON array (the NUL is the span's last byte).</summary>
    void Set(ReadOnlySpan<byte> json);
    /// <summary>Appends a NUL-terminated UTF-8 JSON array; the cursor stays.</summary>
    void Append(ReadOnlySpan<byte> json);
    /// <summary>Align the cursor (no history entry; out of range = no-op).</summary>
    void SetIndex(int i);
    void Remove(int i);
    void Move(int from, int to);
    /// <summary>Inserts a NUL-terminated UTF-8 JSON track object after the cursor.</summary>
    void InsertNext(ReadOnlySpan<byte> json);
    string CurrentId();
}

// Members are called on the UI thread; queued operations run on the pool, one at a time, in order.
internal sealed partial class EngineQueueMirror
{
    private readonly IEngineQueue _engine;
    private readonly Action<string> _log;
    private Task _chain = Task.CompletedTask;
    private Task _settled = Task.CompletedTask;
    private int _pending;
    // Reused by every op (ops run one at a time). Non-ASCII text goes as UTF-8, not \u escapes.
    private readonly ArrayBufferWriter<byte> _json = new();
    private readonly Utf8JsonWriter _writer;
    private int _setGen;          // newest full replace wins
    private int _opGen;           // bumped when an op is queued and when it completes
    private long _known = -1;     // engine queue version after our own last op
    private string[]? _ids;       // ids of the last full set (null = unknown / edited since)

    internal EngineQueueMirror(IEngineQueue engine, Action<string>? log = null)
    {
        _engine = engine;
        _log = log ?? (_ => { });
        _writer = new Utf8JsonWriter(_json, new JsonWriterOptions { Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping });
    }

    /// <summary>Ops queued or running; interlocked so a test without a UI context can read it.</summary>
    public int Pending => Volatile.Read(ref _pending);
    /// <summary>Changes whenever an op is queued or finishes: a snapshot read under another value is stale.</summary>
    public int OpGen => Volatile.Read(ref _opGen);
    public long KnownVersion => Interlocked.Read(ref _known);
    public void SetKnownVersion(long v) => Interlocked.Exchange(ref _known, v);
    public void Adopted(List<Track> queue) => _ids = IdsOf(queue);
    public void Forget() => _ids = null;

    /// <summary>Completes once every op queued so far has finished and been counted down.</summary>
    internal Task Settled => _settled;

    /// <summary>Runs one engine queue operation on the ordered chain.</summary>
    public void Run(Action<IEngineQueue> op) => _settled = Task.WhenAll(_settled, RunOne(op));

    private async Task RunOne(Action<IEngineQueue> op)
    {
        Interlocked.Increment(ref _pending);
        Interlocked.Increment(ref _opGen);
        var t = _chain.ContinueWith(_ =>
        {
            // The known version advances only if no remote changed the queue before this op;
            // otherwise it stays stale so the poll still pulls that remote edit.
            long before = _engine.Version();
            op(_engine);
            long after = _engine.Version();
            Interlocked.CompareExchange(ref _known, after, before);
        }, TaskScheduler.Default);
        _chain = t;
        try { await t; } // resumes on the caller's context (the UI thread in the app)
        catch (Exception ex) { _log("engine queue op failed: " + ex.Message); }
        finally
        {
            Interlocked.Decrement(ref _pending);
            Interlocked.Increment(ref _opGen);
        }
    }

    /// <summary>Full replace and cursor. `mirrored` false clears the engine queue (episode queues
    /// advance in the GUI). Same ids as the last full set only move the cursor.</summary>
    public void Set(List<Track> queue, int index, bool mirrored)
    {
        if (mirrored && _ids != null && _ids.AsSpan().SequenceEqual(IdsOf(queue)))
        {
            // Must NOT bump _setGen: that set may still be pending on the chain.
            SetIndex(index);
            return;
        }
        _ids = mirrored ? IdsOf(queue) : null;
        var snapshot = new List<Track>(queue);
        int gen = ++_setGen;
        Run(e =>
        {
            if (gen != Volatile.Read(ref _setGen)) return; // a newer full replace is queued
            e.Set(QueueJson(mirrored ? snapshot : [])); // JSON built off the UI thread
            if (mirrored && index >= 0) e.SetIndex(index);
        });
    }

    public void SetIndex(int index)
    {
        if (index < 0) return;
        Run(e => e.SetIndex(index));
    }

    /// <summary>Adds `t` at the end; the cursor stays. `then` runs on the chain right after.</summary>
    public void Append(Track t, Action? then = null)
    {
        _ids = null; // content changed outside a full set
        Run(e =>
        {
            e.Append(QueueJson([t]));
            then?.Invoke();
        });
    }

    /// <summary>Granular edit, then the cursor re-asserted from the GUI model. Applied only if the engine
    /// cursor still equals `indexBefore` (an auto-advance moves it first, and the playing row cannot
    /// be removed); otherwise the whole queue is replaced, keeping the engine on its track.</summary>
    public void Edit(Action<IEngineQueue> engineEdit, List<Track> queueAfter, int index, int indexBefore)
    {
        _ids = null; // content changed outside a full set
        var snapshot = new List<Track>(queueAfter);
        Run(e =>
        {
            if (e.Index() == indexBefore)
            {
                long v0 = e.Version();
                engineEdit(e);
                if (e.Version() != v0) // applied
                {
                    if (index >= 0) e.SetIndex(index);
                    return;
                }
            }
            string curId = e.CurrentId();
            e.Set(QueueJson(snapshot));
            // From the app's cursor first: a track queued twice keeps the later copy the engine is on.
            int target = curId.Length > 0 ? snapshot.FindIndex(Math.Max(0, Math.Min(index, snapshot.Count)), t => t.Id == curId) : -1;
            if (target < 0 && curId.Length > 0) target = snapshot.FindIndex(t => t.Id == curId);
            if (target < 0) target = index;
            if (target >= 0) e.SetIndex(target); // the poll adopts it once now-playing matches
        });
    }

    /// <summary>`t` after the cursor, as an edit (see Edit).</summary>
    public void InsertNext(Track t, List<Track> queueAfter, int index, int indexBefore) =>
        Edit(e => e.InsertNext(Json(w => WriteTrack(w, t))), queueAfter, index, indexBefore);

    // The tracks as a JSON array for DZQueueSet and DZQueueAppend.
    private ReadOnlySpan<byte> QueueJson(List<Track> q) => Json(w =>
    {
        w.WriteStartArray();
        foreach (var t in q) WriteTrack(w, t);
        w.WriteEndArray();
    });

    // NUL-terminated UTF-8 JSON in the mirror's reused buffer: valid until the next op. Chain only.
    private ReadOnlySpan<byte> Json(Action<Utf8JsonWriter> write)
    {
        _json.ResetWrittenCount();
        _writer.Reset(_json);
        write(_writer);
        _writer.Flush();
        _json.GetSpan(1)[0] = 0;
        _json.Advance(1);
        return _json.WrittenSpan;
    }

    private static void WriteTrack(Utf8JsonWriter w, Track t)
    {
        w.WriteStartObject();
        w.WriteString("id", t.Id);
        w.WriteString("name", t.Name);
        w.WriteNumber("durationMs", t.DurationMs);
        w.WriteString("artistLine", t.ArtistLine);
        w.WriteString("artistId", t.ArtistId);
        w.WriteStartArray("artists");
        if (!string.IsNullOrEmpty(t.ArtistId) || !string.IsNullOrEmpty(t.ArtistLine))
        {
            w.WriteStartObject();
            w.WriteString("id", t.ArtistId);
            w.WriteString("name", t.ArtistLine);
            w.WriteEndObject();
        }
        w.WriteEndArray();
        w.WriteString("albumName", t.AlbumName);
        w.WriteString("artworkUrl", t.ArtworkUrl);
        w.WriteBoolean("explicit", t.IsExplicit);
        w.WriteEndObject();
    }

    public static string[] IdsOf(List<Track> q)
    {
        var a = new string[q.Count];
        for (int i = 0; i < a.Length; i++) a[i] = q[i].Id;
        return a;
    }
}
