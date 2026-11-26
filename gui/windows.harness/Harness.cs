// Measurements of the app's own controls in a window that is moved off screen and never
// activated. Each test logs one line per fact to HARNESS_OUT; screenshots land beside it.
// HARNESS_ONLY picks tests by name (comma separated). HARNESS_PICTURE, a profile picture URL as the engine reports it, turns
// on the avatar test, the only one that uses the network.

using System;
using System.Collections;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Reflection;
using System.Runtime.InteropServices.WindowsRuntime;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.UI;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;
using Windows.Graphics;
using Windows.Graphics.Imaging;
using WPoint = Windows.Foundation.Point;

namespace PurpleDisc;

public static class Program
{
    [STAThread]
    private static int Main()
    {
        WinRT.ComWrappersSupport.InitializeComWrappers();
        Application.Start(p =>
        {
            SynchronizationContext.SetSynchronizationContext(new SafeContext(DispatcherQueue.GetForCurrentThread()));
            var app = new App();
        });
        return 0;
    }

    // As the app's SafeSyncContext: an exception in an async void must not end the process.
    // Views that reach for the engine throw DllNotFoundException here; that is expected.
    private sealed class SafeContext(DispatcherQueue dq) : SynchronizationContext
    {
        public override void Post(SendOrPostCallback d, object? state) =>
            dq.TryEnqueue(() =>
            {
                try { d(state); }
                // No engine here, by design; any other exception is a failure.
                catch (Exception ex) when (ex is DllNotFoundException or EntryPointNotFoundException) { Harness.Log("  (async void threw " + ex.GetType().Name + ")"); }
                catch (Exception ex) { Harness.Log("  UNHANDLED " + ex.GetType().Name + ": " + ex.Message); }
            });

        public override SynchronizationContext CreateCopy() => new SafeContext(dq);
    }
}

internal static partial class Harness
{
    [System.Runtime.InteropServices.LibraryImport("user32.dll")]
    private static partial IntPtr SendMessageW(IntPtr hWnd, uint msg, nint wParam, IntPtr lParam);

    private static readonly string s_out = Environment.GetEnvironmentVariable("HARNESS_OUT") ?? "result.txt";
    private static readonly string s_dir = Path.GetDirectoryName(Path.GetFullPath(s_out))!;
    private static Window s_window = null!;
    private static Canvas s_canvas = null!;
    private static Grid s_box = new();

    public static void Log(string s) => File.AppendAllText(s_out, s + Environment.NewLine);

    public static async void Run()
    {
        try
        {
            s_window = new Window();
            s_canvas = new Canvas();
            s_window.Content = s_canvas;
            var aw = s_window.AppWindow;
            aw.IsShownInSwitchers = false;
            aw.MoveAndResize(new RectInt32(-30000, -30000, 1700, 1000));
            aw.Show(false);
            await Task.Delay(800);
            Log("scale " + s_canvas.XamlRoot?.RasterizationScale + ", theme " + Application.Current.RequestedTheme);
            // As MainWindow does: pictures decode for this display.
            if (s_canvas.XamlRoot is { } root) Ui.FollowScale(root);
            string only = Environment.GetEnvironmentVariable("HARNESS_ONLY") ?? "";
            foreach (var (name, test) in new (string, Func<Task>)[]
                { ("bar", TestBar), ("states", TestStates), ("tiles", TestTiles), ("rows", TestRows), ("fills", TestFills), ("stale", TestStale), ("transport", TestTransport), ("chrome", TestChrome), ("frame", TestFrame), ("resize", TestResize), ("backdrop", TestBackdrop),("layout", TestLayout), ("cards", TestCards), ("pane", TestPane), ("rail", TestRail),("pages", TestPages), ("offline", TestOffline), ("cost", TestCost), ("memory", TestMemory), ("fixes", TestFixes), ("avatar", TestAvatar) })
            {
                if (only.Length > 0 && !only.Split(',').Contains(name)) continue;
                Log("=== " + name);
                var clock = Stopwatch.StartNew();
                try { await test(); }
                catch (Exception ex) { Log("TEST FAILED " + ex); }
                Log($"--- {name}: {clock.Elapsed.TotalSeconds:0.0} s");
            }
        }
        catch (Exception ex) { Log("FATAL " + ex); }
        Log("=== done");
        // The window closes before the app exits, so nothing of it runs after its teardown.
        s_window.Close();
        Application.Current.Exit();
    }

    // The offline page, the collection hero's switch and a pinned row's mark, from stand-in pins
    // (the engine is not loaded here).
    private static async Task TestOffline()
    {
        var offline = App.Offline;
        offline.Collections = [
            new OfflineCollection { Kind = "album", Id = "oa", Title = "Pinned album", TrackCount = 10, Fetched = 3 },
            new OfflineCollection { Kind = Offline.TrackKind, TrackCount = 2, Fetched = 2 },
        ];
        offline._ids = ["o1"];
        offline._jobs = [new OfflineJob { Kind = "album", Id = "oa", Done = 3, Total = 10 }];
        App.Session.IsOffline = true;

        var page = new OfflineView();
        await Host(page.Root, 1000);
        page.Enter(null, false, null);
        await Settle(600);
        var subtitles = Descendants<TextBlock>(page._grid.Root).Select(t => t.Text).Where(s => s.Length > 0).ToList();
        Log($"offline page: info bar {(page._offline.IsOpen ? "open" : "closed")}, {page._grid.Root.ItemsSourceView?.Count ?? 0} tiles [{string.Join(" | ", subtitles)}] -> " +
            (page._offline.IsOpen && page._grid.Root.ItemsSourceView?.Count == 2 && subtitles.Contains(Loc.Format("Offline_ProgressFormat", 3, 10)) ? "ok" : "WRONG"));
        page.Leave();
        App.Session.IsOffline = false;

        App.Library.Store(Library.Key("album", "oa"), new TracksReply { Tracks = [new Track { Id = "o1", Name = "Pinned song", ArtistLine = "Artist", DurationMs = 1000 }, new Track { Id = "o2", Name = "Other", ArtistLine = "Artist", DurationMs = 1000 }] }, DateTime.UtcNow);
        foreach (double w in new[] { 1000.0, 480 })
        {
            var view = new CollectionView();
            await Host(view.Root, w, 700);
            view.Enter(Collection.Of(new Album { Id = "oa", Name = "Pinned album" }), false, null);
            await Settle(600);
            double right = view._offlineRow.TransformToVisual(s_box).TransformPoint(new WPoint(view._offlineRow.ActualWidth, 0)).X;
            var rows = Descendants<ListViewItem>(view._list.Root).Select(c => c.Tag).OfType<TrackRow>().ToList();
            string marks = string.Join(",", rows.Select(r => r.Track?.Id + ":" + (r._pinned.Visibility == Visibility.Visible ? "mark" : "-")));
            Log($"w={w}: hero switch {(view._offline.IsOn ? "on" : "off")}, progress '{view._offlineProgress.Text}', switch row ends at {right:0} of {w:0}; rows {marks} -> " +
                (view._offline.IsOn && view._offlineProgress.Text.Length > 0 && right <= w && marks.Contains("o1:mark") && marks.Contains("o2:-") ? "ok" : "WRONG"));
            if (w == 480) await Shot(s_box, "offline-hero-480");
            view.Leave();
        }
        {
            var view = new CollectionView();
            await Host(view.Root, 1000, 700);
            view.Enter(Collection.Of(new Album { Id = "oa", Name = "Pinned album" }), false, null);
            await Settle(600);
            string key = Library.Key("album", "oa");
            offline._busy[key] = (false, 1); // an unpin on its way; the album is still pinned
            view.RenderOffline();
            bool on = view._offline.IsOn, enabled = view._offline.IsEnabled;
            offline._busy.Remove(key);
            view.RenderOffline();
            Log($"the hero switch of a pinned album while its unpin is on its way: {(on ? "on" : "off")}, {(enabled ? "enabled" : "disabled")} -> " +
                (!on && !enabled ? "ok" : "WRONG"));
            view.Leave();
        }
        offline.Collections = [];
        offline._ids = [];
        offline._jobs = [];

        // The offline service over a stand-in engine, with a library of its own.
        var engine = new OfflineStandIn();
        var library = new Library();
        var o = new Offline(DispatcherQueue.GetForCurrentThread(), library, engine);
        int changed = 0;
        o.Changed += () => changed++;
        var tracks = new TracksReply { Tracks = [new Track { Id = "s1" }] };
        library.Store(Library.Key(Offline.TrackKind, ""), tracks, DateTime.UtcNow);
        library.Store(Library.Key("playlist", "p"), tracks, DateTime.UtcNow);
        engine.IdList = ["s1"];
        var changedKeys = new List<string>();
        library.Changed += changedKeys.Add;
        await o.LoadAsync();
        library.Changed -= changedKeys.Add;
        Log($"a page on screen hears its key dropped: Changed for {string.Join(",", changedKeys)} -> " +
            (changedKeys.SequenceEqual([Library.Key(Offline.TrackKind, "")]) ? "ok" : "WRONG"));
        bool songsDropped = library.Peek<TracksReply>(Library.Key(Offline.TrackKind, "")) == null;
        bool playlistKept = library.Peek<TracksReply>(Library.Key("playlist", "p")) != null;
        engine.ModeOn = true;
        await o.LoadAsync();
        bool playlistDropped = library.Peek<TracksReply>(Library.Key("playlist", "p")) == null;
        Log($"after a pin: the Songs list dropped {songsDropped}, a playlist kept {playlistKept}; after a mode change the playlist dropped {playlistDropped} -> " +
            (songsDropped && playlistKept && playlistDropped ? "ok" : "WRONG"));

        engine.Calls = 0;
        changed = 0;
        engine.Jobs = [new OfflineJob { Kind = "album", Id = "a", Done = 1, Total = 5 }];
        engine.Active = true;
        await o.PollAsync();
        int calls = engine.Calls;
        engine.Calls = 0;
        changed = 0;
        await o.PollAsync();
        int still = engine.Calls;
        engine.Calls = 0;
        engine.Jobs = [new OfflineJob { Kind = "album", Id = "a", Done = 2, Total = 5 }];
        await o.PollAsync();
        int moved = engine.Calls;
        // A tick whose count moved reads the progress and reloads the rest once (ids, collections, mode, progress).
        Log($"engine calls per tick while a fetch runs: done unchanged {still}, done moved {moved} (first tick {calls}); Changed with the id set unchanged {changed}x -> " +
            (still == 1 && moved == 5 && changed == 0 ? "ok" : "WRONG"));
        o.SetVisible(false);
        engine.Calls = 0;
        await Task.Delay(Timing.OfflineProgressMs * 2 + 200);
        int hidden = engine.Calls;
        o.SetVisible(true);
        await Task.Delay(300);
        int shown = engine.Calls;
        Log($"hidden for two poll periods while a fetch runs: {hidden} engine calls; shown again: {shown} (one reload) -> {(hidden == 0 && shown > 0 ? "ok" : "WRONG")}");

        engine.Jobs = [new OfflineJob { Kind = "album", Id = "a", Done = 2, Total = 5, Error = "disk full" }];
        engine.Active = false;
        await o.LoadAsync();
        bool reported = o._reported.Count == 1;
        engine.Jobs = [];
        await o.LoadAsync();
        Log($"a stopped fetch reported {reported}; its key kept after the job left the list: {o._reported.Count > 0} -> {(reported && o._reported.Count == 0 ? "ok" : "WRONG")}");

        engine.Order.Clear();
        var pin = o.SetPinnedAsync("album", "x", true);
        bool busy = o.Busy("album", "x");
        var unpin = o.SetPinnedAsync("album", "x", false);
        await Task.WhenAll(pin, unpin);
        Log($"pin then unpin at once reach the engine as {string.Join(",", engine.Order)}; the item busy while on the way {busy}, after {o.Busy("album", "x")} -> " +
            (string.Join(",", engine.Order) == "on,off" && busy && !o.Busy("album", "x") ? "ok" : "WRONG"));
        engine.Active = false;
        await o.LoadAsync();
    }

    private sealed class OfflineStandIn : IOfflineEngine
    {
        public int Calls;
        public List<string> IdList = [];
        public IReadOnlyList<OfflineJob> Jobs = [];
        public bool Active, ModeOn;
        public readonly List<string> Order = [];
        public OfflineIdsReply Ids() { Calls++; return new OfflineIdsReply { Ids = [.. IdList] }; }
        public OfflineCollectionsReply Collections() { Calls++; return new OfflineCollectionsReply(); }
        public bool Mode() { Calls++; return ModeOn; }
        public OfflineProgress Progress() { Calls++; return new OfflineProgress { Active = Active, Jobs = Jobs }; }
        public string Pin(string kind, string id, bool on)
        {
            // The pin resolves its list over the network: slower than the unpin after it.
            if (on) Thread.Sleep(300);
            lock (Order) Order.Add(on ? "on" : "off");
            return "";
        }
        public void SetMode(bool on) => ModeOn = on;
    }

    // Working set and managed heap after every page was visited with 3 000-track data; printed per
    // run, so a change's effect on memory shows next to the last run's.
    private static async Task TestMemory()
    {
        var big = Tracks(3000, "m");
        var albums = Enumerable.Range(0, 50).Select(i => new Album { Id = "ma" + i, Name = "Album " + i }).ToList();
        var artists = Enumerable.Range(0, 50).Select(i => new ArtistInfo { Id = "mr" + i, Name = "Artist " + i }).ToList();
        var playlists = Enumerable.Range(0, 50).Select(i => new Playlist { Id = "mp" + i, Name = "Playlist " + i, TrackCount = 3000 }).ToList();
        var now = DateTime.UtcNow;
        App.Library.Store(Library.HomeKey, new HomeReply { TopTracks = big.Take(50).ToList(), Playlists = playlists }, now);
        App.Library.Store(Library.ChartsKey, new CatalogReply { Tracks = big.Take(100).ToList(), Albums = albums, Artists = artists, Playlists = playlists }, now);
        App.Library.Store(Library.PlaylistsKey, new PlaylistsReply { Playlists = playlists }, now);
        App.Library.Store(Library.LikedKey, new TracksReply { Tracks = big }, now);
        App.Library.Store(Library.Key("playlist", "mp0"), new TracksReply { Tracks = Tracks(3000, "n") }, now);
        App.Library.Store(Library.Key("artist", "mx"), new ArtistProfile { Artist = new ArtistInfo { Id = "mx", Name = "Artist" }, Top = big.Take(10).ToList(), Albums = albums, Related = artists }, now);

        static (double Heap, double Set) Measure()
        {
            GC.Collect();
            GC.WaitForPendingFinalizers();
            GC.Collect();
            using var p = Process.GetCurrentProcess();
            return (GC.GetTotalMemory(true) / 1e6, p.WorkingSet64 / 1e6);
        }
        var before = Measure();
        var pages = new (View View, object? Arg)[]
        {
            (new HomeView(), null), (new ChartsView(), null), (new PlaylistsView(), null), (new CollectionView(), Collection.Liked),
            (new CollectionView(), Collection.Of(playlists[0])), (new ArtistView(), "mx"), (new SearchView(), null), (new RecentView(), null),
            (new OfflineView(), null), (new SettingsView(), null), (new EqualizerView(), null),
        };
        foreach (var (view, arg) in pages)
        {
            await Host(view.Root, 1200, 900);
            view.Enter(arg, false, null);
            await Settle(500);
            // A long list down to its end and back, as a reader would scroll it.
            if (view.Scroller is { } sv)
            {
                sv.ChangeView(null, sv.ScrollableHeight, null, disableAnimation: true);
                await Settle(300);
                sv.ChangeView(null, 0, null, disableAnimation: true);
                await Settle(300);
            }
            view.Leave();
        }
        s_box.Children.Clear();
        await Settle();
        var after = Measure();
        Log($"memory, {pages.Length} pages visited with 3 000-track data: managed heap {before.Heap:F1} -> {after.Heap:F1} MB, working set {before.Set:F1} -> {after.Set:F1} MB");
    }

    // The element alone in a box of exactly this size, whatever the window's size.
    private static async Task Host(FrameworkElement e, double width, double height = 700)
    {
        s_box.Children.Clear();
        s_canvas.Children.Clear();
        bool dark = Application.Current.RequestedTheme == ApplicationTheme.Dark;
        s_box = new Grid { Width = width, Height = height, Background = new SolidColorBrush(dark ? Windows.UI.Color.FromArgb(255, 32, 32, 32) : Colors.White) };
        s_box.Children.Add(e);
        s_canvas.Children.Add(s_box);
        // One layout pass and a turn of the dispatcher: the caller settles what it needs after.
        await Settle(50);
    }

    private static async Task Settle(int ms = 400)
    {
        s_canvas.UpdateLayout();
        await Task.Delay(ms);
        s_canvas.UpdateLayout();
    }

    private static IEnumerable<T> Descendants<T>(DependencyObject root) where T : DependencyObject
    {
        int n = VisualTreeHelper.GetChildrenCount(root);
        for (int i = 0; i < n; i++)
        {
            var c = VisualTreeHelper.GetChild(root, i);
            if (c is T t) yield return t;
            foreach (var d in Descendants<T>(c)) yield return d;
        }
    }

