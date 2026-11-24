using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using Xunit;

namespace OpenDeezer.Tests;

public class EngineQueueMirrorTests
{
    private readonly FakeEngineQueue _engine = new();
    private readonly List<string> _log = new();
    private readonly EngineQueueMirror _mirror;

    public EngineQueueMirrorTests()
    {
        _mirror = new EngineQueueMirror(_engine, m => { lock (_log) _log.Add(m); });
        _mirror.SetKnownVersion(_engine.Version()); // as Player.StartAsync does at startup
    }

    private static List<Track> Q(params string[] ids) => ids.Select(id => new Track { Id = id, Name = id }).ToList();

    private async Task DrainAsync()
    {
        var t = _mirror.Settled;
        Assert.Same(t, await Task.WhenAny(t, Task.Delay(5000))); // no hang
        Assert.Equal(0, _mirror.Pending);
    }

    /// <summary>Holds the chain until the returned gate is set, so later calls queue behind it.</summary>
    private ManualResetEventSlim BlockChain()
    {
        var gate = new ManualResetEventSlim(false);
        _mirror.Run(_ => gate.Wait(5000));
        return gate;
    }

    private async Task SetSynced(List<Track> q, int index)
    {
        _mirror.Set(q, index, mirrored: true);
        await DrainAsync();
        Assert.Equal(q.Select(t => t.Id), _engine.Ids);
        Assert.Equal(index, _engine.Index());
    }

    [Fact]
    public async Task Set_SupersededReplacesAreSkipped()
    {
        var gate = BlockChain();
        _mirror.Set(Q("a", "b"), 0, true);
        _mirror.Set(Q("c", "d"), 1, true);
        _mirror.Set(Q("e", "f", "g"), 2, true);
        Assert.Equal(4, _mirror.Pending);
        gate.Set();
        await DrainAsync();

        Assert.Equal(new[] { "Set e,f,g", "SetIndex 2" }, _engine.Calls);
        Assert.Equal(new[] { "e", "f", "g" }, _engine.Ids);
        Assert.Equal(2, _engine.Index());
    }

    [Fact]
    public async Task Set_SameIdsOnlyMovesTheCursor()
    {
        await SetSynced(Q("a", "b", "c"), 0);
        long v = _engine.Version();

        _mirror.Set(Q("a", "b", "c"), 2, true); // fresh Track objects, same ids
        await DrainAsync();

        Assert.Equal(1, _engine.CallCount("Set "));
        Assert.Equal("SetIndex 2", _engine.Calls[^1]);
        Assert.Equal(2, _engine.Index());
        Assert.Equal(v, _engine.Version());
    }

    [Fact]
    public async Task Set_SameIdsWhileFirstSetPending_DoesNotCancelIt()
    {
        var gate = BlockChain();
        _mirror.Set(Q("a", "b", "c"), 0, true);
        _mirror.Set(Q("a", "b", "c"), 1, true); // only a cursor move; must not supersede the pending set
        gate.Set();
        await DrainAsync();

        Assert.Equal(new[] { "a", "b", "c" }, _engine.Ids);
        Assert.Equal(1, _engine.Index());
        Assert.Equal(1, _engine.CallCount("Set "));
    }

    [Fact]
    public async Task Set_NotMirroredClearsEngineQueue_AndNextSetIsFull()
    {
        await SetSynced(Q("a", "b"), 1);
        _mirror.Set(Q("a", "b"), 1, mirrored: false);
        await DrainAsync();
        Assert.Empty(_engine.Ids);
        Assert.Equal(-1, _engine.Index());

        _mirror.Set(Q("a", "b"), 1, true); // ids cache was dropped: a full set again
        await DrainAsync();
        Assert.Equal(3, _engine.CallCount("Set "));
        Assert.Equal(new[] { "a", "b" }, _engine.Ids);
    }

