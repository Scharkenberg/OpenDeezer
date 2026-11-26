// The playback preferences that more than one place sets (the Settings page and the account
// flyout's quick settings). The engine keeps them in playback.json, shared with the terminal UI;
// these are its values as of the last read or set. Each setter applies, then tells every view.

using System;
using System.IO;
using System.Threading.Tasks;

namespace PurpleDisc;

internal static class PlaybackOptions
{
    public static event Action? Changed;

    public static int Quality { get; private set; } = 1;
    public static int CrossfadeMs { get; private set; }
    public static bool ReplayGain { get; private set; }
    public static bool Gapless { get; private set; }
    // Pause when another session of the account starts, and announce this app's starts.
    public static bool SingleDevice { get; private set; } = true;

    // The longest preset not over the engine's length (the terminal UI sets any length).
    public static int CrossfadeIndex => Math.Max(0, Array.FindLastIndex(Timing.CrossfadePresetsMs, ms => ms <= CrossfadeMs));

    public static string[] CrossfadeNames =>
        Array.ConvertAll(Timing.CrossfadePresetsMs, ms => ms == 0 ? Loc.S("Common_Off") : Loc.Plural("Seconds", ms / 1000));

    // Once the engine is up (its settings load has moved the terminal UI's older files into
    // playback.json). Before the engine kept them the values were the app's own, in settings.json:
    // while it still holds them they go to the engine (also over a playback.json the terminal UI
    // wrote first), and leave settings.json once the engine saved them and reads them back. With
    // neither, the app's defaults go.
    public static async Task LoadAsync()
    {
        var s = App.Session.Settings;
        bool held = s.Quality != null || s.CrossfadeMs != null || s.ReplayGain != null || s.Gapless != null;
        var old = (s.Quality ?? 1, s.CrossfadeMs ?? 0, s.ReplayGain ?? false, s.Gapless ?? false);
        string file = Path.Combine(Config.ConfigDir(), "playback.json");
        var (now, saved, single) = await Task.Run(() =>
        {
            if (held || !File.Exists(file))
            {
                DeezerCore.DZSetQuality(old.Item1);
                DeezerCore.DZSetCrossfadeMS(old.Item2);
                DeezerCore.DZSetReplayGain(old.Item3 ? 1 : 0);
                DeezerCore.DZSetGapless(old.Item4 ? 1 : 0);
            }
            return ((DeezerCore.DZQuality(), DeezerCore.DZCrossfadeMS(), DeezerCore.DZReplayGain() == 1, DeezerCore.DZGapless() == 1),
                File.Exists(file), DeezerCore.DZSingleDevice() == 1);
        });
        (Quality, CrossfadeMs, ReplayGain, Gapless) = now;
        SingleDevice = single;
        if (held && saved && now == old)
        {
            (s.Quality, s.CrossfadeMs, s.ReplayGain, s.Gapless) = (null, null, null, null);
            App.Session.SaveSettingsSoon();
        }
        Changed?.Invoke();
    }

    // Whether the account's plan plays quality tier `q` (0 Normal, 1 High, 2 HiFi); every tier
    // while the plan is not known yet.
    public static bool Allowed(int q)
    {
        var session = App.Session;
        return !session.AccountKnown || q == 0 || (q == 1 ? session.Account.CanHq : session.Account.CanHifi);
    }

    public static void SetQuality(int q)
    {
        if (q == Quality) return;
        Quality = q;
        Engine.Post(() => DeezerCore.DZSetQuality(q));
        Changed?.Invoke();
    }

    public static void SetCrossfade(int index)
    {
        int ms = Timing.CrossfadePresetsMs[index];
        if (ms == CrossfadeMs) return;
        CrossfadeMs = ms;
        Engine.Post(() => DeezerCore.DZSetCrossfadeMS(ms));
        Changed?.Invoke();
    }

    public static void SetReplayGain(bool on)
    {
        if (on == ReplayGain) return;
        ReplayGain = on;
        Engine.Post(() => DeezerCore.DZSetReplayGain(on ? 1 : 0));
        Changed?.Invoke();
    }

    public static void SetGapless(bool on)
    {
        if (on == Gapless) return;
        Gapless = on;
        App.Player.SetGapless(on);
        Changed?.Invoke();
    }

    public static void SetSingleDevice(bool on)
    {
        if (on == SingleDevice) return;
        SingleDevice = on;
        Engine.Post(() => DeezerCore.DZSetSingleDevice(on ? 1 : 0));
        Changed?.Invoke();
    }
}