    private static string Shown(FrameworkElement? e)
    {
        if (e == null) return "HIDDEN (not found)";
        DependencyObject? d = e;
        while (d != null)
        {
            if (d is UIElement { Visibility: Visibility.Collapsed }) return "HIDDEN (collapsed: " + d.GetType().Name + ")";
            if (ReferenceEquals(d, s_canvas)) return "visible";
            d = VisualTreeHelper.GetParent(d);
        }
        return "HIDDEN (detached)";
    }

    private static async Task Shot(UIElement e, string name)
    {
        var rtb = new RenderTargetBitmap();
        await rtb.RenderAsync(e);
        var px = (await rtb.GetPixelsAsync()).ToArray();
        using var fs = File.Open(Path.Combine(s_dir, name + ".png"), FileMode.Create, FileAccess.ReadWrite);
        using var ras = fs.AsRandomAccessStream();
        var enc = await BitmapEncoder.CreateAsync(BitmapEncoder.PngEncoderId, ras);
        enc.SetPixelData(BitmapPixelFormat.Bgra8, BitmapAlphaMode.Premultiplied, (uint)rtb.PixelWidth, (uint)rtb.PixelHeight, 96, 96, px);
        await enc.FlushAsync();
        Log($"  screenshot {name}.png");
    }

    // Does each of the bar's three groups fit its column? Columns: now playing, transport, right-hand buttons.
    private static async Task TestBar()
    {
        var bar = new PlayerBar(DispatcherQueue.GetForCurrentThread());
        bar.Root.VerticalAlignment = VerticalAlignment.Bottom;
        await Host(bar.Root, 1180, 120);
        var g = bar.Root;
        var cd = g.ColumnDefinitions;
        async Task MeasureRight(double w, string label)
        {
            s_box.Width = w;
            await Settle(350);
            var right = (StackPanel)g.Children[2];
            double need = 0, minX = double.MaxValue;
            var names = new List<string>();
            foreach (var o in right.Children)
            {
                var c = (FrameworkElement)o;
                if (c.Visibility != Visibility.Visible) continue;
                need += c.ActualWidth;
                minX = Math.Min(minX, c.TransformToVisual(g).TransformPoint(new WPoint(0, 0)).X);
                names.Add(AutomationProperties.GetName(c));
            }
            double slot = LayoutInformation.GetLayoutSlot(right).X;
            double sum = cd[0].ActualWidth + cd[1].ActualWidth + cd[2].ActualWidth + 2 * g.ColumnSpacing + g.Padding.Left + g.Padding.Right;
            string verdict = minX < slot - 0.5 || sum > w + 0.5 ? "CUT OFF by " + Math.Max(slot - minX, sum - w).ToString("F0") : "fits";
            var transport = bar._transport;
            double center = transport.TransformToVisual(g).TransformPoint(new WPoint(((FrameworkElement)transport).ActualWidth / 2, 0)).X - w / 2;
            Log($"{label}w={w} (bar {g.ActualWidth:F0}): columns {cd[0].ActualWidth:F0}/{cd[1].ActualWidth:F0}/{cd[2].ActualWidth:F0}, right group needs {need:F0} -> {verdict}, transport {center:+0;-0;0} from the center  [{string.Join(", ", names)}]");
        }
        foreach (double w in new double[] { 1400, 1180, 1072, 1000, 960, 900, 899, 856, 800, 720, 719, 608, 560, 480 })
        {
            await MeasureRight(w, "");
            if (w is 900 or 720 or 560 or 480) await Shot(s_box, "bar-" + w);
        }

        // Widening the bar never narrows the seek slider, nothing is cut off on the way, and the
        // times and the sound-mode chip show whole at every width (a one-hour length, the widest).
        App.Player.Current = new Track { Id = "t", Name = "A title", ArtistLine = "An artist", DurationMs = 3_723_000 };
        App.Player.DurationMs = 3_723_000L;
        App.Player.Format = "FLAC · 24-bit 96 kHz";
        bar.RenderTrack();
        bar._dur.Text = Fmt.Time(3_723_000);
        bar._pos.Text = Fmt.Time(3_722_000);
        bool Whole(FrameworkElement e) => e.Visibility == Visibility.Visible && e.ActualWidth > 0 && e.ActualWidth >= e.DesiredSize.Width - 0.5
            && LayoutInformation.GetLayoutSlot(e).Width >= e.DesiredSize.Width - 0.5;
        double lastSlider = 0, lastW = 0, worst = 0, minSlider = double.MaxValue;
        string where = "", lost = "";
        int cut = 0;
        foreach (double w in Enumerable.Range(0, (1400 - 480) / 8 + 1).Select(k => 480.0 + 8 * k).Concat([719, 899]).Order())
        {
            s_box.Width = w;
            await Settle(60);
            double slider = bar._seek.ActualWidth;
            if (!Whole(bar._pos) || !Whole(bar._dur) || !Whole(bar._format)) lost += $" {w}";
            if (w is 480 or 576 or 720 or 896 or 1200) Log($"  sweep w={w}: slider {slider:F0}, times {bar._pos.ActualWidth:F0}/{bar._dur.ActualWidth:F0}, chip {bar._format.ActualWidth:F0}");
            double sum = cd[0].ActualWidth + cd[1].ActualWidth + cd[2].ActualWidth + 2 * g.ColumnSpacing + g.Padding.Left + g.Padding.Right;
            if (sum > w + 0.5) cut++;
            if (lastSlider - slider > worst)
            {
                worst = lastSlider - slider;
                where = $"{lastW}->{w}";
            }
            minSlider = Math.Min(minSlider, slider);
            lastSlider = slider;
            lastW = w;
        }
        Log($"sweep 480..1400 px: slider at least {minSlider:F0} px, largest narrowing {worst:F0} px {where}, widths cut off {cut}, times or chip not whole at{(lost.Length > 0 ? lost : " none")} -> {(worst <= 1 && cut == 0 && lost.Length == 0 ? "ok" : "WRONG")}");
        App.Player.Format = "";

        // The seek slider while a play loads (the engine still plays the old stream) and once it plays.
        var player = App.Player;
        var seek = bar._seek;
        player.Current = new Track { Id = "t", DurationMs = 200000 };
        player.DurationMs = 200000L;
        player.State = PlayState.Loading;
        bar.RenderTransport();
        bool whileLoading = seek.IsEnabled;
        player.State = PlayState.Playing;
        bar.RenderTransport();
        bool whilePlaying = seek.IsEnabled;
        Log($"seek slider enabled while loading: {whileLoading}, while playing: {whilePlaying} -> {(!whileLoading && whilePlaying ? "ok" : "WRONG")}");

        // An episode adds its speed button to the right-hand group.
        player.Current = new Track { Id = "e", Name = "An episode", DurationMs = 3600000, IsEpisode = true };
        bar.RenderSpeed();
        foreach (double w in new double[] { 1000, 899, 480 }) await MeasureRight(w, "episode ");
        await Shot(s_box, "bar-episode-480");
        player.Current = new Track { Id = "t", DurationMs = 200000 };
        bar.RenderSpeed();

        // Another session's track: shown in the card while this plays nothing, within the card's column.
        var away = new Track { Id = "web", Name = "Want to Want Me", ArtistLine = "Jason Derulo", DurationMs = 207000 };
        player.Elsewhere = new Elsewhere { Track = away, UntilMs = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() + 207000 };
        player.State = PlayState.Paused;
        bar.RenderTransport();
        bool shownPaused = bar._elsewhere.Visibility == Visibility.Visible;
        foreach (double w in new double[] { 900, 480 })
        {
            s_box.Width = w;
            await Settle(350);
            double end = bar._elsewhere.TransformToVisual(g).TransformPoint(new WPoint(bar._elsewhere.ActualWidth, 0)).X;
            Log($"w={w}: another session's track ends at {end:F0}, its column at {cd[0].ActualWidth + g.Padding.Left:F0} -> {(end <= cd[0].ActualWidth + g.Padding.Left + 0.5 ? "fits" : "CUT OFF")}");
            if (w == 900) await Shot(s_box, "bar-elsewhere-900");
        }
        player.State = PlayState.Playing;
        bar.RenderTransport();
        bool hiddenPlaying = bar._elsewhere.Visibility == Visibility.Collapsed;
        player.State = PlayState.Stopped;
        player.Elsewhere = new Elsewhere { Track = away, UntilMs = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() - 1 };
        bar.RenderTransport();
        bool hiddenEnded = bar._elsewhere.Visibility == Visibility.Collapsed;
        Log($"another session's track shown while paused here: {shownPaused}, hidden while playing here: {hiddenPlaying}, after its end: {hiddenEnded} -> " +
            (shownPaused && hiddenPlaying && hiddenEnded ? "ok" : "WRONG"));

        // A track the other session did not name: the glyph and its label, nothing to play here.
        player.Elsewhere = new Elsewhere { UntilMs = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() + 360000 };
        bar.RenderTransport();
        await Settle(350);
        bool unnamed = bar._elsewhere.Visibility == Visibility.Visible && bar._elsewhereTitleLine.Visibility == Visibility.Collapsed &&
            bar._playHere.Visibility == Visibility.Collapsed && bar._elsewhereArtist.Text == Loc.S("Tooltip_PlayingElsewhere");
        double unnamedEnd = bar._elsewhere.TransformToVisual(g).TransformPoint(new WPoint(bar._elsewhere.ActualWidth, 0)).X;
        await Shot(s_box, "bar-elsewhere-unnamed");
        Log($"w={s_box.Width}: a track the other session did not name shows its label alone: {unnamed}, ends at {unnamedEnd:F0}, " +
            $"its column at {cd[0].ActualWidth + g.Padding.Left:F0} -> {(unnamed && unnamedEnd <= cd[0].ActualWidth + g.Padding.Left + 0.5 ? "ok" : "WRONG")}");
        player.Elsewhere = new();
        player.Current = null;
        player.State = PlayState.Stopped;
        bar.RenderTransport();
    }

    // What each view's empty state leaves on screen.
    private static async Task TestStates()
    {
        var tracks = new List<Track> { new() { Id = "1", Name = "Song", ArtistLine = "Artist", DurationMs = 1000 } };

        var search = new SearchView();
        await Host(search.Root, 1000);
        var filters = search._filters;
        search._result = new CatalogReply { Tracks = tracks };
        search._query = "abc";
        search.ApplyFilter();
        await Settle();
        Log("search, filter All, 1 track: filter bar " + Shown(filters));
        filters.SelectedItem = filters.Items[3];
        await Settle();
        Log("search, filter Albums, 0 albums: filter bar " + Shown(filters));

        // Any button offering "New playlist", by name or by caption, that is on screen.
        var playlists = new PlaylistsView();
        await Host(playlists.Root, 1000);
        string caption = Loc.S("Btn_NewPlaylist");
        string NewPlaylist() => Descendants<Button>(playlists.Root)
            .Where(b => AutomationProperties.GetName(b) == caption || b.Content as string == caption)
            .Any(b => Shown(b) == "visible") ? "visible" : "HIDDEN";
        App.Library.Store(Library.PlaylistsKey, new PlaylistsReply { Playlists = [new Playlist { Id = "p", Name = "One" }] }, DateTime.UtcNow);
        playlists.Enter(null, false, null);
        await Settle();
        Log("playlists, one playlist: New playlist button " + NewPlaylist());
        playlists.Leave();
        App.Library.Store(Library.PlaylistsKey, new PlaylistsReply(), DateTime.UtcNow);
        playlists.Enter(null, false, null);
        await Settle();
        Log("playlists, no playlists: New playlist button " + NewPlaylist());
        playlists.Leave();

        var collection = new CollectionView();
        await Host(collection.Root, 1000);
        var hero = collection._hero;
        App.Library.Store(Library.Key("playlist", "empty"), new TracksReply(), DateTime.UtcNow);
        collection.Enter(Collection.Of(new Playlist { Id = "empty", Name = "Empty playlist" }), false, null);
        await Settle();
        Log("collection, an empty playlist: hero " + Shown(hero.Root));
        collection.Leave();
    }

    // How often a row-limited grid is filled the first time.
    private static async Task TestTiles()
    {
        var albums = Enumerable.Range(0, 40).Select(i => new Album { Id = i.ToString(), Name = "Album " + i }).ToList();
        var grid = new TileGrid<Album>(Tiles.Album, _ => { });
        var section = Section.Tiles("Section_Albums", grid, 2);
        var fills = new List<string>();
        var sw = Stopwatch.StartNew();
        grid.Root.RegisterPropertyChangedCallback(ItemsRepeater.ItemsSourceProperty, (_, _) =>
            fills.Add(((grid.Root.ItemsSource as ICollection)?.Count ?? -1) + " items at " + sw.ElapsedMilliseconds + " ms"));
        var state = new StateView(Ui.Page(section.Root), SkeletonShape.Page);
        state.ShowLoading();
        await Host(state.Root, 1000);
        fills.Clear();
        sw.Restart();
        grid.SetItems(albums);
        state.ShowContent();
        await Settle(800);
        Log($"row-limited grid (2 rows, 40 albums, 1000 px): filled {fills.Count}x: {string.Join("; ", fills)} -> {(fills.Count == 1 ? "ok" : "MADE AGAIN")}");

        // A CDN picture is asked for at the smallest size that covers the decode size; a URL without a size is left alone.
        string sizedUrl = Ui.SizedArt("https://e-cdns-images.dzcdn.net/images/cover/ab12/250x250-000000-80-0-0.jpg", 300);
        string unsized = Ui.SizedArt("https://e-cdns-images.dzcdn.net/images/cover/ab12/cover.jpg", 300);
        Log($"CDN size: {sizedUrl[^28..]}, without a size unchanged {unsized.EndsWith("/ab12/cover.jpg")} -> " +
            (sizedUrl == "https://e-cdns-images.dzcdn.net/images/cover/ab12/500x500-000000-80-0-0.jpg" && unsized.EndsWith("/ab12/cover.jpg") ? "ok" : "WRONG"));

        // How wide a stretched tile gets, and what its picture decodes at.
        // The working directory is gui\windows.harness\out.
        string art = new Uri(Path.GetFullPath(Path.Combine(Environment.CurrentDirectory, "..", "..", "..", "assets", "icon.png"))).AbsoluteUri;
        var pictured = Enumerable.Range(0, 6).Select(i => new Album { Id = "w" + i, Name = "Album " + i, ArtworkUrl = art }).ToList();
        foreach (double w in new[] { 300.0, 440, 1000 })
        {
            var g = new TileGrid<Album>(Tiles.Album, _ => { });
            await Host(Ui.Page(g.Root), w);
            g.SetItems(pictured);
            await Settle(600);
            var tile = Descendants<MediaTile>(g.Root).First();
            var brush = Descendants<Border>(tile).Select(b => b.Background).OfType<ImageBrush>().First();
            int decoded = (brush.ImageSource as BitmapImage)?.DecodePixelWidth ?? 0;
            double needed = tile.ActualWidth * Ui.Scale;
            Log($"tiles at {w} px: tile {tile.ActualWidth:0} px wide ({needed:0} physical), picture decoded at {decoded} px -> {(decoded >= needed - Pad.Tile.Left * 2 * Ui.Scale ? "sharp" : "BLURRED")}");
        }
    }

    // The track and artist pages' backdrop through Backdrop.Set: a picture blurred once, which fills
    // the page at any size (the whole cover, rescaled), and the grain on a square that never sizes it.
    private static async Task TestBackdrop()
    {
        string art = new Uri(Path.GetFullPath(Path.Combine(Environment.CurrentDirectory, "..", "..", "..", "assets", "icon.png"))).AbsoluteUri;
        var backdrop = new Backdrop(new Grid());
        await Host(backdrop.Root, 900, 600);
        backdrop.Set(art);
        await Settle(800);
        var shown = (Grid)backdrop.Root.Children[0];
        var picture = (Border)shown.Children[0];
        var brush = (ImageBrush)picture.Background;
        var first = brush.ImageSource;
        bool Fills() => Math.Abs(picture.ActualWidth - s_box.Width) < 0.5 && Math.Abs(picture.ActualHeight - s_box.Height) < 0.5;
        bool small = Fills();
        s_box.Width = 1300;
        s_box.Height = 760;
        await Settle(200);
        bool large = Fills();
        int tiles = ((Canvas)shown.Children[2]).Children.Count;
        bool fits = backdrop.Root.DesiredSize.Height <= s_box.Height + 0.5;
        bool ok = first is WriteableBitmap { PixelWidth: Size.BackdropPicture } && ReferenceEquals(first, brush.ImageSource) && small && large && fits && tiles > 0;
        await Shot(s_box, "backdrop");
        Log($"backdrop: {(first as WriteableBitmap)?.PixelWidth ?? 0} px blurred picture filling the page at 900 x 600 {small} and 1300 x 760 {large}, the same picture, {tiles} grain tiles, the page asks {backdrop.Root.DesiredSize.Height:0} px -> {(ok ? "ok" : "WRONG")}");
    }