    [Fact]
    public async Task Edit_RemoveAppliesGranularly_WhenEngineCursorMatches()
    {
        await SetSynced(Q("a", "b", "c", "d"), 1);
        long v = _engine.Version();

        _mirror.Edit(e => e.Remove(3), Q("a", "b", "c"), 1, 1);
        await DrainAsync();

        Assert.Equal(1, _engine.CallCount("Set ")); // no full replace
        Assert.Equal(new[] { "Remove 3", "SetIndex 1" }, _engine.Calls.TakeLast(2));
        Assert.Equal(new[] { "a", "b", "c" }, _engine.Ids);
        Assert.Equal(1, _engine.Index());
        Assert.Equal(v + 1, _engine.Version());
    }

    [Fact]
    public async Task Edit_FallbackFindsCursorFromAppCursor_WhenTrackQueuedTwice()
    {
        await SetSynced(Q("a", "b", "c", "b"), 2);
        _engine.AutoAdvance(); // the engine is on the last b before the poll saw it

        _mirror.Edit(e => e.Remove(0), Q("b", "c", "b"), 1, 2);
        await DrainAsync();

        Assert.Equal(new[] { "b", "c", "b" }, _engine.Ids);
        Assert.Equal(2, _engine.Index());
    }

    [Fact]
    public async Task Edit_MoveAppliesGranularly_CursorFollowsTrack()
    {
        await SetSynced(Q("a", "b", "c", "d"), 1);
        // GUI moves row 0 to the end; the playing "b" is now row 0.
        _mirror.Edit(e => e.Move(0, 3), Q("b", "c", "d", "a"), 0, 1);
        await DrainAsync();

        Assert.Equal(1, _engine.CallCount("Set "));
        Assert.Equal(new[] { "b", "c", "d", "a" }, _engine.Ids);
        Assert.Equal(0, _engine.Index());
        Assert.Equal("b", _engine.Current);
    }

    [Fact]
    public async Task Edit_InsertNextAppliesGranularly()
    {
        await SetSynced(Q("a", "b", "c"), 1);
        _mirror.InsertNext(new Track { Id = "x" }, Q("a", "b", "x", "c"), 1, 1);
        await DrainAsync();

        Assert.Equal(1, _engine.CallCount("Set "));
        Assert.Equal(new[] { "a", "b", "x", "c" }, _engine.Ids);
        Assert.Equal(1, _engine.Index());
    }

    [Fact]
    public async Task Append_AddsAtTheEndWithoutAFullReplace()
    {
        await SetSynced(Q("a", "b", "c"), 1);
        _mirror.Append(new Track { Id = "x" });
        await DrainAsync();

        Assert.Equal(1, _engine.CallCount("Set "));
        Assert.Equal(1, _engine.CallCount("Append x"));
        Assert.Equal(new[] { "a", "b", "c", "x" }, _engine.Ids);
        Assert.Equal(1, _engine.Index());
        Assert.Equal(_engine.Version(), _mirror.KnownVersion);
    }

    [Fact]
    public async Task Set_AfterAppend_SendsTheWholeQueueAgain()
    {
        await SetSynced(Q("a", "b"), 0);
        _mirror.Append(new Track { Id = "c" });
        _mirror.Set(Q("a", "b"), 0, mirrored: true);
        await DrainAsync();

        Assert.Equal(2, _engine.CallCount("Set "));
        Assert.Equal(new[] { "a", "b" }, _engine.Ids);
    }

    [Fact]
    public async Task Append_RunsItsFollowUpAfterTheAppend()
    {
        await SetSynced(Q("a", "b"), 1);
        int seen = -1;
        _mirror.Append(new Track { Id = "c" }, () => seen = _engine.Ids.Count);
        await DrainAsync();
        Assert.Equal(3, seen);
    }

    [Fact]
    public async Task QueueJson_KeepsNonAsciiAsUtf8()
    {
        var engine = new CapturingQueue();
        var mirror = new EngineQueueMirror(engine);
        mirror.Set(new List<Track> { new() { Id = "1", Name = "سلام 你好" } }, 0, mirrored: true);
        await mirror.Settled;
        Assert.Contains("سلام 你好", engine.LastSet);
    }

