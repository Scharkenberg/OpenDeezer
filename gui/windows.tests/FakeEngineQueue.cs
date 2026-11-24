using System;
using System.Collections.Generic;
using System.Linq;
using System.Text.Json;
using System.Threading;

namespace OpenDeezer.Tests;

/// <summary>An in-memory engine queue following the Go engine's rules
/// (internal/engine/services.go engineQueue*, internal/queue/queue.go):
///  * Set: replace contents, cursor -> 0 (-1 when empty), version++ (also for the same contents).
///  * Append: add at the end, cursor unchanged, version++ when anything was added.
///  * SetIndex (AlignIndex): out of range or same index = no-op; never bumps.
///  * Remove: out of range or the current row = refused (no change, no bump);
///    otherwise the cursor follows its track, version++.
///  * Move: out of range or from == to = no-op; otherwise the cursor is remapped
///    like queue.Move, version++.
///  * InsertNext: insert at cursor+1 (0 when no cursor), cursor unchanged, version++.
/// Every call is recorded in <see cref="Calls"/>.</summary>
internal sealed class FakeEngineQueue : IEngineQueue
{
    private readonly object _lock = new();
    private readonly List<string> _ids = new();
    private int _index = -1;
    private long _version;
    private readonly List<string> _calls = new();

    public FakeEngineQueue(params string[] ids)
    {
        _ids.AddRange(ids);
        _index = ids.Length > 0 ? 0 : -1;
    }

    public List<string> Ids { get { lock (_lock) return new List<string>(_ids); } }
    public List<string> Calls { get { lock (_lock) return new List<string>(_calls); } }
    public int CallCount(string prefix) => Calls.Count(c => c.StartsWith(prefix, StringComparison.Ordinal));
    public string Current { get { lock (_lock) return _index >= 0 && _index < _ids.Count ? _ids[_index] : ""; } }

    // ---- IEngineQueue ----
    public long Version() { lock (_lock) return _version; }
    public int Index() { lock (_lock) return _index; }

    public void Set(ReadOnlySpan<byte> json)
    {
        var ids = ParseIds(Terminated(json));
        lock (_lock)
        {
            _calls.Add("Set " + string.Join(",", ids));
            _ids.Clear();
            _ids.AddRange(ids);
            _index = _ids.Count > 0 ? 0 : -1;
            _version++;
        }
    }

    public void SetIndex(int i)
    {
        lock (_lock)
        {
            _calls.Add("SetIndex " + i);
            if (i < 0 || i >= _ids.Count || i == _index) return;
            _index = i;
        }
    }

    public void Remove(int i)
    {
        lock (_lock)
        {
            _calls.Add("Remove " + i);
            if (i < 0 || i >= _ids.Count || i == _index) return; // refused
            _ids.RemoveAt(i);
            _version++;
            if (_ids.Count == 0) _index = -1;
            else if (_index > i) _index--;
            else if (_index >= _ids.Count) _index = _ids.Count - 1;
        }
    }

    public void Move(int from, int to)
    {
        lock (_lock)
        {
            _calls.Add($"Move {from} {to}");
            int n = _ids.Count;
            if (from < 0 || from >= n || to < 0 || to >= n || from == to) return;
            var t = _ids[from];
            _ids.RemoveAt(from);
            _ids.Insert(to, t);
            _version++;
            int x = _index;
            if (x == from) x = to;
            else
            {
                if (x > from) x--;
                if (x >= to) x++;
            }
            _index = x;
        }
    }

    public void InsertNext(ReadOnlySpan<byte> json)
    {
        var ids = ParseIds(Terminated(json));
        lock (_lock)
        {
            _calls.Add("InsertNext " + string.Join(",", ids));
            if (ids.Count == 0) return;
            _ids.InsertRange(_index + 1, ids);
            _version++;
        }
    }

    public void Append(ReadOnlySpan<byte> json)
    {
        var ids = ParseIds(Terminated(json));
        lock (_lock)
        {
            _calls.Add("Append " + string.Join(",", ids));
            if (ids.Count == 0) return;
            _ids.AddRange(ids);
            _version++;
        }
    }

    public string CurrentId() => Current;

    // The engine reads a C string: the span must end in its NUL.
    private static string Terminated(ReadOnlySpan<byte> json)
    {
        if (json.Length == 0 || json[^1] != 0) throw new ArgumentException("JSON is not NUL-terminated");
        return System.Text.Encoding.UTF8.GetString(json[..^1]);
    }

    // ---- things other parties do to the engine ----

    /// <summary>The engine auto-advances (natural finish): cursor only, no bump.</summary>
    public void AutoAdvance()
    {
        lock (_lock) if (_index + 1 < _ids.Count) _index++;
    }

    /// <summary>A remote controller (phone remote / control API) appends a track.</summary>
    public void RemoteAppend(string id)
    {
        lock (_lock) { _ids.Add(id); _version++; }
    }

    private static List<string> ParseIds(string json)
    {
        var outl = new List<string>();
        using var doc = JsonDocument.Parse(json);
        var r = doc.RootElement;
        if (r.ValueKind == JsonValueKind.Object) outl.Add(r.GetProperty("id").GetString() ?? "");
        else foreach (var o in r.EnumerateArray()) outl.Add(o.GetProperty("id").GetString() ?? "");
        return outl;
    }
}