    // A window being resized, as a row-limited grid sees it: its width in 8-px steps a frame apart,
    // wider then narrower. Wrong when the grid's items are made again, or a tile in view changes
    // picture or goes blank on the way; the sharper pictures come once the steps rest.
    private static async Task TestResize()
    {
        // A picture of its own for each album (copies in the working directory), as covers are.
        // The working directory is gui\windows.harness\out.
        string source = Path.GetFullPath(Path.Combine(Environment.CurrentDirectory, "..", "..", "..", "assets", "icon.png"));
        var albums = Enumerable.Range(0, 40).Select(i =>
        {
            string copy = Path.Combine(Environment.CurrentDirectory, $"resize-art-{i}.png");
            if (!File.Exists(copy)) File.Copy(source, copy);
            return new Album { Id = "r" + i, Name = "Album " + i, ArtworkUrl = new Uri(copy).AbsoluteUri };
        }).ToList();
        var grid = new TileGrid<Album>(Tiles.Album, _ => { });
        var section = Section.Tiles("Section_Albums", grid, 2);
        int fills = 0;
        grid.Root.RegisterPropertyChangedCallback(ItemsRepeater.ItemsSourceProperty, (_, _) => fills++);
        await Host(Ui.Page(section.Root), 700);
        grid.SetItems(albums);
        await Settle(1000);
        var seen = new HashSet<ImageSource>();
        // By album, in the frame before: the repeater recycles a tile element for another album, and
        // an album that left the view comes back as a new tile.
        var shown = new Dictionary<object, ImageSource>();
        int blankFrames = 0, pictures = 0, blank = 0;
        void Look()
        {
            // Only the tiles placed in the grid: the repeater parks recycled ones far outside it.
            var placed = Descendants<MediaTile>(grid.Root).Where(t =>
            {
                var p = t.TransformToVisual(grid.Root).TransformPoint(new WPoint(0, 0));
                return p.X >= 0 && p.X < grid.Root.ActualWidth && p.Y >= 0 && p.Y < grid.Root.ActualHeight;
            });
            var now = new Dictionary<object, ImageSource>();
            foreach (var tile in placed)
            {
                if (Descendants<Border>(tile).Select(b => b.Background).OfType<ImageBrush>().FirstOrDefault(b => b.ImageSource != null)?.ImageSource is not BitmapImage image) continue;
                if (seen.Add(image)) pictures++;
                if (tile.Item is not { } album) continue;
                now[album] = image;
                // A tile in view that changes picture shows its placeholder until the new one decodes.
                if (!shown.TryGetValue(album, out var before)) continue;
                if (!ReferenceEquals(before, image)) blankFrames++;
                if (image.PixelWidth == 0) blank++;
            }
            shown.Clear();
            foreach (var (album, image) in now) shown[album] = image;
        }
        await Settle(800);
        Look();
        fills = 0; pictures = 0; blankFrames = 0; blank = 0;
        var widths = Enumerable.Range(0, 101).Select(i => 700.0 + i * 8).Concat(Enumerable.Range(0, 101).Select(i => 1500.0 - i * 8));
        foreach (double w in widths)
        {
            s_box.Width = w;
            s_canvas.UpdateLayout();
            Look();
            await Task.Delay(16);
        }
        bool ok = fills == 0 && blankFrames == 0 && blank == 0;
        string sweep = $"items made again {fills}x, pictures of tiles coming into view {pictures}, picture changes in view {blankFrames}, blanked {blank}";
        blankFrames = 0;
        await Settle(800);
        Look();
        Log($"resize sweep 700 -> 1500 -> 700 px: {sweep}; sharper pictures after the rest {blankFrames} -> {(ok ? "ok" : "WRONG")}");
    }

    // Queue rows: how far the title moves when hover swaps the duration for the two buttons.
    private static async Task TestRows()
    {
        var tracks = Enumerable.Range(0, 6).Select(i => new Track { Id = "q" + i, Name = "A fairly long track title that will not fit the queue pane " + i, ArtistLine = "Artist", DurationMs = 205000 }).ToList();
        var queue = new TrackList(ListMode.Queue) { ShowAlbum = false };
        queue.SetItems(tracks);
        await Host(queue.Root, Size.QueuePane - 2 * Space.XS, 400);
        await Settle(1200);
        var duration = Descendants<TextBlock>(queue.Root).FirstOrDefault(t => t.Text == "3:25");
        var title = Descendants<TextBlock>(queue.Root).FirstOrDefault(t => t.Text.StartsWith("A fairly long"));
        Log($"queue row at rest: duration {duration?.ActualWidth:F0} px, title {title?.ActualWidth:F0} px; hover shows 2 x {Size.RowButton} px of buttons, so the title loses {2 * Size.RowButton - (duration?.ActualWidth ?? 0):F0} px");
        await Shot(s_box, "queue");

        // What a list of rows costs at rest: UI elements per realized row, none hovered.
        var full = new TrackList(ListMode.Full);
        full.SetItems(Enumerable.Range(0, 40).Select(i => new Track { Id = "e" + i, Name = "Song " + i, ArtistLine = "Artist", AlbumName = "Album", DurationMs = 200000 }).ToList());
        await Host(full.Root, 1000, 600);
        await Settle(1200);
        var containers = Descendants<ListViewItem>(full.Root).ToList();
        int elements = containers.Sum(c => Descendants<DependencyObject>(c).Count());
        int perRow = containers.Count == 0 ? 0 : elements / containers.Count;
        Log($"rows at rest: {containers.Count} realized, {elements} elements, {perRow} per row -> {(containers.Count > 0 && perRow <= 20 ? "ok" : "WRONG")}");
        if (containers.FirstOrDefault()?.Tag is TrackRow hovered)
        {
            hovered.SetHover(true);
            await Settle(300);
            var shown = new[] { hovered._play, hovered._like, hovered._more }.Count(b => b is { Visibility: Visibility.Visible, Opacity: > 0 });
            Log($"hovered row: {shown} of 3 buttons made and shown -> {(shown == 3 ? "ok" : "WRONG")}");
        }
    }

    // How often a visit fills the content when the Library still holds the same reply, and what Back costs.
    private static async Task TestFills()
    {
        var lists = Enumerable.Range(0, 5).Select(i => new Playlist { Id = "p" + i, Name = "Playlist " + i }).ToList();
        App.Library.Store(Library.HomeKey, new HomeReply { TopTracks = Tracks(20, "f"), Playlists = lists }, DateTime.UtcNow);

        var home = new HomeView();
        await Host(home.Root, 1000);
        int railFills = 0, topFills = 0;
        home._playlists.Root.RegisterPropertyChangedCallback(ItemsRepeater.ItemsSourceProperty, (_, _) => railFills++);
        ((ItemsRepeater)home._top.Root).RegisterPropertyChangedCallback(ItemsRepeater.ItemsSourceProperty, (_, _) => topFills++);
        for (int visit = 1; visit <= 2; visit++)
        {
            railFills = topFills = 0;
            home.Enter(null, visit == 2, visit == 2 ? 0 : null);
            await Settle(600);
            Log($"Home visit {visit}: playlists grid filled {railFills}x, top tracks filled {topFills}x" + (visit == 1 ? "" : $" -> {(railFills == 0 && topFills == 0 ? "ok" : "WRONG")}"));
            home.Leave();
        }

        var sw = new Stopwatch();
        foreach (int count in new[] { 50, 3000 })
        {
            var p = new Playlist { Id = "big" + count, Name = "Playlist" };
            App.Library.Store(Library.Key("playlist", p.Id), new TracksReply { Tracks = Tracks(count, "f") }, DateTime.UtcNow);
            var view = new CollectionView();
            await Host(view.Root, 1200, 900);
            int listFills = 0;
            ((ListView)(view._list).Root).RegisterPropertyChangedCallback(ItemsControl.ItemsSourceProperty, (_, _) => listFills++);
            view.Enter(Collection.Of(p), false, null);
            await Settle(800);
            view.Leave();
            listFills = 0;
            sw.Restart();
            view.Enter(Collection.Of(p), true, 0.0);
            s_canvas.UpdateLayout();
            Log($"Collection, Back to {count} tracks (same reply): list filled {listFills}x, {sw.Elapsed.TotalMilliseconds:F0} ms on the UI thread -> {(listFills == 0 ? "ok" : "WRONG")}");
            view.Leave();
        }
    }

    // A collection page reached again while the Library still holds the same tracks reply:
    // the skipped refill must not leave another collection's hero or an emptied list behind.
    private static async Task TestStale()
    {
        var tracks = Tracks(5, "s");
        App.Library.Store(Library.Key("playlist", "s1"), new TracksReply { Tracks = tracks }, DateTime.UtcNow);
        var view = new CollectionView();
        await Host(view.Root, 1000);
        var hero = view._hero;
        var list = view._list;
        string Title() => hero._title.Text;
        async Task Visit(string id, string name, bool back)
        {
            view.Enter(Collection.Of(new Playlist { Id = id, Name = name }), back, back ? 0 : null);
            await Settle();
        }

        await Visit("s1", "Old name", false);
        view.Leave();
        await Visit("s1", "New name", false);
        Log($"reopened after a rename (same tracks reply): hero title '{Title()}' -> {(Title() == "New name" ? "correct" : "STALE")}");
        view.Leave();

        await Visit("s1", "A", false);
        view.Leave();
        // B is not cached; without an engine its load throws, which leaves the page as a Back before the answer does.
        await Visit("not-cached", "B", false);
        view.Leave();
        await Visit("s1", "A", true);
        Log($"Back to A after B was left while loading: {list.Items.Count} of 5 rows, hero title '{Title()}' -> {(list.Items.Count == 5 && Title() == "A" ? "correct" : "STALE")}");
        view.Leave();

        // A reply leaves the Library: a track it shares with another cached reply stays known.
        var kept = new Track { Id = "ix", Name = "From B", DurationMs = 1000 };
        App.Library.Store(Library.Key("playlist", "ix-b"), new TracksReply { Tracks = [kept] }, DateTime.UtcNow);
        App.Library.Store(Library.Key("playlist", "ix-a"), new TracksReply { Tracks = [new Track { Id = "ix", Name = "From A", DurationMs = 1000 }] }, DateTime.UtcNow);
        App.Library.Remove(Library.Key("playlist", "ix-a"));
        var enriched = App.Library.Enrich([new Track { Id = "ix" }])[0];
        Log($"Library index after the reply that indexed a track left: '{enriched.Name}' -> {(ReferenceEquals(enriched, kept) ? "ok" : "WRONG")}");

        // Lyrics left while they load (without an engine the load throws, as a Back before the answer
        // would leave it), then opened again for the same track once they are in: the lines show.
        var lyrics = new LyricsView();
        await Host(lyrics.Root, 1000);
        var player = App.Player;
        player.Current = new Track { Id = "ly", Name = "Song", ArtistLine = "Artist", DurationMs = 200000 };
        lyrics.Enter(null, false, null);
        await Settle();
        lyrics.Leave();
        App.Library.Store(Library.Key("lyrics", "ly"), new Lyrics { Plain = "First line\nSecond line" }, DateTime.UtcNow);
        lyrics.Enter(null, false, null);
        await Settle();
        bool shown = lyrics._scroller.Visibility == Visibility.Visible && lyrics._lines.Children.Count == 1;
        Log($"lyrics reopened for the same track after a load was left: shown {shown} -> {(shown ? "ok" : "STUCK")}");
        lyrics.Leave();

        // A failed load says so and offers a retry, instead of "no lyrics".
        App.Library.Store(Library.Key("lyrics", "le"), new Lyrics { Error = "boom" }, DateTime.UtcNow);
        player.Current = new Track { Id = "le", Name = "Song", ArtistLine = "Artist", DurationMs = 200000 };
        lyrics.Enter(null, false, null);
        await Settle();
        var texts = Descendants<TextBlock>(lyrics.Root).Where(b => Shown(b) == "visible").Select(b => b.Text).ToList();
        bool failed = texts.Any(s => s.Contains("boom")) && Descendants<Button>(lyrics.Root).Any(b => Shown(b) == "visible" && b.Content as string == Loc.S("Action_Retry"));
        Log($"lyrics that failed to load: the reason and Retry shown {failed} -> {(failed ? "ok" : "WRONG")}");
        lyrics.Leave();

        // A free account gets the lyrics Deezer sends.
        App.Session.SetAccount(new Account { UserId = "me", Name = "Me", LoggedIn = true, Premium = false });
        App.Library.Store(Library.Key("lyrics", "lf"), new Lyrics { IsSynced = true, Synced = [new() { TimeMs = 0, Text = "One" }, new() { TimeMs = 2000, Text = "Two" }] }, DateTime.UtcNow);
        player.Current = new Track { Id = "lf", Name = "Song", ArtistLine = "Artist", DurationMs = 200000 };
        lyrics.Enter(null, false, null);
        await Settle();
        bool freeLines = App.Session.IsFree && lyrics._scroller.Visibility == Visibility.Visible && lyrics._lines.Children.Count == 2;
        Log($"lyrics on a free account: {lyrics._lines.Children.Count} line(s) shown -> {(freeLines ? "ok" : "WRONG")}");
        lyrics.Leave();
        App.Session.SetAccount(new Account());
        player.Current = null;

        // Each queued track keeps the list it was played from, for Deezer's listen log; a track
        // added with Play next keeps the one it had.
        var first = new Track { Id = "c1", Name = "One", DurationMs = 1000 };
        var second = new Track { Id = "c2", Name = "Two", DurationMs = 1000 };
        var fromPlaylist = new ListenContext("playlist_page", "42");
        player.Play(new[] { first, second }, 0, fromPlaylist);
        var extra = new Track { Id = "c3", Name = "Three", DurationMs = 1000 };
        player.PlayNext(extra);
        bool keeps = player.ContextOf(second) == fromPlaylist && player.ContextOf(extra) == ListenContext.None;
        Log($"queued tracks keep the list they were played from: {keeps} -> {(keeps ? "ok" : "WRONG")}");
        player.Reset();

        // A collection page plays its rows from its own list.
        view.Enter(Collection.Of(new Playlist { Id = "s1", Name = "A" }), false, null);
        await Settle();
        bool named = view._list.Context == new ListenContext("playlist_page", "s1");
        Log($"a playlist page names its rows' list: {named} -> {(named ? "ok" : "WRONG")}");
        view.Leave();
    }

    // n tracks with ids prefix + 0.., as a list shows them; `art` gives each its picture.
    private static List<Track> Tracks(int n, string prefix, Func<int, string>? art = null) =>
        Enumerable.Range(0, n).Select(i => new Track { Id = prefix + i, Name = "Song " + i, ArtistLine = "Artist", AlbumName = "Album", DurationMs = 200000, ArtworkUrl = art == null ? "" : art(i) }).ToList();

    private static void Seed(string key, Reply r) => App.Library.Store(key, r, DateTime.UtcNow);

