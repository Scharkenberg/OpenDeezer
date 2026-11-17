// IEngineQueue on the engine DLL. Kept apart so the mirror links into the tests
// without DeezerCore or AppLog.

using System;
using System.Runtime.InteropServices;

namespace OpenDeezer;

internal sealed class NativeEngineQueue : IEngineQueue
{
    public static readonly NativeEngineQueue Instance = new();

    public long Version() => DeezerCore.DZQueueVersion();
    public int Index() => DeezerCore.DZQueueIndex();
    public void Set(ReadOnlySpan<byte> json) => DeezerCore.DZQueueSet(in MemoryMarshal.GetReference(json));
    public void Append(ReadOnlySpan<byte> json) => DeezerCore.DZQueueAppend(in MemoryMarshal.GetReference(json));
    public void SetIndex(int i) => DeezerCore.DZQueueSetIndex(i);
    public void Remove(int i) => DeezerCore.DZQueueRemove(i);
    public void Move(int from, int to) => DeezerCore.DZQueueMove(from, to);
    public void InsertNext(ReadOnlySpan<byte> json) => DeezerCore.DZQueueInsertNext(in MemoryMarshal.GetReference(json));

    public string CurrentId()
    {
        var q = DeezerCore.Queue();
        return q.Index >= 0 && q.Index < q.Tracks.Count ? q.Tracks[q.Index].Id : "";
    }
}

internal sealed partial class EngineQueueMirror
{
    /// <summary>The app's mirror: the native engine, failures logged to AppLog.</summary>
    public EngineQueueMirror() : this(NativeEngineQueue.Instance, AppLog.Write) { }
}