    private sealed class CapturingQueue : IEngineQueue
    {
        public string LastSet = "";
        public long Version() => 0;
        public int Index() => 0;
        public void Set(ReadOnlySpan<byte> json) => LastSet = System.Text.Encoding.UTF8.GetString(json);
        public void Append(ReadOnlySpan<byte> json) { }
        public void SetIndex(int i) { }
        public void Remove(int i) { }
        public void Move(int from, int to) { }
        public void InsertNext(ReadOnlySpan<byte> json) { }
        public string CurrentId() => "";
    }

    [Fact]
    public async Task Edit_FallsBackToFullReplace_WhenEngineCursorIsAhead()
    {
        await SetSynced(Q("a", "b", "c", "d"), 1);
        _engine.AutoAdvance(); // engine is already on "c"; the GUI hasn't caught up
        Assert.Equal(2, _engine.Index());

        // GUI (still on row 1) removes "d".
        _mirror.Edit(e => e.Remove(3), Q("a", "b", "c"), 1, 1);
        await DrainAsync();

        Assert.Equal(0, _engine.CallCount("Remove")); // granular edit not attempted
        Assert.Equal(2, _engine.CallCount("Set "));
        Assert.Equal(new[] { "a", "b", "c" }, _engine.Ids);
        Assert.Equal("c", _engine.Current); // kept on the track it is playing
        Assert.Equal(2, _engine.Index());
    }

    [Fact]
    public async Task Edit_FallsBackToFullReplace_WhenEngineCursorIsAhead_AndRowsShift()
    {
        await SetSynced(Q("a", "b", "c", "d"), 1);
        _engine.AutoAdvance(); // engine on "c" (row 2)

        // GUI (still on row 1) removes row 0: "c" becomes row 1 in the new model.
        _mirror.Edit(e => e.Remove(0), Q("b", "c", "d"), 0, 1);
        await DrainAsync();

        Assert.Equal(0, _engine.CallCount("Remove"));
        Assert.Equal(new[] { "b", "c", "d" }, _engine.Ids);
        Assert.Equal("c", _engine.Current);
        Assert.Equal(1, _engine.Index());
    }

    [Fact]
    public async Task Edit_RefusedRemoveOfPlayingRow_FallsBackToFullReplace()
    {
        await SetSynced(Q("a", "b", "c", "d"), 1);
        long v = _engine.Version();

        // GUI removes the playing row; the engine refuses (no change, no bump).
        _mirror.Edit(e => e.Remove(1), Q("a", "c", "d"), 1, 1);
        await DrainAsync();

        Assert.Contains("Remove 1", _engine.Calls);
        Assert.Equal(2, _engine.CallCount("Set "));
        Assert.Equal(new[] { "a", "c", "d" }, _engine.Ids);
        Assert.Equal(1, _engine.Index()); // "b" is gone: falls back to the GUI cursor
        Assert.Equal(v + 1, _engine.Version()); // only the fallback Set bumped it
    }

    [Fact]
    public async Task Edit_RefusedRemove_KeepsEngineOnItsTrack_WhenStillQueued()
    {
        await SetSynced(Q("a", "b", "c", "b"), 1);

        // GUI drops row 1 (a duplicate "b" stays queued at the end).
        _mirror.Edit(e => e.Remove(1), Q("a", "c", "b"), 1, 1);
        await DrainAsync();

        Assert.Contains("Remove 1", _engine.Calls);
        Assert.Equal(new[] { "a", "c", "b" }, _engine.Ids);
        Assert.Equal("b", _engine.Current); // engine stays on the track it is playing
        Assert.Equal(2, _engine.Index());
    }

    [Fact]
    public async Task Edit_InvalidatesSameIdsShortcut()
    {
        await SetSynced(Q("a", "b", "c"), 0);
        _mirror.Edit(e => e.Remove(2), Q("a", "b"), 0, 0);
        await DrainAsync();

        _mirror.Set(Q("a", "b", "c"), 0, true); // ids equal the LAST FULL set, but the engine was edited since
        await DrainAsync();
        Assert.Equal(2, _engine.CallCount("Set "));
        Assert.Equal(new[] { "a", "b", "c" }, _engine.Ids);
    }