    // A local shuffle or repeat change is sent on the play chain, behind any DZPlay still resolving
    // its stream. This feeds the poll what the engine reports until that call runs: the old value.
    private static async Task TestTransport()
    {
        var player = App.Player;
        EngineSnap EngineStill(bool shuffle, RepeatMode repeat) => new(0, 0, 0, 0, "", null, -1, -1, "", true, "", false, repeat, shuffle,
            player.Volume, "", 0, Environment.TickCount64, DispatchGen: player._playDispatchGen);
        // The play chain stays busy past the poll, as with a DZPlay on a slow connection, until the
        // returned source is completed.
        TaskCompletionSource Busy()
        {
            var held = new TaskCompletionSource();
            player._playChain = held.Task;
            return held;
        }

        var held = Busy();
        player.SetShuffle(true);
        player.ApplySlow(EngineStill(shuffle: false, RepeatMode.Off));
        held.SetResult();
        Log($"shuffle switched on; a poll while the chain is busy still reads the engine's old value: shuffle is {(player.Shuffle ? "on -> kept" : "off -> REVERTED")}");

        held = Busy();
        player.SetRepeat(RepeatMode.All);
        player.ApplySlow(EngineStill(player.Shuffle, RepeatMode.Off));
        held.SetResult();
        Log($"repeat set to All; a poll while the chain is busy still reads the engine's old value: repeat is {player.Repeat} -> {(player.Repeat == RepeatMode.All ? "kept" : "REVERTED")}");

        player.Current = new Track { Id = "newer", DurationMs = 200000 };
        player.PlayTakenOver(new Track { Id = "there" }, new TakeOver { Tracks = [new Track { Id = "there" }] }, player._playDispatchGen - 1);
        Log($"Play here answered after a newer play: the newer play stays: now {player.Current?.Id} -> {(player.Current?.Id == "newer" ? "ok" : "WRONG")}");

        // The poll's rules, fed snapshots as the engine reports them.
        EngineSnap Snap(PlayState st, string nowId, long pos, long dur) => new((int)st, pos, dur, 0, nowId, null, -1, -1, "", false, "", false,
            player.Repeat, player.Shuffle, player.Volume, "", 0, Environment.TickCount64, DispatchGen: player._playDispatchGen, StopsInFlight: player._stopsInFlight);

        player.Current = new Track { Id = "new", DurationMs = 200000 };
        player._playsInFlight = 1;
        player.PositionMs = 0L;
        player.DurationMs = 200000L;
        player.Apply(Snap(PlayState.Playing, "old", 120000, 300000));
        Log($"play resolving, engine still on the old track at 2:00 of 5:00: shown {player.PositionMs} ms of {player.DurationMs} ms, {player.State} -> " +
            (player.PositionMs == 0 && player.DurationMs == 200000 && player.State == PlayState.Loading ? "ok" : "WRONG"));

        Log($"poll delay: playing in view {Player.PollDelay(PlayState.Playing, true)}, playing hidden {Player.PollDelay(PlayState.Playing, false)}, loading {Player.PollDelay(PlayState.Loading, true)}, paused {Player.PollDelay(PlayState.Paused, true)} -> " +
            (Player.PollDelay(PlayState.Playing, true) == Timing.PollFastMs && Player.PollDelay(PlayState.Loading, true) == Timing.PollSlowMs && Player.PollDelay(PlayState.Paused, true) == Timing.PollBackoffMs ? "ok" : "WRONG"));

        bool idleSlow = Enumerable.Range(1, Timing.SlowPollEvery - 1).All(t => Player.ReadsSlow(Timing.PollBackoffMs, t));
        bool fastSlow = Enumerable.Range(1, Timing.SlowPollEvery - 1).Any(t => Player.ReadsSlow(Timing.PollFastMs, t));
        Log($"slow fields at the idle cadence on every poll: {idleSlow}, on off-ticks while playing: {fastSlow} -> {(idleSlow && !fastSlow ? "ok" : "WRONG")}");

        var err = Snap(PlayState.Error, "new", 0, 0);
        Log($"an Errored snapshot equal to the last one sent is still sent: {!err.SameAs(err)} -> {(!err.SameAs(err) ? "ok" : "WRONG")}");

        player._playsInFlight = 0;
        player.State = PlayState.Playing;
        player.PositionMs = 60000L;
        player.Apply(Snap(PlayState.Playing, "new", 60100, 200000));
        long near = player.PositionMs;
        player.Apply(Snap(PlayState.Playing, "new", 61000, 200000));
        long far = player.PositionMs;
        Log($"clock at 60000: engine 100 ms ahead leaves it at {near}, 1000 ms ahead re-bases to {far} -> {(near == 60000 && far >= 61000 ? "ok" : "WRONG")}");

        // While a play is in flight the engine's state, position, length and now-playing are the old stream's.
        void Starting(string id)
        {
            player.Current = new Track { Id = id, DurationMs = 200000 };
            player._playsInFlight = 1;
            player.State = PlayState.Loading;
            player.PositionMs = 0L;
            player.DurationMs = 200000L;
            player._engineNowId = "";
        }
        Starting("new");
        player.Apply(Snap(PlayState.Playing, "new", 120000, 300000));
        Log($"same id played again, engine still at 2:00 of 5:00: shown {player.PositionMs} ms of {player.DurationMs} ms, {player.State} -> " +
            (player.PositionMs == 0 && player.DurationMs == 200000 && player.State == PlayState.Loading ? "ok" : "WRONG"));

        Starting("new");
        player.Apply(Snap(PlayState.Stopped, "", 0, 0));
        var fromIdle = player.State;
        Starting("new");
        player.Apply(Snap(PlayState.Error, "old", 0, 0));
        Log($"play started from idle: {fromIdle}, after an error: {player.State} -> " +
            (fromIdle == PlayState.Loading && player.State == PlayState.Loading ? "ok" : "WRONG"));

        Starting("c");
        player.Apply(Snap(PlayState.Playing, "b", 0, 0) with { NowTrack = new Track { Id = "b", DurationMs = 180000 } });
        Log($"quick skips to c, engine reports the intermediate b: title {player.Current?.Id} -> {(player.Current?.Id == "c" ? "ok" : "WRONG")}");

        int gen = player._playDispatchGen + 1;
        player._playDispatchGen = gen;
        player._playOkGen = gen;
        player._errHandledGen = -1;
        player._playsInFlight = 0;
        player.Current = new Track { Id = "new", DurationMs = 200000 };
        // Read while the previous track's Error was still the engine's state, applied after this play started.
        player.Apply(Snap(PlayState.Error, "old", 0, 0) with { PlayOkGen = gen - 1 });
        bool oldErrorTaken = player._errHandledGen == gen;
        player.Apply(Snap(PlayState.Error, "new", 0, 0) with { PlayOkGen = gen });
        bool ownErrorTaken = player._errHandledGen == gen;
        Log($"the previous track's Error, read before this play started, taken as this play's failure: {oldErrorTaken}; this play's own Error: {ownErrorTaken} -> " +
            (!oldErrorTaken && ownErrorTaken ? "ok" : "WRONG"));

        Starting("a");
        player._playsInFlight = 0;
        player.State = PlayState.Playing;
        player.PositionMs = 150000L;
        player.Apply(Snap(PlayState.Playing, "b", 1000, 180000) with { NowTrack = new Track { Id = "b", DurationMs = 180000 } });
        Log($"auto-advance from a at 2:30 to b at 0:01: title {player.Current?.Id}, shown {player.PositionMs} ms of {player.DurationMs} ms -> " +
            (player.Current?.Id == "b" && player.PositionMs < 10000 && player.DurationMs == 180000 ? "ok" : "WRONG"));
        player._playsInFlight = 0;
        player.Current = null;
        player.State = PlayState.Stopped;

        // The reader against a stand-in engine: the error text once per error state, now-playing
        // only when something that moves it changed.
        var engine = new EngineStandIn();
        var reader = new EngineSnapReader(engine);
        engine.StateNow = (int)PlayState.Error;
        reader.Read(0, 0, 0, 0, slow: false);
        reader.Read(0, 0, 0, 0, slow: false);
        int errorReads = engine.LastErrorReads;
        engine.StateNow = (int)PlayState.Stopped;
        reader.Read(0, 0, 0, 0, slow: false);
        engine.StateNow = (int)PlayState.Error;
        reader.Read(0, 0, 0, 0, slow: false);
        Log($"reader: DZLastErrorJSON read {errorReads}x over one Errored state, {engine.LastErrorReads}x after a second -> {(errorReads == 1 && engine.LastErrorReads == 2 ? "ok" : "WRONG")}");
        int nowReads = engine.NowReads;
        engine.Version++;
        engine.StateNow = (int)PlayState.Playing;
        reader.Read(0, 0, 0, 0, slow: false);
        reader.Read(0, 0, 0, 0, slow: true);
        bool still = engine.NowReads == nowReads;
        engine.NowVersion++;
        reader.Read(0, 0, 0, 0, slow: false);
        Log($"reader: now-playing read again after a queue, state and slow-tick change with its version unmoved {!still}, after its version moved {engine.NowReads > nowReads} -> {(still && engine.NowReads > nowReads ? "ok" : "WRONG")}");
        // Another session's track: read on a slow tick once its version moved, and adopted by the player.
        int awayReads = engine.ElsewhereReads;
        engine.AwayVersion++;
        reader.Read(0, 0, 0, 0, slow: false);
        bool fastSkips = engine.ElsewhereReads == awayReads;
        var awaySnap = reader.Read(0, 0, 0, 0, slow: true);
        reader.Read(0, 0, 0, 0, slow: true);
        int awayEvents = 0;
        player.ElsewhereChanged += () => awayEvents++;
        player.ApplySlow(awaySnap);
        player.ApplySlow(awaySnap);
        Log($"reader: another session's track read on a fast tick {!fastSkips}, {engine.ElsewhereReads - awayReads}x over two slow ticks; " +
            $"the player shows {player.Elsewhere.Track?.Id} after {awayEvents} change(s) -> " +
            (fastSkips && engine.ElsewhereReads - awayReads == 1 && player.Elsewhere.Track?.Id == "web" && awayEvents == 1 ? "ok" : "WRONG"));
        // A session Deezer ended: read on a slow tick, told once.
        engine.Expired = true;
        reader.Read(0, 0, 0, 0, slow: false);
        var expiredSnap = reader.Read(0, 0, 0, 0, slow: true);
        int expiredEvents = 0;
        player.SessionExpired += () => expiredEvents++;
        player.ApplySlow(expiredSnap);
        player.ApplySlow(expiredSnap);
        engine.Expired = false;
        Log($"reader: a session Deezer ended read on a slow tick {expiredSnap.SessionExpired}, told {expiredEvents}x over two -> {(expiredSnap.SessionExpired && expiredEvents == 1 ? "ok" : "WRONG")}");

        // Snapshots of the old stream, read through the real reader, applied after the play finished.
        string Shown() => $"{player.Current?.Id} {player.State} {player.PositionMs} ms of {player.DurationMs} ms";
        bool Untouched() => player.Current?.Id == "new" && player.State == PlayState.Loading && player.PositionMs == 0 && player.DurationMs == 200000;
        var oldStream = new EngineStandIn { StateNow = (int)PlayState.Playing, PosNow = 120000, DurNow = 300000, NowJson = "{\"id\":\"new\"}", NowVersion = 1 };
        Starting("new");
        int dispatched = ++player._playDispatchGen;
        var during = new EngineSnapReader(oldStream).Read(0, -1, 1, dispatched, slow: false);
        player._playsInFlight = 0;
        player._playOkGen = dispatched;
        player.Apply(during);
        string duringShown = Shown();
        bool duringOk = Untouched();
        Starting("new");
        var before = new EngineSnapReader(oldStream).Read(0, -1, 0, player._playDispatchGen, slow: false);
        dispatched = ++player._playDispatchGen;
        player._playsInFlight = 0;
        player._playOkGen = dispatched;
        player.Apply(before);
        Log($"the same id played again; the old stream at 2:00 of 5:00 read while the play resolved: {duringShown}; read before it was dispatched: {Shown()}; both applied after it finished -> " +
            (duringOk && Untouched() ? "ok" : "WRONG"));

        // A phone's play supersedes an app play while it resolves: the phone's track, whose
        // now-playing moved only while the app play was in flight, is shown once that play ended.
        var phone = new EngineSnapReader(new EngineStandIn { StateNow = (int)PlayState.Playing, PosNow = 5000, DurNow = 240000, NowJson = "{\"id\":\"phone\"}", NowVersion = 1 });
        Starting("app");
        dispatched = ++player._playDispatchGen;
        player.Apply(phone.Read(0, -1, 1, dispatched, slow: false));
        player._playsInFlight = 0;
        player.Apply(phone.Read(0, -1, 0, dispatched, slow: false));
        player.Apply(phone.Read(0, -1, 0, dispatched, slow: false));
        Log($"a phone's track that started while an app play resolved, read after that play was superseded: {Shown()} -> " +
            (player.Current?.Id == "phone" && player.State == PlayState.Playing && player.PositionMs >= 5000 && player.PositionMs < 6000 ? "ok" : "WRONG"));

        // Three stream failures in a row, as the poll reads them: a lost network stops on the track
        // with one no-internet failure; a failure of the track itself skips as before.
        (int Moved, List<PlayerFailure> Raised) FailThrice(bool network)
        {
            var raised = new List<PlayerFailure>();
            void On(PlayerFailure f) => raised.Add(f);
            player.Failed += On;
            player.Play(Enumerable.Range(0, 6).Select(i => new Track { Id = "n" + i, DurationMs = 200000 }).ToList(), 1, ListenContext.None);
            int start = player.QueueIndex;
            for (int i = 0; i < 3; i++)
            {
                // The engine started each play dispatched since the last failure.
                if (player.State == PlayState.Loading) player._playOkGen = player._playDispatchGen;
                player._playsInFlight = 0;
                player.Apply(Snap(PlayState.Error, player.Current?.Id ?? "", 95000, 200000) with { PlayOkGen = player._playOkGen, LastError = "no network", LastErrorNetwork = network });
            }
            player.Failed -= On;
            return (player.QueueIndex - start, raised);
        }
        var lost = FailThrice(network: true);
        // The network lost, then a phone starts a track: the bar takes its position and length,
        // and a seek goes to the engine. The phone's track is read once the stop has run.
        await player._playChain.ContinueWith(_ => { });
        player._playsInFlight = 0;
        player.Apply(Snap(PlayState.Playing, "n4", 5000, 240000));
        string phoneShown = Shown();
        bool phoneOk = player.Current?.Id == "n4" && player.State == PlayState.Playing && player.DurationMs == 240000 && player.PositionMs is >= 5000 and < 6000;
        player.Seek(30000);
        bool seekSent = player._seekPump.Busy;
        Log($"the network lost, then the phone plays another track at 0:05: {phoneShown}; a seek sent to the engine {seekSent} -> " +
            (phoneOk && seekSent ? "ok" : "WRONG"));
        var broken = FailThrice(network: false);
        Log($"three failures in a row: network lost -> cursor moved {lost.Moved} row(s), {lost.Raised.Count} failure(s) \"{lost.Raised.FirstOrDefault()?.Title}\"; track failures -> moved {broken.Moved}, {broken.Raised.Count} failure(s) -> " +
            (lost.Moved == 0 && lost.Raised.Count == 1 && lost.Raised[0].Title == Loc.S("NoInternet_Title") && broken.Moved == 2 && broken.Raised.Count == 3 ? "ok" : "WRONG"));
        player.ClearQueue();
        player.Current = null;
        player.State = PlayState.Stopped;

        var repeatBefore = player.Repeat;
        player.SetRepeat(RepeatMode.Off);
        player.Play([new Track { Id = "w0", DurationMs = 200000 }, new Track { Id = "w1", Unavailable = true }, new Track { Id = "w2", Unavailable = true }], 0, ListenContext.None);
        player.Next();
        Log($"Next with only unavailable rows ahead: cursor on row {player.QueueIndex} -> {(player.QueueIndex == 0 ? "ok" : "WRONG")}");
        player.SetRepeat(repeatBefore);
        player.ClearQueue();
        player.Current = null;
        player.State = PlayState.Stopped;

        // An account switch: the old account's stop still on the chain, a Playing snapshot of its
        // track read after the stop was dispatched keeps the new account's restored resume point.
        var queueOwner = typeof(Player).GetField("_queueOwner", BindingFlags.Instance | BindingFlags.NonPublic)!;
        var resumeMs = typeof(Player).GetField("_resumeMs", BindingFlags.Instance | BindingFlags.NonPublic)!;
        player.Reset();
        player.Play([new Track { Id = "rs1", DurationMs = 200000 }], 0, ListenContext.None);
        queueOwner.SetValue(player, "switch");
        player.PositionMs = 90000L;
        player.SaveQueue(wait: true);
        var stopHeld = Busy();
        player.Reset();
        queueOwner.SetValue(player, "");
        player.Restore("switch");
        player._playsInFlight = 0;
        player.Apply(Snap(PlayState.Playing, "old1", 120000, 300000));
        long kept = (long)resumeMs.GetValue(player)!;
        stopHeld.SetResult();
        player.Reset();
        queueOwner.SetValue(player, "");
        Log($"account switch, the stop still queued, a Playing snapshot of the old track: resume point {kept} ms -> {(kept == 90000 ? "ok" : "WRONG")}");

        // The same snapshot read while the stop was queued, applied after the stop ran.
        player.Play([new Track { Id = "rs2", DurationMs = 200000 }], 0, ListenContext.None);
        queueOwner.SetValue(player, "switch2");
        player.PositionMs = 90000L;
        player.SaveQueue(wait: true);
        await player._playChain.ContinueWith(_ => { });
        var stopHeld2 = Busy();
        player.Reset();
        queueOwner.SetValue(player, "");
        player.Restore("switch2");
        player._playsInFlight = 0;
        var readEarly = Snap(PlayState.Playing, "old2", 120000, 300000);
        stopHeld2.SetResult();
        await player._playChain.ContinueWith(_ => { });
        player.Apply(readEarly);
        long kept2 = (long)resumeMs.GetValue(player)!;
        player.Reset();
        queueOwner.SetValue(player, "");
        Log($"account switch, a Playing snapshot read before the stop ran, applied after: resume point {kept2} ms -> {(kept2 == 90000 ? "ok" : "WRONG")}");

        // Don't recommend on the last track with repeat off: playback stops instead of going on.
        player.SetRepeat(RepeatMode.Off);
        player.Play([new Track { Id = "ban1", DurationMs = 200000 }], 0, ListenContext.None);
        player.Current = player.Queue[0];
        int genBefore = player._playDispatchGen;
        Menus.SkipBanned("track", "ban1");
        bool stopped = player._playDispatchGen > genBefore && player.QueueIndex == 0;
        player.Reset();
        Log($"Don't recommend on the only track, repeat off: a stop sent {stopped} -> {(stopped ? "ok" : "WRONG")}");

        // The same with repeat all: the one track is not played again.
        player.SetRepeat(RepeatMode.All);
        player.Play([new Track { Id = "ban2", DurationMs = 200000 }], 0, ListenContext.None);
        player.Current = player.Queue[0];
        var banHeld = Busy();
        int stopsBefore = player._stopsInFlight;
        Menus.SkipBanned("track", "ban2");
        bool stoppedAll = player._stopsInFlight > stopsBefore && player.QueueIndex == 0;
        banHeld.SetResult();
        await player._playChain.ContinueWith(_ => { });
        player.Reset();
        player.SetRepeat(RepeatMode.Off);
        Log($"Don't recommend on the only track, repeat all: a stop sent {stoppedAll} -> {(stoppedAll ? "ok" : "WRONG")}");
    }

