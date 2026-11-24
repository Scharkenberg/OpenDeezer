// Spacing, sizes, breakpoints and timings. Every number the UI uses is named here.

using Microsoft.UI.Xaml;

namespace OpenDeezer;

internal static class Space
{
    public const double XS = 4;
    public const double S = 8;
    public const double M = 12;
    public const double L = 16;
    public const double XXL = 32;
}

internal static class Pad
{
    public static readonly Thickness Page = new(Space.L, Space.M, Space.L, Space.L);
    // Narrow windows save height only: the side margins stay, so text never hugs the edge.
    public static readonly Thickness PageNarrow = new(Space.L, Space.S, Space.L, Space.M);
    public static readonly Thickness Bar = new(Space.M, Space.XS, Space.M, Space.XS);
    public static readonly Thickness Tile = new(Space.XS);
    public static readonly Thickness TileButton = new(Space.S);
    public static readonly Thickness Row = new(Space.S, 0, Space.S, 0);
    // The queue pane runs to the player bar; its rows bring their own inset.
    public static readonly Thickness Pane = new(Space.XS, Space.S, Space.XS, 0);
    public static readonly Thickness PaneHeader = new(Space.S, 0, 0, Space.XS);
    public static readonly Thickness Toast = new(Space.L, 0, Space.L, Space.L);
    public static readonly Thickness SectionTop = new(0, Space.L, 0, 0);
    public static readonly Thickness GroupTop = new(0, Space.M, 0, Space.XS);
    public static readonly Thickness Header = new(0, 0, 0, Space.S);
    // The hero's buttons under its text; an equalizer band's frequency under its slider.
    public static readonly Thickness HeroActions = new(0, Space.S, 0, 0);
    public static readonly Thickness BandLabel = new(0, Space.S, 0, 0);
    public static readonly Thickness InlineEnd = new(0, 0, Space.S, 0);
    // The title bar: buttons from the window edge, the icon between them and the name,
    // the search box kept off its neighbors, the trailing buttons just off the caption buttons.
    public static readonly Thickness TitleBarButton = new(Space.XS, 0, 0, 0);
    public static readonly Thickness TitleBarIcon = new(Space.S, 0, Space.S, 0);
    public static readonly Thickness TitleBarSearch = new(Space.M, 0, Space.M, 0);
    public static readonly Thickness TitleBarEnd = new(0, 0, Space.XS, 0);
}

