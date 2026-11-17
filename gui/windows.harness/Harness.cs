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

namespace OpenDeezer;

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
                catch (Exception ex) { Harness.Log("  (async void threw " + ex.GetType().Name + ")"); }
            });

        public override SynchronizationContext CreateCopy() => new SafeContext(dq);
    }
}

internal static class Harness
{
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
                { ("bar", TestBar), ("states", TestStates), ("tiles", TestTiles), ("rows", TestRows), ("fills", TestFills), ("stale", TestStale), ("transport", TestTransport), ("chrome", TestChrome), ("layout", TestLayout), ("cards", TestCards), ("pane", TestPane), ("pages", TestPages), ("offline", TestOffline), ("cost", TestCost), ("memory", TestMemory), ("avatar", TestAvatar) })
            {
                if (only.Length > 0 && !only.Split(',').Contains(name)) continue;
                Log("=== " + name);
                try { await test(); }
                catch (Exception ex) { Log("TEST FAILED " + ex); }
            }
        }
        catch (Exception ex) { Log("FATAL " + ex); }
        Log("=== done");
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
        Log($"engine calls per tick while a fetch runs: done unchanged {still}, done moved {moved} (first tick {calls}); Changed with the id set unchanged {changed}x -> " +
            (still == 1 && changed == 0 ? "ok" : "WRONG"));
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
        List<Track> Tracks(int n, string prefix) => Enumerable.Range(0, n).Select(i => new Track { Id = prefix + i, Name = "Song " + i, ArtistLine = "Artist " + i % 50, AlbumName = "Album " + i % 200, DurationMs = 200000 }).ToList();
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
        await Settle();
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
        if (e == null) return "not found";
        DependencyObject? d = e;
        while (d != null)
        {
            if (d is UIElement { Visibility: Visibility.Collapsed }) return "HIDDEN (collapsed: " + d.GetType().Name + ")";
            if (ReferenceEquals(d, s_canvas)) return "visible";
            d = VisualTreeHelper.GetParent(d);
        }
        return "detached";
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
        foreach (double w in new double[] { 1400, 1180, 1072, 1000, 960, 900, 899, 856, 800, 720, 719, 608, 560, 480 })
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
            var transport = ((StackPanel)g.Children[1]).Children[0];
            double centre = transport.TransformToVisual(g).TransformPoint(new WPoint(((FrameworkElement)transport).ActualWidth / 2, 0)).X - w / 2;
            Log($"w={w}: columns {cd[0].ActualWidth:F0}/{cd[1].ActualWidth:F0}/{cd[2].ActualWidth:F0}, right group needs {need:F0} -> {verdict}, transport {centre:+0;-0;0} from the centre  [{string.Join(", ", names)}]");
            if (w is 900 or 720 or 560 or 480) await Shot(s_box, "bar-" + w);
        }

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
        player.Current = null;
        player.State = PlayState.Stopped;
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
        Log($"row-limited grid (2 rows, 40 albums, 1000 px): filled {fills.Count}x: {string.Join("; ", fills)}");

        // How wide a stretched tile gets, and what its picture decodes at.
        string art = new Uri(Path.Combine(AppContext.BaseDirectory, "opendeezer.png")).AbsoluteUri;
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

        // What a list of rows costs at rest: UI elements per realised row, none hovered.
        var full = new TrackList(ListMode.Full);
        full.SetItems(Enumerable.Range(0, 40).Select(i => new Track { Id = "e" + i, Name = "Song " + i, ArtistLine = "Artist", AlbumName = "Album", DurationMs = 200000 }).ToList());
        await Host(full.Root, 1000, 600);
        await Settle(1200);
        var containers = Descendants<ListViewItem>(full.Root).ToList();
        int elements = containers.Sum(c => Descendants<DependencyObject>(c).Count());
        Log($"rows at rest: {containers.Count} realised, {elements} elements, {(containers.Count == 0 ? 0 : elements / containers.Count)} per row");
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
        List<Track> Tracks(int n) => Enumerable.Range(0, n).Select(i => new Track { Id = "f" + i, Name = "Song " + i, ArtistLine = "Artist", AlbumName = "Album", DurationMs = 200000 }).ToList();
        var lists = Enumerable.Range(0, 5).Select(i => new Playlist { Id = "p" + i, Name = "Playlist " + i }).ToList();
        App.Library.Store(Library.HomeKey, new HomeReply { TopTracks = Tracks(20), Playlists = lists }, DateTime.UtcNow);

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
            Log($"Home visit {visit}: playlists grid filled {railFills}x, top tracks filled {topFills}x");
            home.Leave();
        }

        var sw = new Stopwatch();
        foreach (int count in new[] { 50, 3000 })
        {
            var p = new Playlist { Id = "big" + count, Name = "Playlist" };
            App.Library.Store(Library.Key("playlist", p.Id), new TracksReply { Tracks = Tracks(count) }, DateTime.UtcNow);
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
            Log($"Collection, Back to {count} tracks (same reply): list filled {listFills}x, {sw.Elapsed.TotalMilliseconds:F0} ms on the UI thread");
            view.Leave();
        }
    }

    // A collection page reached again while the Library still holds the same tracks reply:
    // the skipped refill must not leave another collection's hero or an emptied list behind.
    private static async Task TestStale()
    {
        var tracks = Enumerable.Range(0, 5).Select(i => new Track { Id = "s" + i, Name = "Song " + i, ArtistLine = "Artist", DurationMs = 200000 }).ToList();
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
    }

    // A local shuffle or repeat change is sent on the play chain, behind any DZPlay still resolving
    // its stream. This feeds the poll what the engine reports until that call runs: the old value.
    private static async Task TestTransport()
    {
        var player = App.Player;
        EngineSnap EngineStill(bool shuffle, RepeatMode repeat) => new(0, 0, 0, 0, "", null, -1, -1, "", true, "", false, repeat, shuffle,
            player.Volume, "", 0, Environment.TickCount64, DispatchGen: player._playDispatchGen);
        const int wait = 1400;
        // The play chain stays busy past the poll, as with a DZPlay on a slow connection.
        void Busy() => player._playChain = Task.Delay(wait + 2000);

        Busy();
        player.SetShuffle(true);
        await Task.Delay(wait);
        player.ApplySlow(EngineStill(shuffle: false, RepeatMode.Off));
        Log($"shuffle switched on; a poll {wait} ms later still reads the engine's old value: shuffle is {(player.Shuffle ? "on -> kept" : "off -> REVERTED")}");

        Busy();
        player.SetRepeat(RepeatMode.All);
        await Task.Delay(wait);
        player.ApplySlow(EngineStill(player.Shuffle, RepeatMode.Off));
        Log($"repeat set to All; a poll {wait} ms later still reads the engine's old value: repeat is {player.Repeat} -> {(player.Repeat == RepeatMode.All ? "kept" : "REVERTED")}");

        // The poll's rules, fed snapshots as the engine reports them.
        EngineSnap Snap(PlayState st, string nowId, long pos, long dur) => new((int)st, pos, dur, 0, nowId, null, -1, -1, "", false, "", false,
            player.Repeat, player.Shuffle, player.Volume, "", 0, Environment.TickCount64, DispatchGen: player._playDispatchGen);

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
        player.Current = null;
        player.State = PlayState.Stopped;
    }

    private sealed class EngineStandIn : IEngineReads
    {
        public int StateNow, LastErrorReads, NowReads;
        public long Version, NowVersion, PosNow, DurNow;
        public string NowJson = "{\"id\":\"t\"}";
        public long NowPlayingVersion() => NowVersion;
        public int FinishedCount() => 0;
        public int State() => StateNow;
        public long PositionMs() => PosNow;
        public long DurationMs() => DurNow;
        public int QueueIndex() => -1;
        public long QueueVersion() => Version;
        public string LastError() { LastErrorReads++; return "failed"; }
        public byte[] NowPlayingUtf8() { NowReads++; return System.Text.Encoding.UTF8.GetBytes(NowJson); }
        public string Format() => "";
        public bool IsPreview() => false;
        public RepeatMode Repeat() => RepeatMode.Off;
        public bool Shuffle() => false;
        public double Volume() => 1;
        public string Connected() => "";
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
                // Each shown element's centre against the bar's (16), and the search text's against its box.
                var off = bar.Root.Children.OfType<FrameworkElement>().Append(account)
                    .Where(e => e.Visibility == Visibility.Visible && e.ActualHeight > 0 && e is not StackPanel)
                    .Select(e => (e, e is ContentPresenter { Content: FrameworkElement c } ? c : e))
                    .Select(p => $"{p.Item2.GetType().Name} {p.Item2.TransformToVisual(bar.Root).TransformPoint(new WPoint(0, p.Item2.ActualHeight / 2)).Y - Size.TitleBar / 2:+0.0;-0.0;0}");
                Log("  centre offsets: " + string.Join(", ", off));
                var text = Descendants<TextBlock>(search.Root).FirstOrDefault(t => t.Text.Length > 0 && Shown(t) == "visible");
                if (text != null)
                    Log($"  search text centre {text.TransformToVisual(search.Root).TransformPoint(new WPoint(0, text.ActualHeight / 2)).Y - search.Root.ActualHeight / 2:+0.0;-0.0;0} from the box's");
            }
        }
        // The title-bar flyout's panel, with the name and plan its opening fills in.
        var holder = (StackPanel)((Flyout)account.Flyout).Content;
        var panel = holder.Children.OfType<Border>().First();
        holder.Children.Remove(panel);
        var flyout = (StackPanel)panel.Child;
        var who = (StackPanel)flyout.Children[0];
        ((TextBlock)who.Children[0]).Text = AccountPanel.Name;
        ((TextBlock)who.Children[1]).Text = AccountPanel.Plan;
        panel.HorizontalAlignment = HorizontalAlignment.Left;
        await Host(panel, 600, 400);
        Log($"account flyout content {flyout.ActualWidth:0} x {flyout.ActualHeight:0}: " +
            string.Join(", ", flyout.Children.OfType<FrameworkElement>().Select(c => $"{c.GetType().Name} {c.DesiredSize.Width:0}")));
        await Shot(flyout, "account-flyout");
        var button = Ui.Button("Switch account", () => { });
        await Host(button, 300, 100);
        Log($"text button corner radius {button.CornerRadius.TopLeft} -> {(button.CornerRadius.TopLeft > 0 ? "rounded" : "SQUARE")}");
    }

    // Every page with data: where its title sits and the gap under it (the gaps must all be equal),
    // then each distinct margin, padding and spacing the page sets, with its count. A value used
    // once across all pages is marked: a candidate inconsistency.
    private static async Task TestLayout()
    {
        void Seed(string key, Reply r) => App.Library.Store(key, r, DateTime.UtcNow);
        var tracks = Enumerable.Range(0, 12).Select(i => new Track { Id = "l" + i, Name = "Song " + i, ArtistLine = "Artist", AlbumName = "Album", DurationMs = 200000 }).ToList();
        var albums = Enumerable.Range(0, 12).Select(i => new Album { Id = "a" + i, Name = "Album " + i }).ToList();
        var artists = Enumerable.Range(0, 12).Select(i => new ArtistInfo { Id = "r" + i, Name = "Artist " + i }).ToList();
        var playlists = Enumerable.Range(0, 12).Select(i => new Playlist { Id = "p" + i, Name = "Playlist " + i, TrackCount = i }).ToList();
        Seed(Library.HomeKey, new HomeReply { TopTracks = tracks, Playlists = playlists });
        Seed(Library.ChartsKey, new CatalogReply { Tracks = tracks, Albums = albums, Artists = artists, Playlists = playlists });
        Seed(Library.PlaylistsKey, new PlaylistsReply { Playlists = playlists });
        Seed(Library.Key("artist", "x"), new ArtistProfile { Artist = new ArtistInfo { Id = "x", Name = "Artist" }, Top = tracks, Albums = albums, Related = artists });
        Seed(Library.Key("playlist", "p0"), new TracksReply { Tracks = tracks });

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
            if (title == null) Log($"{name}: no page title");
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
            Contributors = new[] { new Contributor { Role = "Composer", Names = new[] { "Thomas Bangalter", "Guy-Manuel de Homem-Christo" } }, new Contributor { Role = "Lyricist", Names = new[] { "Edwin Birdsong" } } },
        });
        await Settle(600);
        var details = track._details;
        var credits = track._credits;
        Log($"track: {details.RowDefinitions.Count} detail rows, {credits.RowDefinitions.Count} credit rows, title '{track._hero._title.Text}'");
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
        void Seed(string key, Reply r) => App.Library.Store(key, r, DateTime.UtcNow);
        var tracks = Enumerable.Range(0, 3000).Select(i => new Track { Id = "c" + i, Name = "Song " + i, ArtistLine = "Artist", DurationMs = 200000, ArtworkUrl = urls[i] }).ToList();
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
            (bitmaps.Count > sources.Count ? "pictures are made again" : "each picture made once"));

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
            var cards = Descendants<CommunityToolkit.WinUI.Controls.SettingsCard>(view.Root).Where(c => Shown(c) == "visible" && c.ActualHeight > 0).ToList();
            var heights = cards.Select(c => c.ActualHeight).ToList();
            var title = Descendants<TextBlock>(view.Root).First(t => t.Style == Ui.TextStyle(TextKind.PageTitle));
            double titleX = title.TransformToVisual(view.Root).TransformPoint(new WPoint(0, 0)).X;
            Log($"w={w}: {cards.Count} cards, height min {heights.Min():0} max {heights.Max():0}; title at x={titleX:0}");
            foreach (var c in cards.Where(c => c.ActualHeight > 60))
            {
                var parts = Descendants<FrameworkElement>(c).Where(e => !string.IsNullOrEmpty(e.Name) && e.ActualHeight > 0 && Shown(e) == "visible")
                    .Select(e => $"{e.Name}={e.ActualHeight:0}");
                Log($"  tall: '{c.Header}' {c.ActualHeight:0} px, padding {c.Padding}, content {(c.Content as FrameworkElement)?.GetType().Name} {(c.Content as FrameworkElement)?.ActualHeight:0}: {string.Join(" ", parts.Take(12))}");
            }
            if (w == 480) await Shot(s_box, "cards-480");
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