    private sealed class EngineStandIn : IEngineReads
    {
        public int StateNow, LastErrorReads, NowReads, ElsewhereReads;
        public long Version, NowVersion, AwayVersion, PosNow, DurNow;
        public string NowJson = "{\"id\":\"t\"}";
        public long NowPlayingVersion() => NowVersion;
        public int FinishedCount() => 0;
        public int State() => StateNow;
        public long PositionMs() => PosNow;
        public long DurationMs() => DurNow;
        public int QueueIndex() => -1;
        public long QueueVersion() => Version;
        public LastErrorReply LastError() { LastErrorReads++; return new() { Error = "failed" }; }
        public byte[] NowPlayingUtf8() { NowReads++; return System.Text.Encoding.UTF8.GetBytes(NowJson); }
        public string Format() => "";
        public bool IsPreview() => false;
        public RepeatMode Repeat() => RepeatMode.Off;
        public bool Shuffle() => false;
        public double Volume() => 1;
        public string Connected() => "";
        public bool Expired;
        public bool SessionExpired() => Expired;
        public long ElsewhereVersion() => AwayVersion;
        public Elsewhere Elsewhere() { ElsewhereReads++; return new() { Track = new() { Id = "web" }, UntilMs = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() + 207000 }; }
    }

    // A window of its own, its title bar set up as MainWindow does and not maximized: where the client
    // area starts against the visible frame, and the search box against the band the bar shows.
    private static async Task TestFrame()
    {
        var w = new Window();
        var search = new SearchBox(DispatcherQueue.GetForCurrentThread(), App.Navigator);
        var bar = new AppTitleBar(w, () => { }, () => { }, Ui.IconButton(Icons.Settings, "Nav_Settings", null)) { Search = search.Root };
        var below = new Border { Background = new SolidColorBrush(Colors.Gray) };
        var rows = Ui.Rows("auto *", bar.Root, below);
        w.Content = rows;
        w.ExtendsContentIntoTitleBar = true;
        w.AppWindow.TitleBar.PreferredHeightOption = Microsoft.UI.Windowing.TitleBarHeightOption.Standard;
        w.SetTitleBar(bar.Root);
        w.AppWindow.IsShownInSwitchers = false;
        w.AppWindow.MoveAndResize(new RectInt32(-28000, -28000, 1200, 800));
        w.AppWindow.Show(false);
        await Task.Delay(1500);
        var hwnd = WinRT.Interop.WindowNative.GetWindowHandle(w);
        double s = rows.XamlRoot.RasterizationScale;
        var origin = new NativeMethods.POINT();
        NativeMethods.DwmGetWindowAttribute(hwnd, NativeMethods.DWMWA_EXTENDED_FRAME_BOUNDS, out var frame,
            System.Runtime.InteropServices.Marshal.SizeOf<NativeMethods.RECT>());
        NativeMethods.ClientToScreen(hwnd, ref origin);
        double border = (origin.Y - frame.Top) / s;
        var r = search.Root.TransformToVisual(null).TransformBounds(new Windows.Foundation.Rect(0, 0, search.Root.ActualWidth, search.Root.ActualHeight));
        double bandBottom = below.TransformToVisual(null).TransformPoint(new WPoint(0, 0)).Y;
        double caption = w.AppWindow.TitleBar.Height / s;
        Log($"scale {s}, frame top {frame.Top}, client top {origin.Y}: border {border:0.##} DIP; bar margin {bar.Root.Margin.Top:0.##}, row {rows.RowDefinitions[0].ActualHeight:0.##}, caption buttons {caption:0.##}");
        Log($"search {r.Top:0.##}..{r.Bottom:0.##} in the client, the band shown 0..{bandBottom:0.##}: gap above {r.Top:0.##}, below {bandBottom - r.Bottom:0.##}");
        Log($"from the frame's top: search center {r.Top + r.Height / 2 + border:0.##}, caption center {caption / 2:0.##}, band {border:0.##}..{bandBottom + border:0.##}");
        // The bar is lifted by the border the shown window has, not the one it had before its
        // content extended into the title bar.
        Log($"bar lift {-bar.Root.Margin.Top:0.##} for a border of {border:0.##}, title bar row {rows.RowDefinitions[0].ActualHeight:0.##} -> {(Math.Abs(bar.Root.Margin.Top + border) < 0.5 && rows.RowDefinitions[0].ActualHeight >= Size.TitleBar - border - 0.5 ? "ok" : "WRONG")}");
        // The window's minimum size as Windows asks for it before a border drag.
        WindowBounds.LimitSize(hwnd);
        var info = new NativeMethods.MINMAXINFO();
        unsafe { SendMessageW(hwnd, NativeMethods.WM_GETMINMAXINFO, 0, (IntPtr)(&info)); }
        Log($"minimum size {info.ptMinTrackSize.X / s:0} x {info.ptMinTrackSize.Y / s:0} -> {(Math.Abs(info.ptMinTrackSize.X / s - Size.WindowMinWidth) <= 1 && Math.Abs(info.ptMinTrackSize.Y / s - Size.WindowMinHeight) <= 1 ? "ok" : "WRONG")}");
        w.Close();
    }

    // The title bar at three widths (height, search width, the gap between the last button and
    // the caption-button inset), and the corners of the kit's text button.
    private static async Task TestChrome()
    {
        var search = new SearchBox(DispatcherQueue.GetForCurrentThread(), App.Navigator);
        var account = AccountPanel.TitleBarButton();
        var settings = Ui.IconButton(Icons.Settings, "Nav_Settings", null);
        settings.Visibility = Visibility.Collapsed;
        var bar = new AppTitleBar(s_window, () => { }, () => { }, account, settings) { Search = search.Root };
        bar.Back.Visibility = Visibility.Visible;
        foreach (double w in new[] { 1400.0, 900, 560, 480 })
        {
            await Host(bar.Root, w, Size.TitleBar);
            var end = bar.Root.ColumnDefinitions[^1];
            double accountEnd = account.TransformToVisual(bar.Root).TransformPoint(new WPoint(account.ActualWidth, 0)).X;
            double gap = w - end.ActualWidth - accountEnd;
            Log($"w={w}: bar height {bar.Root.ActualHeight}, search {search.Root.ActualWidth:0} x {search.Root.ActualHeight:0}, gap after the avatar {gap:0} -> " +
                (bar.Root.ActualHeight == Size.TitleBar && search.Root.ActualHeight <= Size.TitleBar && gap <= Space.XS ? "ok" : "WRONG"));
            if (w == 900)
            {
                await Shot(bar.Root, "titlebar-900");
                // Each shown element's center against the bar's (16), and the search text's against its box.
                var off = bar.Root.Children.OfType<FrameworkElement>().Append(account)
                    .Where(e => e.Visibility == Visibility.Visible && e.ActualHeight > 0 && e is not StackPanel)
                    .Select(e => (e, e is ContentPresenter { Content: FrameworkElement c } ? c : e))
                    .Select(p => $"{p.Item2.GetType().Name} {p.Item2.TransformToVisual(bar.Root).TransformPoint(new WPoint(0, p.Item2.ActualHeight / 2)).Y - Size.TitleBar / 2:+0.0;-0.0;0}");
                Log("  center offsets: " + string.Join(", ", off));
                var text = Descendants<TextBlock>(search.Root).FirstOrDefault(t => t.Text.Length > 0 && Shown(t) == "visible");
                if (text != null)
                    Log($"  search text center {text.TransformToVisual(search.Root).TransformPoint(new WPoint(0, text.ActualHeight / 2)).Y - search.Root.ActualHeight / 2:+0.0;-0.0;0} from the box's");
            }
        }
        // The title-bar flyout's panel, with the name and plan its opening fills in.
        var holder = (StackPanel)((Flyout)account.Flyout).Content;
        var panel = holder.Children.OfType<Border>().First();
        var flyout = (StackPanel)panel.Child;
        var who = (StackPanel)flyout.Children[0];
        ((TextBlock)who.Children[0]).Text = AccountPanel.Name;
        ((TextBlock)who.Children[1]).Text = AccountPanel.Plan;
        ((Flyout)account.Flyout).Content = null;
        holder.HorizontalAlignment = HorizontalAlignment.Left;
        await Host(holder, 600, 400);
        Log($"account flyout content {holder.ActualWidth:0} x {holder.ActualHeight:0} (needs {holder.DesiredSize.Height:0} tall): " +
            string.Join(", ", flyout.Children.OfType<FrameworkElement>().Select(c => $"{c.GetType().Name} {c.DesiredSize.Width:0}")));
        await Shot(holder, "account-flyout");
        // The flyout keeps its width whatever it shows. Its lists are as wide as their widest item from
        // the start (a ComboBox otherwise grows to it once its list has been open); the harness cannot
        // open a list, so this compares with a plain ComboBox showing the widest item.
        var lone = QuickSettings.Quality();
        lone.HorizontalAlignment = HorizontalAlignment.Left;
        await Host(lone, 600, 100);
        double fitted = lone.ActualWidth;
        double widest = 0;
        for (int i = 0; i < lone.Items.Count; i++)
        {
            var plain = new ComboBox { HorizontalAlignment = HorizontalAlignment.Left };
            plain.Items.Add(((ComboBoxItem)lone.Items[i]).Content);
            plain.SelectedIndex = 0;
            await Host(plain, 600, 100);
            widest = Math.Max(widest, plain.ActualWidth);
        }
        Log($"account flyout width {holder.ActualWidth:0} (fixed {Size.FlyoutWidth:0}); quality list {fitted:0}, widest item shown plainly {widest:0} -> {(Math.Abs(holder.ActualWidth - Size.FlyoutWidth) < 0.5 && fitted + 0.5 >= widest ? "ok" : "WRONG")}");
        var button = Ui.Button("Switch account", () => { });
        await Host(button, 300, 100);
        Log($"text button corner radius {button.CornerRadius.TopLeft} -> {(button.CornerRadius.TopLeft > 0 ? "rounded" : "SQUARE")}");
        var logo = Ui.Logo(Size.AppIcon);
        logo.HorizontalAlignment = HorizontalAlignment.Left;
        await Host(logo, 300, 100);
        bool drawn = Descendants<Border>(logo).Any(b => Microsoft.UI.Xaml.Hosting.ElementCompositionPreview.GetElementChildVisual(b) != null);
        Log($"logo {logo.ActualWidth:0} x {logo.ActualHeight:0} (asked {Size.AppIcon:0}), child visual {drawn} -> " +
            (logo.ActualWidth == Size.AppIcon && logo.ActualHeight == Size.AppIcon && drawn ? "ok" : "WRONG"));
    }

    // Every page with data: where its title sits and the gap under it (the gaps must all be equal),
    // then each distinct margin, padding and spacing the page sets, with its count. A value used
    // once across all pages is marked: a candidate inconsistency.
    private static async Task TestLayout()
    {
        var tracks = Tracks(12, "l");
        var albums = Enumerable.Range(0, 12).Select(i => new Album { Id = "a" + i, Name = "Album " + i }).ToList();
        var artists = Enumerable.Range(0, 12).Select(i => new ArtistInfo { Id = "r" + i, Name = "Artist " + i }).ToList();
        var playlists = Enumerable.Range(0, 12).Select(i => new Playlist { Id = "p" + i, Name = "Playlist " + i, TrackCount = i }).ToList();
        Seed(Library.HomeKey, new HomeReply { TopTracks = tracks, Playlists = playlists });
        Seed(Library.ChartsKey, new CatalogReply { Tracks = tracks, Albums = albums, Artists = artists, Playlists = playlists });
        Seed(Library.PlaylistsKey, new PlaylistsReply { Playlists = playlists });
        Seed(Library.Key("artist", "x"), new ArtistProfile { Artist = new ArtistInfo { Id = "x", Name = "Artist" }, Top = tracks, Albums = albums, Related = artists });
        Seed(Library.Key("playlist", "p0"), new TracksReply { Tracks = tracks });
        // Home's pinned row, a pin of each kind it shows.
        Seed(Library.PinnedKey, new PinnedReply
        {
            Items = new[] { "Album", "Artist", "Playlist", "Podcast", "Track" }.Select((t, i) => new SearchItem { Type = t, Id = "pin" + i, Title = t + " pinned", Subtitle = "Someone" }).ToList(),
        });

        var pages = new (string Name, View View, object? Arg, Action<View>? Fill)[]
        {
            // No history: the first section stays hidden, so a page's spacing must skip it.
            ("Home", new HomeView(), null, null),
            ("Charts", new ChartsView(), null, null),
            // History needs the engine: the content is shown by hand.
            ("Recent", new RecentView(), null, v =>
            {
                var recent = (RecentView)v;
                recent._total.Text = "12 h 5 min";
                recent._recent.SetItems(tracks);
                recent._state.ShowContent();
            }),
            ("Playlists", new PlaylistsView(), null, null),
            ("Search", new SearchView(), null, v =>
            {
                var search = (SearchView)v;
                search._title.Text = "Results for abc";
                search._result = new CatalogReply { Tracks = tracks, Albums = albums, Artists = artists, Playlists = playlists };
                search._query = "abc";
                search.ApplyFilter();
            }),
            ("Settings", new SettingsView(), null, null),
            ("Equalizer", new EqualizerView(), null, null),
            ("Artist", new ArtistView(), "x", null),
            ("Collection", new CollectionView(), Collection.Of(playlists[0]), null),
        };
        // The pinned row is one of Home's rows: its tiles as wide as the playlists' and as far apart
        // as the rows under it.
        {
            var home = new HomeView();
            await Host(home.Root, 1000);
            home.Enter(null, false, null);
            await Settle(600);
            var tiles = Descendants<MediaTile>(home.Root).Where(t => Shown(t) == "visible").ToList();
            var pinTile = tiles.FirstOrDefault(t => t.Item is SearchItem);
            var listTile = tiles.FirstOrDefault(t => t.Item is Playlist);
            double Top(FrameworkElement e) => e.TransformToVisual(home.Root).TransformPoint(new WPoint(0, 0)).Y;
            var sections = ((StackPanel)home._pinned.Root.Parent).Parent is StackPanel stack ? stack.Children.OfType<FrameworkElement>().Where(e => e.Visibility == Visibility.Visible).ToList() : [];
            var gapsBetween = sections.Zip(sections.Skip(1), (a, b) => Top(b) - Top(a) - a.ActualHeight).Select(g => Math.Round(g)).Distinct().ToList();
            bool same = pinTile != null && listTile != null && Math.Abs(pinTile.ActualWidth - listTile.ActualWidth) < 0.5;
            Log($"Home pinned row: {tiles.Count(t => t.Item is SearchItem)} tiles, tile {pinTile?.ActualWidth:0} wide (playlists {listTile?.ActualWidth:0}), gaps between rows {string.Join("/", gapsBetween)} -> {(same && gapsBetween.Count == 1 ? "ok" : "WRONG")}");
            await Shot(s_box, "home-pinned");
        }

        var census = new List<(string Page, string Value)>();
        var gaps = new List<double>();
        foreach (var (name, view, arg, fill) in pages)
        {
            if (view is SettingsView) App.Session.SignedIn = true;
            await Host(view.Root, 1000);
            view.Enter(arg, false, null);
            fill?.Invoke(view);
            await Settle(600);
            var title = Descendants<TextBlock>(view.Root).FirstOrDefault(t => t.Style == Ui.TextStyle(TextKind.PageTitle) && Shown(t) == "visible");
            // A hero page (artist, collection) titles itself in its hero.
            bool hero = Descendants<TextBlock>(view.Root).Any(t => t.Style == Ui.TextStyle(TextKind.HeroTitle) && Shown(t) == "visible");
            if (title == null) Log(hero ? $"{name}: hero title, no page title" : $"{name}: page title HIDDEN");
            else
            {
                var row = (FrameworkElement)VisualTreeHelper.GetParent(title);
                var above = new HashSet<DependencyObject>();
                for (DependencyObject? d = row; d != null; d = VisualTreeHelper.GetParent(d)) above.Add(d);
                var at = row.TransformToVisual(view.Root).TransformPoint(new WPoint(0, 0));
                double bottom = at.Y + row.ActualHeight;
                var next = Descendants<FrameworkElement>(view.Root)
                    .Where(e => e is TextBlock or Control or Image || e is Border { Background: not null })
                    .Where(e => !above.Contains(e) && e.ActualHeight > 0 && e.Opacity > 0 && Shown(e) == "visible")
                    .Select(e => (E: e, Y: e.TransformToVisual(view.Root).TransformPoint(new WPoint(0, 0)).Y))
                    .Where(p => p.Y >= bottom - 0.5)
                    .OrderBy(p => p.Y).FirstOrDefault();
                double gap = next.E == null ? double.NaN : next.Y - bottom;
                gaps.Add(gap);
                Log($"{name}: title at {at.X:0},{at.Y:0}, gap under it {gap:0} to {next.E?.GetType().Name}");
            }
            census.AddRange(Spacings(view.Root).Select(v => (name, v)));
            if (name is "Home" or "Settings") await Shot(s_box, "layout-" + name.ToLowerInvariant());
            view.Leave();
            if (view is SettingsView) App.Session.SignedIn = false;
        }
        Log("gaps under the title: " + (gaps.Count > 0 && gaps.All(g => Math.Abs(g - gaps[0]) < 0.5) ? $"all {gaps[0]:0} -> equal" : "DIFFER: " + string.Join(", ", gaps.Select(g => g.ToString("0")))));
        var total = census.GroupBy(c => c.Value).ToDictionary(g => g.Key, g => g.Count());
        foreach (var page in census.GroupBy(c => c.Page))
            Log($"  {page.Key}: " + string.Join("; ", page.GroupBy(c => c.Value).OrderBy(g => g.Key)
                .Select(g => $"{g.Key} x{g.Count()}" + (total[g.Key] == 1 ? " (once)" : ""))));
    }

