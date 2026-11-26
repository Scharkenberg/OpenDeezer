// Stands in for gui\windows\App.xaml.cs: the same static services, but OnLaunched runs the
// measurements instead of building MainWindow or signing in.

using System;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;

namespace PurpleDisc;

public partial class App : Application
{
    private static readonly DispatcherQueue s_dq = DispatcherQueue.GetForCurrentThread();

    internal static Settings Settings { get; } = Config.LoadSettings();
    internal static Library Library { get; } = new();
    internal static Player Player { get; } = new(s_dq, Settings, Library);
    internal static Offline Offline { get; } = new(s_dq, Library);
    internal static Session Session { get; } = new(s_dq, Settings, Library, Player, Offline);
    internal static Navigator Navigator { get; } = new();
    internal static IntPtr WindowHandle => IntPtr.Zero;

    public App()
    {
        InitializeComponent();
        UnhandledException += (_, e) =>
        {
            e.Handled = true;
            Harness.Log("UNHANDLED " + e.Exception);
        };
    }

    internal static bool Restart() => false;

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        Loc.SetLanguage("en-US");
        Harness.Run();
    }
}

internal static class AppLog
{
    internal static void Write(string msg) => Harness.Log("APPLOG " + msg);
}