internal static class Size
{
    public const double Row = 44;
    public const double RowArt = 36;
    public const double QueueRow = 40;
    public const double QueueArt = 32;
    public const double BarArt = 48;
    public const double HeroArt = 168;
    public const double HeroArtNarrow = 120;
    public const double ArtistArt = 200;
    public const double Tile = 140;
    // A stretched tile's picture decodes again in these steps as it widens.
    public const double TileDecodeStep = 32;
    // A skeleton's line of text (a title, a row's name).
    public const double SkeletonLine = 2 * Tile;
    // A scrolling label travels this far past its end, so the last glyph clears the clip.
    public const double MarqueeOvershoot = 2;
    public const double IconButton = 32;
    public const double RowButton = 28;
    public const double AlbumColumnMin = 1000;
    public const float HeroElevation = 32;
    public const double PlayButton = 36;
    public const double PlayRing = 20;
    public const double Avatar = 28;
    public const double AppIcon = 64;
    public const double MenuRow = 36;
    public const double SignInColumn = 360;
    // A state message's text.
    public const double MessageMax = 360;
    // A field that would otherwise grow with its content: a settings list, the title-bar search box.
    public const double FieldMax = 360;
    // The CDN image sizes artwork is requested at, ascending (Ui.Bitmap picks the smallest that
    // covers the decode size, else the largest).
    public static readonly int[] CdnArt = [56, 250, 500, 1000];
    // A page backdrop's picture is blurred: decoded small.
    public const double BackdropDecode = 120;
    // The track overview's artwork.
    public const double TrackArt = 280;
    // The title bar is as tall as the standard caption buttons; the search box keeps one
    // width and only narrows when the window leaves it less room.
    public const double TitleBar = 32;
    public const double TitleBarIcon = 16;
    // A field in the title bar: 2 px of air above and below.
    public const double TitleBarField = 28;
    public const double Toast = 560;
    public const float ToastRise = (float)Space.L;
    // The navigation pane: dragged between PaneMin and PaneMax (at most PaneMaxShare of the window).
    public const double Pane = 240;
    public const double PaneMin = 180;
    public const double PaneMax = 400;
    public const double PaneMaxShare = 0.4;
    // The collapsed pane: 46 centers its icons between the window edge and the page
    // (NavigationView's 48 leaves them 1.4 px nearer the edge).
    public const double Rail = 46;
    public const double Grip = 6;
    public const double QueuePane = 320;
    public const double VolumeSlider = 96;
    public const double FlyoutWidth = 280;
    // A ComboBox's own width beside its text: the left padding, the chevron's column and the border.
    public const double ComboChrome = 52;
    // A card's border (CardBorder in App.xaml), on each side.
    public const double CardBorder = 1;
    // The now-playing text never gets narrower than this; the bar drops other things first.
    public const double BarText = 140;
    public const double BarCenterMin = 220;
    // On a narrow bar the center takes this much before the sides even out (the seek slider's room).
    public const double BarCenterNarrow = 160;
    // The transport column's share of the bar against each side's one.
    public const int BarCenterWeight = 2;
    public const double BarCenterMax = 640;
    public const double ContentMax = 1000;
    public const double ReadingMax = 720;
    // How far ascenders and descenders may draw outside a text box (a scrolling label's clip).
    public const double TextOverhang = Space.S;
    // Followed lyrics keep the active line at this share of the viewport's height.
    public const double LyricsLineAt = 0.4;
    public const double QrCode = 220;
    public const double EqBand = 160;
    public const double TimeText = 40;
    public const double DurationColumn = 56;
    // The bar under an episode's title that shows how far the account got.
    public const double EpisodeProgress = 2;
    // The episode speed button in the player bar ("1.25×").
    public const double SpeedButton = 48;
    public const double FieldMin = 220;
    public const double WindowMinWidth = 480;
    public const double WindowMinHeight = 520;
    public const double WindowWidth = 1180;
    public const double WindowHeight = 760;
    // A saved window size past this is not a real one (a corrupt settings.json).
    public const int SavedWindowMax = 32000;
}

internal static class Count
{
    public const int TopTracks = 10;
    public const int ArtistTopTracks = 5;
    public const int SearchTopTracks = 5;
    public const int TileRows = 2;
    // Search's All filter shows one row of each kind of tile.
    public const int SearchTileRows = 1;
    // Typing searches from this many characters on; Enter searches any.
    public const int SearchMinChars = 2;
    public const int BackStack = 50;
    // Opened collections the Library keeps, and the tracks between them.
    public const int CachedCollections = 20;
    public const int CachedTracks = 10000;
    // Decoded pictures kept for reuse (Ui.Bitmap).
    public const int Bitmaps = 200;
    public const int RecentTiles = 20;
    public const int History = 100;
    // Recent searches the search box offers.
    public const int SearchHistory = 10;
    public const int StatsDays = 30;
    public const int RestorePasses = 20;
    public const int SkeletonTiles = 5;
    public const int SkeletonRows = 8;
    // The player bar's right-hand buttons at the Small width: lyrics, queue, Connect, volume and More.
    public const int SmallBarButtons = 5;
    // Failed plays in a row before the app stops skipping ahead.
    public const int PlayFailuresBeforeStop = 3;
    // The control API's port on the local network, and a generated access token's random bytes.
    public const int ControlPort = 7654;
    public const int TokenBytes = 16;
    public const int EqBands = 10;
}