    // The track overview filled from a sample record, and the artist hero's picture at both widths.
    private static async Task TestPages()
    {
        var track = new TrackView();
        await Host(track.Root, 900, 900);
        track._track = new Track { Id = "1", Name = "Song" };
        track.Fill(new TrackInfo
        {
            Id = "1", Title = "Harder, Better, Faster, Stronger", Version = "Live", Explicit = true,
            Artists = new[] { new TrackArtist { Id = "27", Name = "Daft Punk", Role = "main" }, new TrackArtist { Name = "Guest", Role = "featured" } },
            Album = new TrackAlbum { Id = "302127", Title = "Discovery", ReleaseDate = "2001-03-07", Label = "Parlophone", TrackCount = 14, Genres = new[] { "Dance", "Electro" } },
            TrackNumber = 4, DiskNumber = 1, DurationMs = 224000, Bpm = 123.4, Isrc = "GBDUW0000059", Copyright = "(P) 2001 Daft Life",
            Contributors = new[] { new Contributor { RoleKey = "composer", Names = new[] { "Thomas Bangalter", "Guy-Manuel de Homem-Christo" } }, new Contributor { RoleKey = "lyricist", Names = new[] { "Edwin Birdsong" } } },
        });
        await Settle(600);
        var details = track._details;
        var credits = track._credits;
        // The fixture gives 10 detail rows and 2 credit rows.
        Log($"track: {details.RowDefinitions.Count} detail rows, {credits.RowDefinitions.Count} credit rows, title '{track._hero._title.Text}' -> {(details.RowDefinitions.Count == 10 && credits.RowDefinitions.Count == 2 ? "ok" : "WRONG")}");
        await Shot(s_box, "track");

        foreach (double w in new[] { 1000.0, 600 })
        {
            var artist = new ArtistView();
            await Host(artist.Root, w, 700);
            artist.Enter("x", false, null);
            await Settle(600);
            var person = Descendants<PersonPicture>(artist.Root).First();
            var box = (FrameworkElement)VisualTreeHelper.GetParent(VisualTreeHelper.GetParent(person));
            var shown = person.TransformToVisual(box).TransformBounds(new Windows.Foundation.Rect(0, 0, person.ActualWidth, person.ActualHeight));
            Log($"artist w={w}: picture box {box.ActualWidth:0}, picture drawn {shown.Width:0} -> {(Math.Abs(shown.Width - box.ActualWidth) < 1 ? "fits" : "CLIPPED")}");
            artist.Leave();
        }
    }

    // Costs, with local pictures (one 64 px PNG copied per URL): bitmaps made while a 3 000-row
    // list scrolls down and back; working set after visiting the picture pages.
    private static async Task TestCost()
    {
        // A combo filled one item at a time, as the device, preset and explicit lists are.
        foreach (int count in new[] { 10, 40 })
        {
            var fitted = Ui.Combo([], -1, _ => { });
            var plain = new ComboBox();
            await Host(new StackPanel { Children = { fitted, plain } }, 400, 200);
            var fitClock = Stopwatch.StartNew();
            for (int i = 0; i < count; i++) fitted.Items.Add("Speakers (Realtek High Definition Audio) " + i);
            double fit = fitClock.Elapsed.TotalMilliseconds;
            fitClock.Restart();
            for (int i = 0; i < count; i++) plain.Items.Add("Speakers (Realtek High Definition Audio) " + i);
            double unfitted = fitClock.Elapsed.TotalMilliseconds;
            await Settle(50);
            fitClock.Restart();
            var once = Ui.Combo(Enumerable.Range(0, count).Select(i => "Speakers (Realtek High Definition Audio) " + i).ToArray(), -1, _ => { });
            double single = fitClock.Elapsed.TotalMilliseconds;
            Log($"combo filled with {count} items one by one: {fit:0.00} ms on the UI thread, {unfitted:0.00} ms unfitted, one fit of all {single:0.00} ms; fitted width {fitted.MinWidth:0} = {once.MinWidth:0} -> {(fitted.MinWidth == once.MinWidth ? "ok" : "WRONG")}");
        }
        var player = App.Player;
        var owner = typeof(Player).GetField("_queueOwner", BindingFlags.Instance | BindingFlags.NonPublic)!;
        player.Reset();
        player.Play(Enumerable.Range(0, 3000).Select(i => new Track { Id = "k" + i, Name = "Song " + i, ArtistLine = "Artist", AlbumName = "Album", DurationMs = 200000, ArtworkUrl = "https://example.invalid/" + i + ".jpg" }).ToList(), 0, new ListenContext("playlist_page", "1"));
        owner.SetValue(player, "cost");
        player.SaveQueue();
        var clock = Stopwatch.StartNew();
        for (int i = 0; i < 10; i++) player.SaveQueue();
        double save = clock.Elapsed.TotalMilliseconds / 10;
        player.SaveQueue(wait: true);
        player.Reset();
        owner.SetValue(player, "");
        clock.Restart();
        player.Restore("cost");
        double restore = clock.Elapsed.TotalMilliseconds;
        Log($"3 000-track queue on the UI thread: save {save:0.0} ms, restore {restore:0.0} ms ({player.Queue.Count} restored)");
        player.Reset();
        owner.SetValue(player, "");
        // Only pictures up to a tile's size are kept: a widened tile's larger decodes are not.
        var picCache = Ui.s_bitmaps;
        int keptBefore = picCache.Count;
        for (int wider = 1; wider <= 4; wider++)
            Ui.Bitmap("https://example.invalid/wide.png", Size.Tile + wider * Size.TileDecodeStep, reuse: true);
        Ui.Bitmap("https://example.invalid/tile.png", Size.Tile, reuse: true);
        int added = picCache.Count - keptBefore;
        Log($"pictures kept after four widened-tile decodes and one tile-size decode: {added} -> {(added == 1 ? "ok" : "WRONG")}");

        string dir = Path.Combine(s_dir, "art");
        Directory.CreateDirectory(dir);
        string first = Path.Combine(dir, "0.png");
        if (!File.Exists(first))
        {
            var px = new byte[64 * 64 * 4];
            for (int i = 0; i < px.Length; i += 4) { px[i] = (byte)(i % 251); px[i + 1] = (byte)(i / 256 % 251); px[i + 2] = 128; px[i + 3] = 255; }
            using (var fs = File.Open(first, FileMode.Create, FileAccess.ReadWrite))
            {
                var enc = await BitmapEncoder.CreateAsync(BitmapEncoder.PngEncoderId, fs.AsRandomAccessStream());
                enc.SetPixelData(BitmapPixelFormat.Bgra8, BitmapAlphaMode.Premultiplied, 64, 64, 96, 96, px);
                await enc.FlushAsync();
            }
        }
        var urls = Enumerable.Range(0, 3000).Select(i =>
        {
            string p = Path.Combine(dir, i + ".png");
            if (!File.Exists(p)) File.Copy(first, p);
            return new Uri(p).AbsoluteUri;
        }).ToList();
        var tracks = Tracks(3000, "c", i => urls[i]);
        var albums = Enumerable.Range(0, 24).Select(i => new Album { Id = "ca" + i, Name = "Album " + i, ArtworkUrl = urls[i] }).ToList();
        Seed(Library.Key("artist", "cost"), new ArtistProfile
        {
            Artist = new ArtistInfo { Id = "cost", Name = "Artist", ArtworkUrl = urls[0] },
            Top = tracks.GetRange(0, 10), Albums = albums,
            Related = albums.Select(a => new ArtistInfo { Id = a.Id, Name = a.Name, ArtworkUrl = a.ArtworkUrl }).ToList(),
        });

        var list = new TrackList();
        var page = Ui.Page(list.Root);
        await Host(page, 900, 700);
        list.SetItems(tracks);
        await Settle(600);
        var bitmaps = new HashSet<BitmapImage>();
        var sources = new HashSet<string>();
        void Collect()
        {
            foreach (var b in Descendants<Border>(page))
                if (b.Background is ImageBrush { ImageSource: BitmapImage bmp } && bitmaps.Add(bmp)) sources.Add(bmp.UriSource?.AbsoluteUri ?? "");
        }
        // About 136 rows down and back: within the 200 pictures Ui.Bitmap keeps.
        double step = 600, end = Math.Min(page.ScrollableHeight, 6000);
        for (double y = 0; y <= end; y += step)
        {
            page.ChangeView(null, y, null, true);
            await Settle(30);
            Collect();
        }
        int down = bitmaps.Count;
        for (double y = end; y >= 0; y -= step)
        {
            page.ChangeView(null, y, null, true);
            await Settle(30);
            Collect();
        }
        await Settle(500);
        Log($"3 000-row list, 6 000 px down and back: {sources.Count} pictures, {down} bitmaps going down, {bitmaps.Count} after coming back -> " +
            (bitmaps.Count > sources.Count ? "pictures are MADE AGAIN" : "each picture made once"));

        GC.Collect();
        GC.WaitForPendingFinalizers();
        var p0 = Process.GetCurrentProcess();
        long before = p0.WorkingSet64;
        var views = new View[] { new HomeView(), new ChartsView(), new ArtistView(), new CollectionView() };
        Seed(Library.HomeKey, new HomeReply { TopTracks = tracks.GetRange(0, 10), Playlists = albums.Select(a => new Playlist { Id = "p" + a.Id, Name = a.Name, ArtworkUrl = a.ArtworkUrl }).ToList() });
        Seed(Library.ChartsKey, new CatalogReply { Tracks = tracks.GetRange(0, 50), Albums = albums, Artists = [], Playlists = [] });
        Seed(Library.Key("playlist", "cost"), new TracksReply { Tracks = tracks.GetRange(0, 200) });
        var args = new object?[] { null, null, "cost", Collection.Of(new Playlist { Id = "cost", Name = "Cost" }) };
        for (int i = 0; i < views.Length; i++)
        {
            await Host(views[i].Root, 1000, 700);
            views[i].Enter(args[i], false, null);
            await Settle(800);
            views[i].Leave();
        }
        s_canvas.Children.Clear();
        GC.Collect();
        GC.WaitForPendingFinalizers();
        await Settle(500);
        long kept = Process.GetCurrentProcess().WorkingSet64;
        // What clearing the pictures of kept views would give back.
        foreach (var v in views)
        {
            foreach (var b in Descendants<Border>(v.Root)) if (b.Background is ImageBrush brush) brush.ImageSource = null;
            foreach (var p in Descendants<PersonPicture>(v.Root)) p.ProfilePicture = null;
        }
        GC.Collect();
        GC.WaitForPendingFinalizers();
        await Settle(500);
        long cleared = Process.GetCurrentProcess().WorkingSet64;
        Log($"working set: {before / 1048576.0:0.0} MB before the picture pages, {kept / 1048576.0:0.0} MB with {views.Length} kept views, {cleared / 1048576.0:0.0} MB with their pictures cleared");
        GC.KeepAlive(views);
    }

    // The navigation pane sliding over a narrow window: how far its surface stops short of the bottom.
    private static async Task TestPane()
    {
        var nav = new NavigationView { PaneDisplayMode = NavigationViewPaneDisplayMode.LeftMinimal, IsBackButtonVisible = NavigationViewBackButtonVisible.Collapsed };
        nav.MenuItems.Add(new NavigationViewItem { Content = "Home" });
        await Host(nav, 480, 600);
        nav.IsPaneOpen = true;
        await Settle(800);
        s_box.Background = new SolidColorBrush(Colors.Red);
        await Shot(s_box, "pane");
        foreach (var name in new[] { "PaneContentGrid", "PaneRoot" })
        {
            var e = Descendants<FrameworkElement>(nav).FirstOrDefault(x => x.Name == name);
            if (e == null) { Log(name + ": not found"); continue; }
            double bottom = e.TransformToVisual(nav).TransformPoint(new WPoint(0, e.ActualHeight)).Y;
            Log($"{name}: bottom {bottom:0.#} of {nav.ActualHeight:0} -> gap {nav.ActualHeight - bottom:0.#}");
            for (DependencyObject? d = e; d != null && d != nav; d = VisualTreeHelper.GetParent(d))
                if (d is FrameworkElement f && (f.Margin != default || f is Border { BorderThickness.Bottom: > 0 }))
                    Log($"    {f.GetType().Name} '{f.Name}' margin {f.Margin} {(f is Border b ? "border " + b.BorderThickness : "")}");
        }
    }

    // The compact pane as MainWindow builds it: the space left and right of each icon.
    private static async Task TestRail()
    {
        var nav = new NavigationView
        {
            PaneDisplayMode = NavigationViewPaneDisplayMode.LeftCompact,
            IsBackButtonVisible = NavigationViewBackButtonVisible.Collapsed,
            IsPaneToggleButtonVisible = false,
            IsSettingsVisible = true,
            AlwaysShowHeader = false,
            OpenPaneLength = Size.Pane,
            CompactPaneLength = Size.Rail,
        };
        nav.MenuItems.Add(new NavigationViewItem { Content = "Home", Icon = Ui.Icon(Icons.Home) });
        nav.MenuItems.Add(new NavigationViewItemHeader { Content = "Library" });
        nav.MenuItems.Add(new NavigationViewItem { Content = "Liked", Icon = Ui.Icon(Icons.Heart) });
        nav.Content = new Grid();
        await Host(nav, 1000, 600);
        await Settle(800);
        await Shot(s_box, "rail");
        double X(FrameworkElement e, double x) => e.TransformToVisual(nav).TransformPoint(new WPoint(x, 0)).X;
        foreach (var name in new[] { "PaneRoot", "PaneContentGrid", "RootSplitView", "PaneToggleButtonGrid", "ContentGrid" })
            if (Descendants<FrameworkElement>(nav).FirstOrDefault(x => x.Name == name) is { } e)
                Log($"{name}: x {X(e, 0):0.#} to {X(e, e.ActualWidth):0.#} margin {e.Margin}{(e is Border b ? " border " + b.BorderThickness : "")}{(e is Grid g ? " border " + g.BorderThickness : "")}");
        var split = Descendants<SplitView>(nav).First();
        Log($"split: compact {split.CompactPaneLength}, display {split.DisplayMode}");
        foreach (var icon in Descendants<FontIcon>(nav).Where(i => i.ActualWidth > 0))
        {
            double left = X(icon, 0), right = split.CompactPaneLength - X(icon, icon.ActualWidth);
            Log($"icon {(int)icon.Glyph[0]:X}: {left:0.#} from the window edge, {right:0.#} from the pane's -> {(Math.Abs(left - right) <= 1 ? "ok" : "WRONG")}");
        }
        foreach (var item in Descendants<NavigationViewItem>(nav))
            Log($"item {item.Content}: x {X(item, 0):0.#} to {X(item, item.ActualWidth):0.#} margin {item.Margin}");
    }