    [Fact]
    public async Task KnownVersion_AdvancesWithOurOwnOps()
    {
        await SetSynced(Q("a", "b", "c"), 0);
        Assert.Equal(_engine.Version(), _mirror.KnownVersion);

        _mirror.Edit(e => e.Remove(2), Q("a", "b"), 0, 0);
        await DrainAsync();
        Assert.Equal(_engine.Version(), _mirror.KnownVersion);

        _mirror.SetIndex(1); // cursor-only op: version unchanged, still known
        await DrainAsync();
        Assert.Equal(_engine.Version(), _mirror.KnownVersion);
    }

    [Fact]
    public async Task KnownVersion_StaysStale_AfterRemoteEdit()
    {
        await SetSynced(Q("a", "b", "c"), 0);
        long known = _mirror.KnownVersion;

        _engine.RemoteAppend("r"); // phone remote edits before our next op
        _mirror.SetIndex(1);
        await DrainAsync();
        Assert.Equal(known, _mirror.KnownVersion);
        Assert.NotEqual(_engine.Version(), _mirror.KnownVersion); // the poll would pull

        _mirror.Edit(e => e.Remove(2), Q("a", "b", "r"), 1, 1); // our own content op on top
        await DrainAsync();
        Assert.Equal(known, _mirror.KnownVersion); // still stale: the remote edit isn't absorbed
        Assert.NotEqual(_engine.Version(), _mirror.KnownVersion);

        // The poll adopts the engine queue -> SetKnownVersion; later ops advance again.
        _mirror.SetKnownVersion(_engine.Version());
        _mirror.Edit(e => e.Remove(0), Q("b", "r"), 0, 1);
        await DrainAsync();
        Assert.Equal(_engine.Version(), _mirror.KnownVersion);
    }

    [Fact]
    public async Task OpGen_ChangesWhenQueued_AndWhenCompleted()
    {
        int g0 = _mirror.OpGen;
        var gate = BlockChain();
        int g1 = _mirror.OpGen;
        Assert.NotEqual(g0, g1);
        Assert.Equal(1, _mirror.Pending);

        _mirror.SetIndex(0);
        int g2 = _mirror.OpGen;
        Assert.NotEqual(g1, g2);
        Assert.Equal(2, _mirror.Pending);

        gate.Set();
        await DrainAsync();
        Assert.NotEqual(g2, _mirror.OpGen);
        Assert.Equal(0, _mirror.Pending);
    }

    [Fact]
    public async Task Pending_ReturnsToZero_AndChainSurvivesAFailingOp()
    {
        await SetSynced(Q("a", "b"), 0);
        _mirror.Run(_ => throw new InvalidOperationException("boom"));
        _mirror.SetIndex(1);
        await DrainAsync();

        Assert.Equal(0, _mirror.Pending);
        Assert.Equal(1, _engine.Index()); // the op after the failure still ran
        lock (_log) Assert.Contains(_log, m => m.Contains("boom"));
    }

    [Fact]
    public async Task Ops_ReachTheEngineInCallOrder()
    {
        await SetSynced(Q("a", "b", "c", "d", "e"), 0);
        var gate = BlockChain();
        _mirror.Edit(e => e.Remove(4), Q("a", "b", "c", "d"), 0, 0);
        _mirror.Edit(e => e.Remove(3), Q("a", "b", "c"), 0, 0);
        _mirror.Edit(e => e.Move(2, 1), Q("a", "c", "b"), 0, 0);
        gate.Set();
        await DrainAsync();

        Assert.Equal(new[] { "a", "c", "b" }, _engine.Ids);
        Assert.Equal(1, _engine.CallCount("Set "));
    }

    [Fact]
    public async Task Settled_CompletesImmediately_WhenIdle()
    {
        Assert.True(_mirror.Settled.IsCompleted);
        await DrainAsync();
    }
}
