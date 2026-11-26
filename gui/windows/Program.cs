// Entry point: a second launch hands its activation to the running instance and exits,
// so only one engine uses the config files, control port and discovery.

using System;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.Windows.AppLifecycle;

namespace PurpleDisc;

public static class Program
{
    [STAThread]
    private static int Main(string[] args)
    {
        WinRT.ComWrappersSupport.InitializeComWrappers();
        if (RedirectIfSecondary()) return 0;

        Application.Start(p =>
        {
            SynchronizationContext.SetSynchronizationContext(new SafeSyncContext(DispatcherQueue.GetForCurrentThread()));
            _ = new App();
        });
        return 0;
    }

    // The UI-thread context. An exception in an async void handler surfaces through Post
    // and would end the process without reaching Application.UnhandledException.
    private sealed class SafeSyncContext : SynchronizationContext
    {
        private readonly DispatcherQueue _dq;
        public SafeSyncContext(DispatcherQueue dq) => _dq = dq;

        public override void Post(SendOrPostCallback d, object? state) =>
            _dq.TryEnqueue(() =>
            {
                try { d(state); }
                catch (Exception ex) { App.ReportException(ex); }
            });

        public override void Send(SendOrPostCallback d, object? state)
        {
            if (_dq.HasThreadAccess) { d(state); return; }
            throw new NotSupportedException("Send to the UI thread is not supported.");
        }

        public override SynchronizationContext CreateCopy() => new SafeSyncContext(_dq);
    }

    private static bool RedirectIfSecondary()
    {
        AppInstance key;
        try { key = AppInstance.FindOrRegisterForKey("PurpleDisc.Main"); }
        catch (Exception ex) { AppLog.Write("single-instance check failed: " + ex.Message); return false; }

        if (key.IsCurrent)
        {
            key.Activated += (_, _) => App.OnSecondInstance();
            return false;
        }

        // Let the primary take the foreground when it restores its window.
        NativeMethods.AllowSetForegroundWindow(key.ProcessId);
        var activation = AppInstance.GetCurrent().GetActivatedEventArgs();
        IntPtr done = NativeMethods.CreateEventW(IntPtr.Zero, true, false, null);
        // RedirectActivationToAsync must not block this STA thread directly.
        Task.Run(() =>
        {
            try { key.RedirectActivationToAsync(activation).AsTask().Wait(); }
            catch (Exception ex) { AppLog.Write("redirect failed: " + ex.Message); }
            NativeMethods.SetEvent(done);
        });
        NativeMethods.CoWaitForMultipleObjects(0, 0xFFFFFFFF, 1, new[] { done }, out _);
        return true;
    }
}