    // Every settings card's height and its header's left edge, at three widths.
    private static async Task TestCards()
    {
        App.Session.SignedIn = true;
        var view = new SettingsView();
        foreach (double w in new[] { 1000.0, 700, 480 })
        {
            await Host(view.Root, w, 900);
            view.Enter(null, false, null);
            await Settle(600);
            // Folder boxes with real-length paths: a path decides its box's width.
            foreach (var box in Descendants<TextBox>(view.Root)) box.Text = @"C:\Users\Someone\AppData\Local\purpledisc";
            await Settle(300);
            var cards = Descendants<CommunityToolkit.WinUI.Controls.SettingsCard>(view.Root).Where(c => Shown(c) == "visible" && c.ActualHeight > 0).ToList();
            var heights = cards.Select(c => c.ActualHeight).ToList();
            // No title breaks inside a word: each is at least as wide as its longest word (layout rounds a pixel).
            var broken = new List<string>();
            foreach (var c in cards)
            {
                if (c.Header is not string header) continue;
                var shown = Descendants<TextBlock>(c).FirstOrDefault(t => t.Text == header);
                var probe = new TextBlock { Style = shown?.Style, FontSize = shown?.FontSize ?? 14, FontFamily = shown?.FontFamily, FontWeight = shown?.FontWeight ?? Microsoft.UI.Text.FontWeights.Normal };
                double longest = 0;
                foreach (var word in header.Split(' '))
                {
                    probe.Text = word;
                    probe.Measure(new Windows.Foundation.Size(double.PositiveInfinity, double.PositiveInfinity));
                    longest = Math.Max(longest, probe.DesiredSize.Width);
                }
                if (shown == null || shown.ActualWidth + 1.5 < longest) broken.Add($"'{header}' {shown?.ActualWidth ?? 0:0} < {longest:0}");
            }

            Log($"w={w}: titles broken inside a word: {broken.Count} {string.Join(", ", broken)} -> {(broken.Count == 0 ? "ok" : "WRONG")}");
            var title = Descendants<TextBlock>(view.Root).First(t => t.Style == Ui.TextStyle(TextKind.PageTitle));
            double titleX = title.TransformToVisual(view.Root).TransformPoint(new WPoint(0, 0)).X;
            Log($"w={w}: {cards.Count} cards, height min {heights.Min():0} max {heights.Max():0}; title at x={titleX:0} -> {(heights.Min() >= Size.Row ? "ok" : "WRONG")}");
            foreach (var c in cards.Where(c => c.ActualHeight > 60))
            {
                var parts = Descendants<FrameworkElement>(c).Where(e => !string.IsNullOrEmpty(e.Name) && e.ActualHeight > 0 && Shown(e) == "visible")
                    .Select(e => $"{e.Name}={e.ActualHeight:0}");
                Log($"  tall: '{c.Header}' {c.ActualHeight:0} px, padding {c.Padding}, content {(c.Content as FrameworkElement)?.GetType().Name} {(c.Content as FrameworkElement)?.ActualHeight:0}: {string.Join(" ", parts.Take(12))}");
            }
            if (w == 480) await Shot(s_box, "cards-480");
            if (w == 1000)
            {
                var single = cards.FirstOrDefault(c => Equals(c.Header, Loc.S("Settings_SingleDevice")));
                bool on = single?.Content is ToggleSwitch { IsOn: true };
                Log($"single-device card: switch on {on}, '{single?.Description}' -> " +
                    (on && Equals(single?.Description, Loc.S("Settings_SingleDeviceOn")) ? "ok" : "WRONG"));
            }
            view.Leave();
        }
        App.Session.SignedIn = false;

        // A confirmation answered with its first button, as FolderSetting asks it: the answer is that button's.
        var anchor = new Button { Content = "anchor" };
        await Host(anchor, 400, 300);
        var answer = new TaskCompletionSource<bool>();
        var confirm = StorageGroup.Confirm("Settings_CacheFolderConfirm", ("Btn_Change", () => answer.TrySetResult(true)), ("Btn_Cancel", () => answer.TrySetResult(false)));
        confirm.Closed += (_, _) => answer.TrySetResult(false);
        confirm.ShowAt(anchor);
        await Settle();
        var change = Descendants<Button>((FrameworkElement)confirm.Content).FirstOrDefault();
        if (change == null || !confirm.IsOpen) Log("the confirmation did not open off screen; not measured");
        else
        {
            new Microsoft.UI.Xaml.Automation.Peers.ButtonAutomationPeer(change).Invoke();
            await Settle();
            Log($"open confirmation answered with its first button: {(answer.Task.IsCompleted ? answer.Task.Result.ToString() : "no answer")}, closed {!confirm.IsOpen} -> " +
                (answer.Task.IsCompleted && answer.Task.Result ? "ok" : "WRONG"));
        }

        // A Storage call that throws is a failure like one that answers its reason.
        var group = new StorageGroup();
        string outcome;
        try { outcome = await group.Run("Settings_Title", () => throw new IOException("disk gone")) ? "worked" : "failed"; }
        catch (Exception ex) { outcome = "threw " + ex.GetType().Name; }
        Log($"a Storage call that throws: {outcome} -> {(outcome == "failed" ? "ok" : "WRONG")}");
    }

    // The margins, paddings and spacings set on the shown elements under `root`, leaving out
    // zeros and the named parts of control templates.
    private static IEnumerable<string> Spacings(FrameworkElement root)
    {
        static string T(Thickness t) => t.Left == t.Top && t.Top == t.Right && t.Right == t.Bottom ? $"{t.Left:0}" : $"{t.Left:0},{t.Top:0},{t.Right:0},{t.Bottom:0}";
        static bool Set(DependencyObject o, DependencyProperty p, out object v)
        {
            v = o.ReadLocalValue(p);
            return v is Thickness t && t != default || v is double d && d != 0;
        }
        var todo = new Stack<DependencyObject>();
        todo.Push(root);
        while (todo.Count > 0)
        {
            var o = todo.Pop();
            if (o is UIElement { Visibility: Visibility.Collapsed }) continue;
            for (int i = VisualTreeHelper.GetChildrenCount(o) - 1; i >= 0; i--) todo.Push(VisualTreeHelper.GetChild(o, i));
            if (o is not FrameworkElement e || !string.IsNullOrEmpty(e.Name)) continue;
            if (Set(e, FrameworkElement.MarginProperty, out var m)) yield return "margin " + T((Thickness)m);
            var padding = e switch
            {
                Control => Control.PaddingProperty,
                Border => Border.PaddingProperty,
                StackPanel => StackPanel.PaddingProperty,
                Grid => Grid.PaddingProperty,
                _ => null,
            };
            if (padding != null && Set(e, padding, out var p)) yield return "padding " + T((Thickness)p);
            if (e is StackPanel && Set(e, StackPanel.SpacingProperty, out var s)) yield return $"spacing {(double)s:0}";
            if (e is Grid && Set(e, Grid.ColumnSpacingProperty, out var cs)) yield return $"column spacing {(double)cs:0}";
            if (e is Grid && Set(e, Grid.RowSpacingProperty, out var rs)) yield return $"row spacing {(double)rs:0}";
        }
    }

