// Display text for engine values.

using System.Globalization;

namespace PurpleDisc;

internal static class Fmt
{
    public static string Time(long ms)
    {
        long s = ms < 0 ? 0 : ms / 1000;
        return $"{s / 60}:{s % 60:D2}";
    }

    public static string Duration(long ms)
    {
        long min = (ms + 30_000) / 60_000;
        return min >= 60 ? Loc.Format("Duration_HoursMinutesFormat", min / 60, min % 60) : Loc.Plural("Minutes", min);
    }

    // "340 MB", "1.2 GB" (binary units, the reader's number format).
    public static string Bytes(long bytes)
    {
        double mb = bytes / 1048576.0;
        return mb < 1024 ? Loc.Format("Size_MbFormat", mb.ToString("0", Loc.Culture))
                         : Loc.Format("Size_GbFormat", (mb / 1024).ToString("0.#", Loc.Culture));
    }

    // An engine date ("2026-11-20", "2026-11-20 10:00:00") in the reader's short form.
    public static string Date(string date) =>
        System.DateTime.TryParse(date, CultureInfo.InvariantCulture, DateTimeStyles.None, out var d) ? d.ToString("d", Loc.Culture) : date;

    public static string Fans(long n) => n <= 0 ? "" : Loc.Plural("Fans", n);

    public static string Band(double hz) =>
        hz >= 1000 ? (hz / 1000).ToString("0.#", CultureInfo.InvariantCulture) + "k"
                   : ((long)hz).ToString(CultureInfo.InvariantCulture);

    public static string Preset(string id)
    {
        var parts = id.Split('-');
        for (int i = 0; i < parts.Length; i++)
            if (parts[i].Length > 0) parts[i] = char.ToUpperInvariant(parts[i][0]) + parts[i][1..];
        return string.Join(" ", parts);
    }

    // The chip keeps the codec alone (MP3, AAC, FLAC).
    public static string FormatChip(string format) => format.Split(" · ")[0];

    // A discovery client id as a device type. OS names stay verbatim.
    public static string ConnectType(string client) => client.ToLowerInvariant() switch
    {
        "tui" => Loc.S("ConnectType_Terminal"),
        "darwin" or "macos" => "macOS",
        "windows" => "Windows",
        "linux" or "gnome" or "kde" => "Linux",
        "" => Loc.S("ConnectType_Device"),
        _ => client,
    };
}
