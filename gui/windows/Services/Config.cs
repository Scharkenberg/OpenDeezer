// Settings and saved login in %APPDATA%\opendeezer: settings.json and arl.txt
// (raw UTF-8, no BOM, plaintext because the TUI reads the same file).

using System;
using System.IO;
using System.Reflection;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace OpenDeezer;

// AudioDevice "" = default.
internal sealed class Settings
{
    // The playback settings of a settings.json older than the engine's playback.json: read once
    // to seed that file (PlaybackOptions.LoadAsync), then null and no longer written.
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public int? Quality { get; set; }
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public bool? ReplayGain { get; set; }
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public bool? Gapless { get; set; }
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] public int? CrossfadeMs { get; set; }
    public bool CloseToTray { get; set; } = true;
    public string AudioDevice { get; set; } = "";
    public string Language { get; set; } = "";
    public double Volume { get; set; } = 1.0;
    // Restored window bounds in physical pixels; WinW 0 = never saved.
    public int WinX { get; set; }
    public int WinY { get; set; }
    public int WinW { get; set; }
    public int WinH { get; set; }
    public bool WinMax { get; set; }
    public bool TrayHintShown { get; set; }
    // The navigation pane's width in DIPs; 0 = the default.
    public double PaneWidth { get; set; }
    // SHA-256 of the %DEEZER_ARL% value active when the user signed in or out in
    // the app. That value is then ignored in favor of arl.txt; a new one is honored.
    public string IgnoredEnvArlHash { get; set; } = "";
    public bool WipeWebProfile { get; set; }
    // The account signed in last: its cached Home paints while the next sign-in runs.
    public string LastUserId { get; set; } = "";
    // Each list's sort (ListControls), by list.
    public System.Collections.Generic.Dictionary<string, int> ListSorts { get; set; } = new();
    // "" follows Windows, else "light" or "dark".
    public string Theme { get; set; } = "";
}

internal static class Config
{
    private static readonly object s_writeLock = new();

    public static string ConfigDir()
    {
        var appdata = Environment.GetEnvironmentVariable("APPDATA");
        if (string.IsNullOrEmpty(appdata)) appdata = Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData);
        return string.IsNullOrEmpty(appdata) ? "" : Path.Combine(appdata, "opendeezer");
    }

    private static string SettingsPath() => Path.Combine(ConfigDir(), "settings.json");

    private static string ArlPath() => Path.Combine(ConfigDir(), "arl.txt");

    public static string LoadArl(out bool fromEnv)
    {
        string env = EnvArl();
        fromEnv = env.Length > 0;
        return fromEnv ? env : LoadArlFile();
    }

    public static string LoadArlFile()
    {
        try { return File.Exists(ArlPath()) ? File.ReadAllText(ArlPath(), Encoding.UTF8).Trim() : ""; }
        catch (Exception ex)
        {
            AppLog.Write("read arl: " + ex.Message);
            return "";
        }
    }

    public static string EnvArl() => (Environment.GetEnvironmentVariable("DEEZER_ARL") ?? "").Trim();

    public static string EnvArlHash()
    {
        string env = EnvArl();
        return env.Length == 0 ? "" : Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(env)));
    }

    public static void DeleteArl()
    {
        try { File.Delete(ArlPath()); }
        catch (Exception ex) { AppLog.Write("delete arl: " + ex.Message); }
    }

    public static void SaveArl(string arl)
    {
        if (ConfigDir().Length == 0) return;
        try
        {
            Directory.CreateDirectory(ConfigDir());
            WriteAtomic(ArlPath(), arl);
        }
        catch (Exception ex) { AppLog.Write("save arl: " + ex.Message); }
    }

    public static Settings LoadSettings()
    {
        var s = new Settings();
        try
        {
            if (ConfigDir().Length > 0 && File.Exists(SettingsPath()))
                s = JsonSerializer.Deserialize(File.ReadAllText(SettingsPath()), WireJson.Default.Settings) ?? s;
        }
        catch (Exception ex) { AppLog.Write("read settings: " + ex.Message); }
        return Sanitize(s);
    }

    private static Settings Sanitize(Settings s)
    {
        if (Array.FindIndex(Loc.Languages, l => l.Tag == s.Language) < 0) s.Language = "";
        s.Volume = double.IsFinite(s.Volume) ? Math.Clamp(s.Volume, 0, 1) : 1.0;
        s.PaneWidth = double.IsFinite(s.PaneWidth) && s.PaneWidth >= Size.PaneMin && s.PaneWidth <= Size.PaneMax ? s.PaneWidth : 0;
        if (s.WinW < 0 || s.WinH < 0 || s.WinW > Size.SavedWindowMax || s.WinH > Size.SavedWindowMax)
        {
            s.WinW = 0;
            s.WinH = 0;
        }
        return s;
    }

    public static void SaveSettings(Settings s)
    {
        if (ConfigDir().Length == 0) return;
        try
        {
            Directory.CreateDirectory(ConfigDir());
            WriteAtomic(SettingsPath(), JsonSerializer.Serialize(s, WireJson.Default.Settings));
        }
        catch (Exception ex) { AppLog.Write("save settings: " + ex.Message); }
    }

    // Temp file, then swap: a crash or a concurrent writer never leaves a truncated file.
    internal static void WriteAtomic(string path, string text)
    {
        lock (s_writeLock)
        {
            string tmp = path + ".tmp";
            File.WriteAllText(tmp, text, new UTF8Encoding(false));
            File.Move(tmp, path, overwrite: true);
        }
    }

    public static string AppVersion()
    {
        var v = Assembly.GetExecutingAssembly().GetName().Version;
        return v == null ? "" : $"{v.Major}.{v.Minor}.{Math.Max(0, v.Build)}";
    }

    public static string DeviceName() =>
        string.IsNullOrEmpty(Environment.MachineName) ? "OpenDeezer for Windows" : Environment.MachineName;
}