    // Wiring that broke before: subscriptions, menus, labels and queue positions.
    private static async Task TestFixes()
    {
        const BindingFlags Hidden = BindingFlags.Instance | BindingFlags.Static | BindingFlags.NonPublic;
        var accountChanged = typeof(Session).GetField("AccountChanged", Hidden)!;
        int Handlers() => (accountChanged.GetValue(App.Session) as Delegate)?.GetInvocationList().Length ?? 0;
        int handlers = Handlers();
        var settings = new SettingsView();
        await Host(settings.Root, 1000);
        settings.Enter(null, false, null);
        await Settle();
        settings.Leave();
        Log($"a Settings page built, shown and left: {Handlers() - handlers} account handlers kept -> {(Handlers() == handlers ? "ok" : "WRONG")}");
        App.Library.Store(Library.PinnedKey, new PinnedReply(), DateTime.UtcNow);
        App.Library.KeepPinned(new SearchItem { Type = "Album", Id = "pa" }, true);
        bool pinnedNow = App.Library.IsPinned("Album", "pa");
        App.Library.KeepPinned(new SearchItem { Type = "Album", Id = "pa" }, false);
        Log($"pinned from a menu: Pin to Home known at once {pinnedNow}, unpinned at once {!App.Library.IsPinned("Album", "pa")} -> {(pinnedNow && !App.Library.IsPinned("Album", "pa") ? "ok" : "WRONG")}");

        var view = new CollectionView();
        await Host(view.Root, 1000);
        var more = (Button)typeof(CollectionView).GetField("_more", Hidden)!.GetValue(view)!;
        var hidden = new List<string>();
        foreach (var c in new[] { Collection.Liked, Collection.Of(new OfflineCollection { Kind = Offline.TrackKind }) })
        {
            view.Enter(c, false, null);
            await Settle();
            hidden.Add(c.Kind + ": " + more.Visibility);
            view.Leave();
        }
        Log($"More on Liked and Pinned songs: {string.Join(", ", hidden)} -> {(hidden.All(h => h.EndsWith("Collapsed")) ? "ok" : "WRONG")}");
        var account = typeof(Session).GetProperty("Account")!;
        account.SetValue(App.Session, new Account { UserId = "me", Name = "Same Name", LoggedIn = true });
        var own = typeof(CollectionView).GetField("_own", Hidden)!;
        var owners = new List<string>();
        foreach (var (ownerId, name) in new[] { ("me", "Same Name"), ("other", "Same Name") })
        {
            view.Enter(Collection.Of(new Playlist { Id = "op" + ownerId, Name = "List", Owner = name, OwnerId = ownerId }), false, null);
            await Settle(100);
            owners.Add(ownerId + ": " + ((bool)own.GetValue(view)! ? "own" : "not own"));
            view.Leave();
        }
        account.SetValue(App.Session, new Account());
        Log($"playlists by owner id, both owners named as the account: {string.Join(", ", owners)} -> {(owners.SequenceEqual(["me: own", "other: not own"]) ? "ok" : "WRONG")}");

        App.Library.Store(Library.Key("podcast", "pe"), new EpisodesReply { Episodes = [new Episode { Id = "e1", Title = "One", DurationMs = 1000 }, new Episode { Id = "e2", Title = "Two", DurationMs = 1000 }] }, DateTime.UtcNow);
        view.Enter(Collection.Of(new Podcast { Id = "pe", Name = "Show" }), false, null);
        await Settle();
        string episodes = Loc.Plural("Episodes", 2);
        bool counted = Descendants<TextBlock>(view.Root).Any(b => b.Text.Contains(episodes));
        Log($"a podcast's hero counts \"{episodes}\": {counted} -> {(counted ? "ok" : "WRONG")}");
        view.Leave();

        App.Library.Store(Library.Key(Library.LibraryKind, "album"), new LibraryReply { Albums = [new Album { Id = "fa", Name = "Kept" }] }, DateTime.UtcNow);
        var heart = new FavoriteButton();
        await Host(heart.Root, 100, 100);
        heart.Bind("album", "fa");
        await Settle();
        Log($"heart of an album in the kept favorites: {(heart.Root.IsChecked == true ? "on" : "off")}, enabled {heart.Root.IsEnabled} -> {(heart.Root.IsChecked == true && heart.Root.IsEnabled ? "ok" : "WRONG")}");

        var player = App.Player;
        player.Reset();
        try { typeof(HomeView).GetMethod("PlayPinned", Hidden)!.Invoke(null, [new SearchItem { Type = "Track", Id = "pt", Title = "Pinned song" }]); }
        catch (TargetInvocationException ex) { Log("  (Play on a pinned song threw " + ex.InnerException?.GetType().Name + ")"); }
        bool plays = player.Queue.Any(t => t.Id == "pt");
        Log($"Play on a pinned song queues it: {plays} -> {(plays ? "ok" : "WRONG")}");
        player.Reset();

        static Track T(string id) => new() { Id = id, Name = id, DurationMs = 1000 };
        if (player.Shuffle) player.SetShuffle(false);
        player.Play(new[] { T("q0"), T("q1"), T("q2"), T("q3") }, 0, ListenContext.None);
        player.Enqueue(new[] { T("x") }, ListenContext.None);
        player.Move(1, 3);
        player.Enqueue(new[] { T("y") }, ListenContext.None);
        string moved = string.Join(",", player.Queue.Select(t => t.Id));
        Log($"added, dragged past the next rows, then another added: {moved} -> {(moved == "q0,y,q1,q2,x,q3" ? "ok" : "WRONG")}");
        player.Reset();
        player.Play(new[] { T("q0"), T("q1"), T("q2"), T("q3") }, 0, ListenContext.None);
        player.Enqueue(new[] { T("x") }, ListenContext.None);
        player.SetShuffle(true);
        player.Enqueue(new[] { T("z") }, ListenContext.None);
        int at = player.Queue.Select(t => t.Id).ToList().IndexOf("z");
        Log($"added, shuffled, then another added: at row {at} after the playing one at {player.QueueIndex} -> {(at == player.QueueIndex + 1 ? "ok" : "WRONG")}");
        player.SetShuffle(false);
        player.Reset();
        static List<string> Texts(IEnumerable<MenuFlyoutItemBase> items) => items.Select(i => i switch { MenuFlyoutSubItem s => s.Text, ToggleMenuFlyoutItem g => g.Text, MenuFlyoutItem m => m.Text, _ => "-" }).ToList();
        App.Session.SetAccount(new Account { UserId = "me", Name = "Me", LoggedIn = true });
        App.Library.Store(Library.PlaylistsKey, new PlaylistsReply { Playlists = [new Playlist { Id = "pm", Name = "Mine", OwnerId = "me" }, new Playlist { Id = "po", Name = "Theirs", OwnerId = "other" }] }, DateTime.UtcNow);
        var trackItems = new List<MenuFlyoutItemBase>();
        Menus.Track(trackItems, new Track { Id = "t1", Name = "Song", ArtistLine = "A", DurationMs = 1000 }, new TrackMenu());
        var addTo = trackItems.OfType<MenuFlyoutSubItem>().FirstOrDefault(s => s.Text == Loc.S("Menu_AddToPlaylist"));
        var targets = addTo == null ? [] : Texts(addTo.Items).Where(s => s is "Mine" or "Theirs").ToList();
        Log($"Add to playlist lists: {string.Join(", ", targets)} -> {(targets.SequenceEqual(["Mine"]) ? "ok" : "WRONG")}");

        App.Session.SetAccount(new Account());
        view.Enter(Collection.Of(new Playlist { Id = "pm", Name = "Mine", OwnerId = "me" }), false, null);
        await Settle(100);
        App.Session.SetAccount(new Account { UserId = "me", Name = "Me", LoggedIn = true });
        await Settle(100);
        bool ownLate = (bool)own.GetValue(view)!;
        view.Leave();
        Log($"own playlist opened before the account was known, then the account: own {ownLate} -> {(ownLate ? "ok" : "WRONG")}");

        var unknownHeart = new FavoriteButton();
        await Host(unknownHeart.Root, 100, 100);
        unknownHeart.Bind("podcast", "unknown-podcast");
        bool lockedWhileReading = !unknownHeart.Root.IsEnabled;
        await Settle(100);
        Log($"heart of a podcast no kept list knows: disabled while its state is read {lockedWhileReading} -> {(lockedWhileReading ? "ok" : "WRONG")}");

        var modeField = typeof(Offline).GetField("_mode", Hidden)!;
        modeField.SetValue(App.Offline, true);
        var colItems = new List<MenuFlyoutItemBase>();
        Menus.Collection(colItems, Collection.Of(new Album { Id = "oa1", Name = "Album" }));
        var rowItems = new List<MenuFlyoutItemBase>();
        Menus.Track(rowItems, new Track { Id = "t2", Name = "Song", DurationMs = 1000 }, new TrackMenu());
        modeField.SetValue(App.Offline, false);
        bool Enabled(List<MenuFlyoutItemBase> items, string key) => items.FirstOrDefault(i => Texts([i])[0] == Loc.S(key))?.IsEnabled ?? false;
        bool Present(List<MenuFlyoutItemBase> items, string key) => items.Any(i => Texts([i])[0] == Loc.S(key));
        bool offlineRule = Present(colItems, "Pin_Add") && !Enabled(colItems, "Pin_Add") && Present(rowItems, "Menu_Like") && !Enabled(rowItems, "Menu_Like") && Enabled(rowItems, "Menu_PlayNext");
        Log($"offline menus: Pin disabled, Like disabled, Play next enabled {offlineRule} -> {(offlineRule ? "ok" : "WRONG")}");

        var bar = new PlayerBar(DispatcherQueue.GetForCurrentThread());
        await Host(bar.Root, 1200, 120);
        player.Current = new Track { Id = "ep1", Name = "Episode", DurationMs = 1000, IsEpisode = true };
        var barMenu = new MenuFlyout();
        typeof(PlayerBar).GetMethod("FillMore", Hidden)!.Invoke(bar, [barMenu]);
        player.Current = null;
        bool marks = Texts(barMenu.Items).Contains(Loc.S("Episode_MarkPlayed"));
        Log($"the bar's More for an episode holds Mark as played {marks} -> {(marks ? "ok" : "WRONG")}");

        App.Library.Store(Library.Key("album", "sa1"), new TracksReply { Tracks = [new Track { Id = "st1", Name = "One", ArtistId = "ar1", ArtistLine = "Artist", DurationMs = 1000 }] }, DateTime.UtcNow);
        view.Enter(new Collection(CollectionKind.Album, "sa1", "Searched", "Artist", ""), false, null);
        await Settle(100);
        var albumItems = new List<MenuFlyoutItemBase>();
        typeof(CollectionView).GetMethod("FillMore", Hidden)!.Invoke(view, [albumItems]);
        view.Leave();
        bool goes = Present(albumItems, "Menu_GoToArtist");
        Log($"an album opened from a search item whose reply names an artist offers Go to artist {goes} -> {(goes ? "ok" : "WRONG")}");
        App.Library.Store(Library.Key("album", "sa2"), new TracksReply { Tracks = [new Track { Id = "st2", Name = "One", ArtistId = "ar1", ArtistLine = "A", DurationMs = 1000 }, new Track { Id = "st3", Name = "Two", ArtistId = "ar2", ArtistLine = "B", DurationMs = 1000 }] }, DateTime.UtcNow);
        view.Enter(new Collection(CollectionKind.Album, "sa2", "Compilation", "Various", ""), false, null);
        await Settle(100);
        var mixedItems = new List<MenuFlyoutItemBase>();
        typeof(CollectionView).GetMethod("FillMore", Hidden)!.Invoke(view, [mixedItems]);
        view.Leave();
        bool mixed = Present(mixedItems, "Menu_GoToArtist");
        Log($"a compilation (tracks by two artist ids) opened from a search item offers Go to artist {mixed} -> {(!mixed ? "ok" : "WRONG")}");
        var favorites = new PlaylistsView();
        await Host(favorites.Root, 1000);
        App.Library.Store(Library.Key(Library.LibraryKind, "album"), new LibraryReply { Albums = [new Album { Id = "fa1", Name = "One" }, new Album { Id = "fa2", Name = "Two" }] }, DateTime.UtcNow);
        favorites._tabs.SelectedItem = favorites._tabs.Items[1];
        favorites.Enter(null, false, null);
        await Settle(200);
        App.Library.KeepFavorite("album", "fa1", false);
        await Settle(200);
        var favAlbums = (TileGrid<Album>)typeof(PlaylistsView).GetField("_albums", Hidden)!.GetValue(favorites)!;
        int albumTiles = favAlbums.Root.ItemsSourceView?.Count ?? -1;
        favorites.Leave();
        Log($"a favorite album removed on the Favorites tab: {albumTiles} tile left -> {(albumTiles == 1 ? "ok" : "WRONG")}");

        var pageItems = new List<MenuFlyoutItemBase>();
        Menus.Track(pageItems, new Track { Id = "t3", Name = "Song", DurationMs = 1000 }, new TrackMenu(Page: true));
        bool noDetails = !Present(pageItems, "Menu_TrackDetails");
        Log($"the Track page's own menu has no Track details {noDetails} -> {(noDetails ? "ok" : "WRONG")}");
        var historyItems = new List<MenuFlyoutItemBase>();
        Menus.Track(historyItems, new Track { Id = "h1", Name = "Song", ArtistLine = "Artist", DurationMs = 1000 }, new TrackMenu());
        bool historyGoes = Present(historyItems, "Menu_GoToArtist");
        Log($"a history track (no artist id) offers Go to artist {historyGoes} -> {(!historyGoes ? "ok" : "WRONG")}");
        var search = new SearchView();
        await Host(search.Root, 1000);
        foreach (var (q, back) in new[] { ("a", false), ("b", false), ("a", true) })
        {
            search.Enter(q, back, null);
            await Settle(50);
            if (!back) search.Leave();
        }
        string backQuery = (string)typeof(SearchView).GetField("_query", Hidden)!.GetValue(search)!;
        search.Leave();
        Log($"a search for \"a\", then \"b\", then Back to \"a\": the page's query is \"{backQuery}\" -> {(backQuery == "a" ? "ok" : "WRONG")}");

        App.Library.Store(Library.PinnedKey, new PinnedReply(), DateTime.UtcNow);
        App.Library.Store(Library.HomeKey, new HomeReply(), DateTime.UtcNow);
        App.Library.KeepPinned(new SearchItem { Type = "Album", Id = "ka", Title = "Kept album" }, true);
        var homePins = new HomeView();
        await Host(homePins.Root, 1000);
        homePins.Enter(null, false, null);
        await Settle(300);
        // The grid's items (it lays tiles out only once measured on screen).
        var pinTiles = (IReadOnlyList<SearchItem>)typeof(TileGrid<SearchItem>).GetField("_all", Hidden)!.GetValue(homePins._pinned)!;
        bool titled = pinTiles.Count > 0 && pinTiles[0].Title == "Kept album";
        homePins.Leave();
        Log($"a fresh pin on Home shows its title {titled} -> {(titled ? "ok" : "WRONG")}");

        var pinItems = new List<MenuFlyoutItemBase>();
        HomeView.PinMenu(pinItems, new SearchItem { Type = "Album", Id = "ka", Title = "Kept album" });
        bool pinPlays = Present(pinItems, "Menu_PlayNext");
        Log($"a pinned album tile's menu holds Play next {pinPlays} -> {(pinPlays ? "ok" : "WRONG")}");

        // Offline mode as the engine reports it: the buttons outside the menus are disabled, never hidden.
        var engineField = typeof(Offline).GetField("_engine", Hidden)!;
        var realEngine = engineField.GetValue(App.Offline);
        engineField.SetValue(App.Offline, new OfflineStandIn());
        App.Session.SetAccount(new Account { UserId = "me", Name = "Me", LoggedIn = true, Premium = true });
        await App.Offline.SetModeAsync(true);
        player.Current = new Track { Id = "ot", Name = "Song", DurationMs = 1000 };
        typeof(PlayerBar).GetMethod("RenderLike", Hidden)!.Invoke(bar, null);
        bool barHeart = ((Button)typeof(PlayerBar).GetField("_like", Hidden)!.GetValue(bar)!).IsEnabled;
        player.Current = null;
        var trackPage = new TrackView();
        await Host(trackPage.Root, 1000);
        trackPage.Enter(new Track { Id = "ot", Name = "Song", DurationMs = 1000 }, false, null);
        await Settle(100);
        bool pageLike = ((Button)typeof(TrackView).GetField("_like", Hidden)!.GetValue(trackPage)!).IsEnabled;
        bool pageRadio = Descendants<Button>(trackPage.Root).First(b => AutomationProperties.GetName(b) == Loc.S("Menu_StartRadio")).IsEnabled;
        trackPage.Leave();
        view.Enter(Collection.Of(new Album { Id = "oa2", Name = "Album" }), false, null);
        await Settle(100);
        bool heroDownload = ((Button)typeof(CollectionView).GetField("_download", Hidden)!.GetValue(view)!).IsEnabled;
        var colHeart = ((FavoriteButton)typeof(CollectionView).GetField("_favorite", Hidden)!.GetValue(view)!).Root;
        view.Leave();
        // A row's heart, New playlist, Save as playlist (the queue's and a mix's), the bar's artist
        // link and an album's artist link.
        var row = new TrackRow(new Grid(), new Button(), _ => { });
        row.Bind(new Track { Id = "or", Name = "Row", ArtistLine = "A", DurationMs = 1000 }, 0, new RowOptions(false));
        row.SetHover(true);
        bool rowHeart = row._like?.IsEnabled ?? false;
        var lists = new PlaylistsView();
        await Host(lists.Root, 1000);
        lists.Enter(null, false, null);
        await Settle(100);
        bool newPlaylist = ((Button)typeof(PlaylistsView).GetField("_newPlaylist", Hidden)!.GetValue(lists)!).IsEnabled;
        lists.Leave();
        var pane = new QueuePane(new Grid(), new Grid());
        player.Play([new Track { Id = "oq", Name = "Queued", DurationMs = 1000 }], 0, ListenContext.None);
        typeof(QueuePane).GetMethod("RenderState", Hidden)!.Invoke(pane, null);
        bool queueSave = ((Button)typeof(QueuePane).GetField("_save", Hidden)!.GetValue(pane)!).IsEnabled;
        view.Enter(Collection.Of("Mix", [new Track { Id = "om", Name = "Mixed", DurationMs = 1000 }], new ListenContext("x", "y")), false, null);
        await Settle(100);
        var mixItems = new List<MenuFlyoutItemBase>();
        typeof(CollectionView).GetMethod("FillMore", Hidden)!.Invoke(view, [mixItems]);
        bool mixSave = mixItems.OfType<MenuFlyoutItem>().First(i => i.Text == Loc.S("Menu_SaveAsPlaylist")).IsEnabled;
        view.Leave();
        player.Current = new Track { Id = "ot", Name = "Song", ArtistId = "ar", ArtistLine = "A", DurationMs = 1000 };
        typeof(PlayerBar).GetMethod("RenderNowPlaying", Hidden)!.Invoke(bar, null);
        bool barArtist = ((Control)typeof(PlayerBar).GetField("_artist", Hidden)!.GetValue(bar)!).IsEnabled;
        player.ClearQueue();
        player.Current = null;
        App.Library.Store(Library.Key("album", "oa4"), new TracksReply { Tracks = [new Track { Id = "ol", Name = "One", ArtistId = "ar9", ArtistLine = "Artist", DurationMs = 1000 }] }, DateTime.UtcNow);
        view.Enter(new Collection(CollectionKind.Album, "oa4", "Album", "Artist", "") { ArtistId = "ar9" }, false, null);
        await Settle(100);
        bool albumArtist = Descendants<TextBlock>(view.Root).Any(tb => tb.Inlines.OfType<Microsoft.UI.Xaml.Documents.Hyperlink>().Any());
        view.Leave();
        await App.Offline.SetModeAsync(false);
        var (colHeartShown, colHeartEnabled) = (colHeart.Visibility, colHeart.IsEnabled); // the view is entered again below
        // A mode change while a track plays reaches the bar, the queue pane and Home's Flow moods
        // with no render call: the bar's heart and artist link, the queue's Save as playlist, a mood.
        var modePane = new QueuePane(new Grid(), new Grid());
        var playing = new Track { Id = "mq", Name = "Queued", ArtistId = "ar", ArtistLine = "A", DurationMs = 1000 };
        player.Play([playing], 0, ListenContext.None);
        player.Current = playing;
        await Host(view.Root, 1000); // shown, as a page is
        view.Enter(Collection.Of(new Album { Id = "oa5", Name = "Album" }), false, null);
        await Settle(100);
        var moodsHome = new HomeView();
        moodsHome.Enter(null, false, null);
        var mood = new Button();
        ((StackPanel)typeof(HomeView).GetField("_moods", Hidden)!.GetValue(moodsHome)!).Children.Add(mood);
        var barLike = (Button)typeof(PlayerBar).GetField("_like", Hidden)!.GetValue(bar)!;
        var barLink = (Control)typeof(PlayerBar).GetField("_artist", Hidden)!.GetValue(bar)!;
        var paneSave = (Button)typeof(QueuePane).GetField("_save", Hidden)!.GetValue(modePane)!;
        await App.Offline.SetModeAsync(true);
        bool[] modeOn = [barLike.IsEnabled, barLink.IsEnabled, paneSave.IsEnabled, mood.IsEnabled];
        await App.Offline.SetModeAsync(false);
        bool[] modeOff = [barLike.IsEnabled, barLink.IsEnabled, paneSave.IsEnabled, mood.IsEnabled];
        moodsHome.Leave();
        view.Leave();
        player.ClearQueue();
        player.Current = null;
        Log($"offline mode on: bar heart, bar artist link, queue Save as playlist, a mood enabled {string.Join(" ", modeOn)}; off again {string.Join(" ", modeOff)} -> " +
            (!modeOn.Any(e => e) && modeOff.All(e => e) ? "ok" : "WRONG"));
        engineField.SetValue(App.Offline, realEngine);
        Log($"offline: bar heart enabled {barHeart}, Track page Like {pageLike}, Radio {pageRadio}, hero Download {heroDownload}; a collection's heart {colHeartShown}, enabled {colHeartEnabled}; " +
            $"row heart {rowHeart}, New playlist {newPlaylist}, queue Save as playlist {queueSave}, a mix's Save as playlist {mixSave}, bar artist link {barArtist}, album artist link {albumArtist} -> " +
            (!barHeart && !pageLike && !pageRadio && !heroDownload && colHeartShown == Visibility.Visible && !colHeartEnabled
             && !rowHeart && !newPlaylist && !queueSave && !mixSave && !barArtist && !albumArtist ? "ok" : "WRONG"));
        // Download clicked during a pin's fetch: the fetch's progress does not enable it again
        // while the download runs.
        var held = new TaskCompletionSource();
        view.DownloadBatch = _ => held.Task;
        view.Enter(Collection.Of(new Album { Id = "od1", Name = "Album" }), false, null);
        await Settle(100);
        var download = (Button)typeof(CollectionView).GetField("_download", Hidden)!.GetValue(view)!;
        typeof(CollectionView).GetMethod("Download", Hidden)!.Invoke(view, null);
        view.RenderOffline();
        bool duringDownload = download.IsEnabled;
        held.SetResult();
        await Settle(50);
        view.RenderOffline();
        bool afterDownload = download.IsEnabled;
        view.DownloadBatch = Menus.DownloadCollection;
        view.Leave();
        Log($"Download clicked, then a pin's progress: enabled {duringDownload} while it runs, {afterDownload} after it ended -> {(!duringDownload && afterDownload ? "ok" : "WRONG")}");
        // A fetch forgotten by a drop ends after a newer one for the same key started: a third
        // caller joins the newer one.
        int reads = 0;
        var realFetch = App.Library.FetchReply;
        var gates = new List<TaskCompletionSource>();
        string heldKey = Library.Key("album", "lf1");
        // Only this key's fetches are held: another key's (an earlier step's refresh) reads as usual.
        App.Library.FetchReply = key =>
        {
            if (key != heldKey) return realFetch(key);
            var g = new TaskCompletionSource();
            lock (gates) gates.Add(g);
            Interlocked.Increment(ref reads);
            g.Task.Wait();
            return new TracksReply { Error = "held" };
        };
        // The fetches start on the thread pool: wait for them (at most 5 s), not for a fixed time.
        async Task<bool> Started(int n)
        {
            var until = DateTime.UtcNow.AddSeconds(5);
            while (true)
            {
                lock (gates) if (gates.Count >= n) return true;
                if (DateTime.UtcNow > until)
                {
                    lock (gates) Log($"a held fetch for a dropped key: {gates.Count} of {n} started after 5 s -> WRONG");
                    return false;
                }
                await Task.Delay(5);
            }
        }
        void ReleaseAll()
        {
            lock (gates) foreach (var g in gates) g.TrySetResult();
            App.Library.FetchReply = realFetch;
        }
        var first = App.Library.GetAsync<TracksReply>(heldKey);
        App.Library.Drop("album");
        var second = App.Library.GetAsync<TracksReply>(heldKey);
        if (!await Started(1))
        {
            ReleaseAll();
            return;
        }
        lock (gates) gates[0].SetResult();
        await first;
        var third = App.Library.GetAsync<TracksReply>(heldKey);
        if (!await Started(2))
        {
            ReleaseAll();
            return;
        }
        lock (gates) foreach (var g in gates) g.TrySetResult();
        await Task.WhenAll(second, third);
        App.Library.FetchReply = realFetch;
        Log($"a fetch dropped while a newer one for its key runs ends, then a third caller asks: {reads} engine reads -> {(reads == 2 ? "ok" : "WRONG")}");
        // An album opened before the account was read: a Free plan read then disables Download and the offline switch.
        App.Session.SetAccount(new Account());
        view.Enter(Collection.Of(new Album { Id = "oa3", Name = "Album" }), false, null);
        await Settle(100);
        App.Session.SetAccount(new Account { UserId = "me", Name = "Me", LoggedIn = true, Premium = false });
        await Settle(100);
        bool freeDownload = ((Button)typeof(CollectionView).GetField("_download", Hidden)!.GetValue(view)!).IsEnabled;
        bool freeOffline = ((ToggleSwitch)typeof(CollectionView).GetField("_offline", Hidden)!.GetValue(view)!).IsEnabled;
        view.Leave();
        Log($"an album shown, then a Free account read: Download enabled {freeDownload}, Available offline enabled {freeOffline} -> {(!freeDownload && !freeOffline ? "ok" : "WRONG")}");
        App.Session.SetAccount(new Account());
    }

    // The title-bar avatar as AccountPanel builds it, for the picture URL in HARNESS_PICTURE.
    private static async Task TestAvatar()
    {
        string url = Environment.GetEnvironmentVariable("HARNESS_PICTURE") ?? "";
        if (url.Length == 0)
        {
            Log("skipped: set HARNESS_PICTURE (run.ps1 -Picture) to load a real profile picture");
            return;
        }
        var sw = Stopwatch.StartNew();
        var button = AccountPanel.TitleBarButton();
        await Host(button, 100, 80);
        App.Session.SetAccount(new Account { UserId = "1", Picture = url, Name = "Test User", LoggedIn = true });
        var picture = (PersonPicture)button.Content;
        if (picture.ProfilePicture is BitmapImage image)
        {
            image.ImageOpened += (_, _) => Log($"opened {image.PixelWidth}x{image.PixelHeight} after {sw.ElapsedMilliseconds} ms");
            image.ImageFailed += (_, e) => Log($"FAILED '{e.ErrorMessage}' after {sw.ElapsedMilliseconds} ms");
        }
        await Settle(8000);
        Log("avatar shows " + (picture.ProfilePicture == null ? "initials" : "the picture"));
        await Shot(button, "avatar");
    }
}
