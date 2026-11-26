// The application and its services. The startup sign-in begins before the window is built.

using System;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.Windows.AppLifecycle;

namespace PurpleDisc;

public partial class App : Application
{
    // Created on the UI thread: Program constructs App inside Application.Start.
    private static readonly DispatcherQueue s_dq = DispatcherQueue.GetForCurrentThread();
    private static MainWindow? s_window;

    internal static Settings Settings { get; } = Config.LoadSettings();
    internal static Library Library { get; } = new();
    internal static Player Player { get; } = new(s_dq, Settings, Library);
    internal static Offline Offline { get; } = new(s_dq, Library);
    internal static Session Session { get; } = new(s_dq, Settings, Library, Player, Offline);
    internal static Navigator Navigator { get; } = new();

    internal static IntPtr WindowHandle => s_window == null ? IntPtr.Zero : WinRT.Interop.WindowNative.GetWindowHandle(s_window);

    public App()
    {
        Session.PrepareWebProfile(Settings);
        InitializeComponent();
        UnhandledException += (_, e) =>
        {
            e.Handled = true;
            ReportException(e.Exception);
        };
    }

    internal static void ReportException(Exception ex)
    {
        AppLog.Write("Unhandled: " + ex);
        var w = s_window;
        if (w != null && s_dq.TryEnqueue(() => w.ShowUnhandledError(ex))) return;
        MessageBoxW(IntPtr.Zero, Loc.S("Error_Unhandled") + "\n\n" + ex.Message, "Purple Disc", 0x10);
    }

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        Loc.SetLanguage(Settings.Language);
        Session.Start();
        s_window = new MainWindow();
        s_window.Activate();
    }

    // Program redirects a second launch here, on a worker thread.
    internal static void OnSecondInstance()
    {
        var w = s_window;
        s_dq.TryEnqueue(() => w?.RestoreWindow());
    }

    internal static bool Restart()
    {
        s_window?.PrepareExit();
        // The new process would otherwise find this one and redirect into it.
        try { AppInstance.GetCurrent().UnregisterKey(); }
        catch (Exception ex) { AppLog.Write("unregister key: " + ex.Message); }
        var reason = AppInstance.Restart("");
        AppLog.Write("restart failed: " + reason);
        s_window?.CancelExit();
        return false;
    }

    [LibraryImport("user32.dll", StringMarshalling = StringMarshalling.Utf16)]
    private static partial int MessageBoxW(IntPtr hWnd, string text, string caption, uint type);
}

// %APPDATA%\purpledisc\windows.log; at 512 KB it moves to windows.log.old.
internal static class AppLog
{
    private const long MaxBytes = 512 * 1024;
    private static readonly object s_lock = new();

    internal static void Write(string msg)
    {
        string dir = Config.ConfigDir();
        if (dir.Length == 0) return;
        string path = Path.Combine(dir, "windows.log");
        try
        {
            lock (s_lock)
            {
                Directory.CreateDirectory(dir);
                var fi = new FileInfo(path);
                if (fi.Exists && fi.Length > MaxBytes) File.Move(path, path + ".old", overwrite: true);
                File.AppendAllText(path, $"{DateTime.Now:yyyy-MM-dd HH:mm:ss} {msg}{Environment.NewLine}");
            }
        }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
        {
            Debug.WriteLine("windows.log: " + ex.Message);
        }
    }
}
