// The installed apps live in %LOCALAPPDATA%\Programs\PurpleDisc\app: the desktop app (PurpleDisc.exe
// and its files), the terminal app (purpledisc-tui.exe), or both. The release's self-extractor runs
// "PurpleDisc.exe --install" from its temp folder; Settings > Apps runs "--uninstall" (from
// purpledisc-setup.exe, a copy of PurpleDisc.exe alone, when only the terminal app is installed).
// Both run before WinRT and the Windows App SDK are touched, and so does CleanUp at every start.
// While the terminal app is installed, the folder is on the user's PATH.

using System;
using System.Diagnostics;
using System.IO;
using System.Threading;
using Microsoft.Win32;

namespace PurpleDisc;

internal static class Installer
{
    // Set by an installer to make the running apps save and quit (MainWindow and the installed
    // terminal app wait on it).
    internal const string QuitEvent = "PurpleDisc.Quit";
    // A release installer downloaded by the app (Session.InstallUpdateAsync).
    internal const string DownloadPrefix = "PurpleDisc-", DownloadSuffix = "-windows-x64.exe";
    private const string SetupMutex = "PurpleDisc.Setup";
    private const string Name = "Purple Disc for Deezer", TuiName = "Purple Disc for Deezer (Terminal)";
    // SetupExe: PurpleDisc.exe alone, for Settings > Apps, when only the terminal app is installed. Not
    // "uninstall.exe": the folder is on PATH, so "uninstall" typed in a terminal would start it.
    private const string Exe = "PurpleDisc.exe", TuiExe = "purpledisc-tui.exe", SetupExe = "purpledisc-setup.exe";
    private const int Gui = 1, Tui = 2;
    private const string UninstallKey = @"Software\Microsoft\Windows\CurrentVersion\Uninstall\PurpleDisc";
    private const string Home = "https://github.com/Scharkenberg/PurpleDisc";
    private const uint ASFW_ANY = uint.MaxValue;

    private static string Root => Path.Combine(Folder("LOCALAPPDATA", Environment.SpecialFolder.LocalApplicationData), "Programs", "PurpleDisc");
    private static string AppDir => Path.Combine(Root, "app");
    private static string NewDir => AppDir + ".new";
    private static string OldDir => AppDir + ".old";
    private static string Shortcut(string name) =>
        Path.Combine(Folder("APPDATA", Environment.SpecialFolder.ApplicationData), @"Microsoft\Windows\Start Menu\Programs", name + ".lnk");

    // Only the installed copy updates itself; the zip and a dev build never do.
    internal static bool IsInstalled =>
        string.Equals(Path.TrimEndingDirectorySeparator(AppContext.BaseDirectory), AppDir, StringComparison.OrdinalIgnoreCase);

    internal static int Install()
    {
        using var setup = new Mutex(false, SetupMutex);
        Take(setup, Timeout.Infinite);
        try
        {
            Repair();
            string gui = Path.Combine(AppDir, Exe), tui = Path.Combine(AppDir, TuiExe);
            string? present = Array.Find([gui, Path.Combine(AppDir, SetupExe), tui], File.Exists);
            Version mine = FileVersion(Environment.ProcessPath!), installed = present != null ? FileVersion(present) : mine;
            if (installed > mine && !SetupText.Ask(string.Format(SetupText.T.Older, installed.ToString(3), mine.ToString(3)))) return 0;
            // An installer that carries the terminal app asks which apps; the default is what is installed now.
            int apps = Gui;
            if (File.Exists(Path.Combine(AppContext.BaseDirectory, TuiExe)))
            {
                int now = (File.Exists(gui) ? Gui : 0) | (File.Exists(tui) ? Tui : 0);
                apps = SetupText.Pick(mine.ToString(3), now == 0 ? Gui : now);
                if (apps == 0) return 0;
            }
            long bytes;
            try { bytes = Copy(AppContext.BaseDirectory, NewDir, apps); }
            catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
            {
                Delete(NewDir);
                SetupText.Tell(string.Format(SetupText.T.Failed, ex.Message));
                return 1;
            }
            StopRunning();
            if (!Swap())
            {
                Delete(NewDir);
                SetupText.Tell(string.Format(SetupText.T.Busy, AppDir));
                return 1;
            }
            Delete(OldDir);
            Register(apps, bytes);
            NativeMethods.AllowSetForegroundWindow(ASFW_ANY);
            // The terminal app, a console program, gets a console window of its own.
            Process.Start(new ProcessStartInfo((apps & Gui) != 0 ? gui : tui) { WorkingDirectory = AppDir })?.Dispose();
            return 0;
        }
        finally { setup.ReleaseMutex(); }
    }

