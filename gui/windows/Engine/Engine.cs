// Helpers for calling the engine off the UI thread: fire-and-forget calls and
// a coalescing pump for values that change faster than the engine applies them.

using System;
using System.Threading.Tasks;

namespace PurpleDisc;

internal static class Engine
{
    public static void Post(Action call) =>
        Task.Run(call).ContinueWith(t => AppLog.Write("engine call failed: " + t.Exception?.GetBaseException().Message),
            TaskContinuationOptions.OnlyOnFaulted);
}

// One send in flight; values posted meanwhile collapse into one trailing send. UI thread only.
internal sealed class LatestValuePump<T>(Func<T, Task> send, Action? drained = null)
{
    private T _latest = default!;
    private bool _dirty;

    public bool Busy { get; private set; }

    public async void Post(T value)
    {
        _latest = value;
        if (Busy)
        {
            _dirty = true;
            return;
        }
        Busy = true;
        try
        {
            do
            {
                _dirty = false;
                await send(_latest);
            }
            while (_dirty);
        }
        finally
        {
            Busy = false;
        }
        drained?.Invoke();
    }
}