// Slider and number-box steps and ranges.
internal static class Steps
{
    // The seek slider, in ms: arrow keys, Page Up/Down, and the value's grain.
    public const double Seek = 5000;
    public const double SeekPage = 30000;
    public const double SeekGrain = 1000;
    // Volume, in percent: arrow keys and Ctrl+Up/Down, then Page Up/Down.
    public const double Volume = 5;
    public const double VolumePage = 10;
    // A volume read back from the engine within this of the shown one (0..1) is the same.
    public const double VolumeSame = 0.005;
    // Equalizer bands and preamp, in dB: -EqMaxDb to +EqMaxDb.
    public const double EqMaxDb = 12;
    public const double EqDb = 0.5;
    // The stream cache's sizes, in GB (0 is off).
    public static readonly int[] CacheSizesGb = [0, 1, 2, 5, 10, 20];
    // How long an unplayed song stays in the cache, in days (0 is forever).
    public static readonly int[] CacheAgesDays = [0, 7, 30, 90, 180, 365];
}

internal enum WidthClass { Narrow, Small, Medium, Wide }

internal static class Breakpoint
{
    public const double Narrow = 720;
    public const double Medium = 900;
    public const double Wide = 1200;

    public static WidthClass Of(double width) =>
        width < Narrow ? WidthClass.Narrow : width < Medium ? WidthClass.Small : width < Wide ? WidthClass.Medium : WidthClass.Wide;
}

internal static class Timing
{
    // The engine poll: playing in view, playing hidden or loading, otherwise (and after repeated failures).
    public const int PollFastMs = 250;
    public const int PollSlowMs = 1000;
    public const int PollBackoffMs = 5000;
    public const int PollFailuresBeforeBackoff = 5;
    // Every Nth poll (every one at PollBackoffMs) also reads the slow fields and now-playing; such a
    // snapshot always reaches the UI thread.
    public const int SlowPollEvery = 4;
    // The position clock; it is re-based only when the engine's position is more than PositionDriftMs away.
    public const int PositionTickMs = 250;
    public const int PositionDriftMs = 250;
    public const int PrevRestartsAfterMs = 5000;
    public const int EngineStopWaitMs = 1500;
    public const int SmtcTimelineMs = 5000;
    public const int LyricsFollowPauseMs = 5000;
    public const int LibraryMaxAgeMinutes = 5;
    public const int SearchDebounceMs = 300;
    public const int ToastMs = 5000;
    public const int SettingsSaveDebounceMs = 500;
    // queue.json is written this long after the last queue, track or state change.
    public const int QueueSaveDebounceMs = 2000;
    public const int CookieCheckMs = 2000;
    public const int DiscoverMs = 700;
    public const int CrossFadeMs = 150;
    public const int BackdropFadeMs = 400;
    public const int ToastInMs = 200;
    public const int ToastOutMs = 150;
    public const int RevealMs = 80;
    public const int SleepRefreshMs = 1000;
    // How often an offline fetch under way is read.
    public const int OfflineProgressMs = 1000;
    // Home's greeting: morning until this hour, afternoon until the next, then evening.
    public const int MorningEndsHour = 12;
    public const int AfternoonEndsHour = 18;
    public static readonly int[] SleepMinutes = [5, 10, 20, 30, 45, 60];
    public static readonly int[] CrossfadePresetsMs = [0, 3000, 6000, 12000];
    // Deleting the web profile: the previous instance's msedgewebview2 can hold it for a moment.
    public const int WipeTries = 10;
    public const int WipeRetryMs = 200;
    // A label that does not fit scrolls at MarqueeSpeed px/s after MarqueeRampMs, holds at the end,
    // returns at MarqueeReturnSpeed px/s (at most MarqueeMaxReturnMs) and fades between the two.
    public const float MarqueeSpeed = 45f;
    public const float MarqueeReturnSpeed = 240f;
    public const int MarqueeRampMs = 350;
    public const int MarqueeHoldMs = 1100;
    public const int MarqueeMaxReturnMs = 800;
    public const int MarqueeFadeMs = 150;
}