    internal static int Uninstall()
    {
        using var setup = new Mutex(false, SetupMutex);
        Take(setup, Timeout.Infinite);
        try
        {
            StopRunning();
            foreach (var name in new[] { Name, TuiName })
                if (File.Exists(Shortcut(name))) File.Delete(Shortcut(name));
            Registry.CurrentUser.DeleteSubKeyTree(UninstallKey, throwOnMissingSubKey: false);
            SetPath(false);
            if (SetupText.Ask(SetupText.T.DeleteData))
            {
                Delete(Config.ConfigDir());
                Delete(Path.Combine(Folder("LOCALAPPDATA", Environment.SpecialFolder.LocalApplicationData), "purpledisc"));
            }
            // This exe runs from the folder: a hidden cmd removes it once this process is gone.
            Process.Start(new ProcessStartInfo("cmd.exe",
                $"/d /c for /l %i in (0,0,1) do @(tasklist /nh /fi \"pid eq {Environment.ProcessId}\" | find /i \"{Path.GetFileName(Environment.ProcessPath)}\" >nul || (rmdir /s /q \"{Root}\" & exit))")
            {
                CreateNoWindow = true,
                WorkingDirectory = Path.GetTempPath(),
            })?.Dispose();
            return 0;
        }
        finally { setup.ReleaseMutex(); }
    }

    // At every start of the installed app, unless an installer is at work: what an interrupted
    // install or a finished update left behind.
    internal static void CleanUp()
    {
        if (!IsInstalled) return;
        using var setup = new Mutex(false, SetupMutex);
        if (!Take(setup, 0)) return;
        try
        {
            Repair();
            foreach (var file in Directory.EnumerateFiles(Path.GetTempPath(), DownloadPrefix + "*" + DownloadSuffix))
            {
                try { File.Delete(file); }
                catch (Exception ex) when (ex is IOException or UnauthorizedAccessException) { }
            }
        }
        finally { setup.ReleaseMutex(); }
    }

    // A swap cut short: app.new is complete once app was moved to app.old, so it is finished;
    // app.old alone is rolled back. Whatever is left of app.new and app.old then goes.
    private static void Repair()
    {
        if (!Directory.Exists(AppDir) && Directory.Exists(OldDir))
            Directory.Move(Directory.Exists(NewDir) ? NewDir : OldDir, AppDir);
        Delete(NewDir);
        Delete(OldDir);
    }

    // The folder rename itself fails while anything runs from or holds the folder, so files are
    // never swapped one by one.
    private static bool Swap()
    {
        if (Directory.Exists(AppDir) && !Move(AppDir, OldDir)) return false;
        if (Move(NewDir, AppDir)) return true;
        if (Directory.Exists(OldDir)) Directory.Move(OldDir, AppDir);
        return false;
    }

    // Tried for about 10 s.
    private static bool Move(string from, string to)
    {
        for (int i = 0; ; i++)
        {
            try
            {
                Directory.Move(from, to);
                return true;
            }
            catch (Exception ex) when (ex is IOException or UnauthorizedAccessException)
            {
                if (i == 50) return false;
                Thread.Sleep(200);
            }
        }
    }

    // Asks a running Purple Disc to save and quit, and waits up to 10 s for it to be gone.
    private static void StopRunning()
    {
        if (!EventWaitHandle.TryOpenExisting(QuitEvent, out var quit)) return;
        using (quit) quit.Set();
        for (int i = 0; i < 100 && EventWaitHandle.TryOpenExisting(QuitEvent, out quit); i++)
        {
            quit.Dispose();
            Thread.Sleep(100);
        }
    }

