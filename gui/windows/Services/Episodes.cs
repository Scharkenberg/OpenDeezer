// Podcast episodes beyond their Track: each one's details (date, description), the account's
// bookmarks (where it stopped, played or not), marking them played, and the speed episodes play
// at. UI thread only.

using System;
using System.Collections.Generic;
using System.Runtime.CompilerServices;
using System.Threading.Tasks;

namespace OpenDeezer;

internal static class Episodes
{
    // The speeds Deezer's app cycles through, in that order.
    public static readonly double[] Speeds = [1.0, 1.25, 1.5, 1.75, 2.0, 0.5, 0.75];

    private static readonly ConditionalWeakTable<Track, Episode> s_info = new();
    private static IReadOnlyDictionary<string, EpisodeBookmark> s_marks = new Dictionary<string, EpisodeBookmark>();

    // The bookmarks were read again, or one was changed here.
    public static event Action? Changed;

    public static double Speed { get; private set; } = 1.0;

    public static void Note(Track t, Episode e) => s_info.AddOrUpdate(t, e);

    public static Episode? Info(Track t) => s_info.TryGetValue(t, out var e) ? e : null;

    public static EpisodeBookmark? Mark(string id) => s_marks.TryGetValue(id, out var m) ? m : null;

    // Another account, or none: the bookmarks were the last one's.
    public static void Reset() => s_marks = new Dictionary<string, EpisodeBookmark>();

    public static async Task RefreshAsync()
    {
        s_marks = await Task.Run(DeezerCore.EpisodeBookmarks);
        Changed?.Invoke();
    }

    public static async void SetPlayed(Track t, bool played)
    {
        if (await Task.Run(() => DeezerCore.DZMarkEpisodePlayed(t.Id, played ? 1 : 0)) == 0)
        {
            Toast.Error(Loc.S("Episode_MarkFailTitle"), Loc.S("Episode_MarkFailBody"));
            return;
        }
        var marks = new Dictionary<string, EpisodeBookmark>(s_marks) { [t.Id] = new EpisodeBookmark { Heard = played } };
        s_marks = marks;
        Changed?.Invoke();
    }

    // The next speed in Deezer's cycle, at once when an episode plays.
    public static void NextSpeed()
    {
        int i = Array.IndexOf(Speeds, Speed);
        Speed = Speeds[(i + 1) % Speeds.Length];
        double s = Speed;
        Engine.Post(() => DeezerCore.DZSetPlaybackSpeed(s));
    }

    // "1.25×"
    public static string SpeedText(double s) => s.ToString("0.##", Loc.Culture) + "×";
}
