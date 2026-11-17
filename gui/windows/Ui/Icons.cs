// Segoe Fluent Icons glyphs, by name.

namespace OpenDeezer;

internal static class Icons
{
    public const string Play = "\uE768";
    public const string Pause = "\uE769";
    public const string Previous = "\uE892";
    public const string Next = "\uE893";
    public const string Shuffle = "\uE8B1";
    public const string Repeat = "\uE8EE";
    public const string RepeatOne = "\uE8ED";
    public const string Heart = "\uEB51";
    public const string HeartFill = "\uEB52";
    public const string Add = "\uE710";
    public const string Download = "\uE896";
    public const string Lyrics = "\uE7F0";
    public const string Queue = "\uE8FD";
    public const string Cast = "\uEC15";
    public const string Volume = "\uE767";
    public const string Mute = "\uE74F";
    public const string More = "\uE712";
    public const string Person = "\uE77B";
    public const string Sleep = "\uE708";
    public const string Settings = "\uE713";
    public const string Home = "\uE80F";
    public const string Back = "\uE72B";
    public const string Menu = "\uE700";
    public const string Playlists = "\uEA37";
    public const string Recent = "\uE81C";
    public const string Flow = "\uEC4F";
    public const string Charts = "\uE909";
    public const string Search = "\uE721";
    public const string Check = "\uE73E";
    public const string Remove = "\uE711";
    public const string ChevronRight = "\uE76C";
    public const string Equalizer = "\uE9E9";
    public const string Globe = "\uE774";
    public const string Speakers = "\uE7F5";
    public const string Crossfade = "\uE8AB";
    public const string Tray = "\uE73F";
    public const string Folder = "\uE8B7";
    public const string Disk = "\uEDA2";
    public const string Warning = "\uE7BA";
    public const string Update = "\uE895";
    public const string Phone = "\uE8EA";
    public const string Info = "\uE946";
    public const string Podcast = "\uE720";
    public const string Album = "\uE93C";
    public const string Computer = "\uE7F8";
    public const string Error = "\uE783";
    public const string Quality = "\uE8D6";
    public const string Rename = "\uE8AC";
    public const string Delete = "\uE74D";
    public const string Clock = "\uE916";
    public const string PlayNext = "\uE72A";
    public const string Terminal = "\uE756";
    public const string Devices = "\uE977";
    public const string Preset = "\uE81E";
    public const string Gain = "\uE995";
    public const string Headphones = "\uE7F6";
    public const string Code = "\uE943";
    // A track or collection kept for offline use; the Offline page.
    public const string Pinned = "\uEC61";
    public const string Offline = "\uE709";

    public static string Device(string client) => client.ToLowerInvariant() switch
    {
        "tui" => Terminal,
        "windows" or "darwin" or "macos" or "linux" or "gnome" or "kde" => Computer,
        "android" or "ios" => Phone,
        "" => Devices,
        _ => Cast,
    };
    public const string NoteText = "\u266A";
}