    // The chosen apps' files. Without the desktop app, PurpleDisc.exe alone goes in as SetupExe.
    private static long Copy(string from, string to, int apps)
    {
        long bytes = 0;
        foreach (var file in Directory.EnumerateFiles(from, "*", SearchOption.AllDirectories))
        {
            string name = Path.GetRelativePath(from, file);
            if (name.Equals(TuiExe, StringComparison.OrdinalIgnoreCase))
            {
                if ((apps & Tui) == 0) continue;
            }
            else if ((apps & Gui) == 0)
            {
                if (!name.Equals(Exe, StringComparison.OrdinalIgnoreCase)) continue;
                name = SetupExe;
            }
            string target = Path.Combine(to, name);
            Directory.CreateDirectory(Path.GetDirectoryName(target)!);
            File.Copy(file, target, overwrite: true);
            bytes += new FileInfo(file).Length;
        }
        return bytes;
    }

    // A Start menu shortcut per installed app; the one of an app no longer chosen goes.
    private static void Register(int apps, long bytes)
    {
        string gui = Path.Combine(AppDir, Exe), tui = Path.Combine(AppDir, TuiExe);
        foreach (var (name, exe, app) in new[] { (Name, gui, Gui), (TuiName, tui, Tui) })
        {
            if ((apps & app) != 0) ShellLink.Save(Shortcut(name), exe, name);
            else if (File.Exists(Shortcut(name))) File.Delete(Shortcut(name));
        }
        bool hasGui = (apps & Gui) != 0;
        using var key = Registry.CurrentUser.CreateSubKey(UninstallKey);
        key.SetValue("DisplayName", Name);
        key.SetValue("DisplayVersion", BuildInfo.Version);
        key.SetValue("Publisher", "Scharkenberg");
        key.SetValue("DisplayIcon", hasGui ? gui : tui);
        key.SetValue("InstallLocation", Root);
        key.SetValue("UninstallString", $"\"{Path.Combine(AppDir, hasGui ? Exe : SetupExe)}\" --uninstall");
        SetPath((apps & Tui) != 0);
        key.SetValue("EstimatedSize", (int)(bytes / 1024));
        key.SetValue("NoModify", 1);
        key.SetValue("NoRepair", 1);
        key.SetValue("URLInfoAbout", Home);
        key.SetValue("HelpLink", Home);
    }

    // Adds the app folder to the user's PATH (HKCU\Environment, kept as REG_EXPAND_SZ or REG_SZ as found)
    // or takes it out, then tells running programs; terminals opened from then on find purpledisc-tui.
    private static void SetPath(bool add)
    {
        using var env = Registry.CurrentUser.CreateSubKey("Environment");
        string old = env.GetValue("Path", "", RegistryValueOptions.DoNotExpandEnvironmentNames) as string ?? "";
        var parts = new System.Collections.Generic.List<string>();
        foreach (var p in old.Split(';', StringSplitOptions.RemoveEmptyEntries))
            if (!Path.TrimEndingDirectorySeparator(p).Equals(AppDir, StringComparison.OrdinalIgnoreCase)) parts.Add(p);
        if (add) parts.Add(AppDir);
        string value = string.Join(';', parts);
        if (value == old) return;
        if (value.Length == 0) env.DeleteValue("Path", throwOnMissingValue: false);
        else env.SetValue("Path", value, old.Length > 0 ? env.GetValueKind("Path") : RegistryValueKind.ExpandString);
        NativeMethods.SendMessageTimeoutW(HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Environment", SMTO_ABORTIFHUNG, 1000, out _);
    }

    private const nint HWND_BROADCAST = 0xFFFF;
    private const uint WM_SETTINGCHANGE = 0x1A, SMTO_ABORTIFHUNG = 0x2;

    private static void Delete(string dir)
    {
        try { if (Directory.Exists(dir)) Directory.Delete(dir, recursive: true); }
        catch (Exception ex) when (ex is IOException or UnauthorizedAccessException) { }
    }

    // A holder that ended without releasing the mutex leaves it to the next one.
    private static bool Take(Mutex m, int timeoutMs)
    {
        try { return m.WaitOne(timeoutMs); }
        catch (AbandonedMutexException) { return true; }
    }

    private static Version FileVersion(string exe)
    {
        var v = FileVersionInfo.GetVersionInfo(exe);
        return new Version(v.FileMajorPart, v.FileMinorPart, v.FileBuildPart);
    }

    private static string Folder(string variable, Environment.SpecialFolder fallback) =>
        Environment.GetEnvironmentVariable(variable) is { Length: > 0 } v ? v : Environment.GetFolderPath(fallback);
}
