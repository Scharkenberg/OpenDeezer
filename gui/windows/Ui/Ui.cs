// Element factories: text, icons, buttons, chips, panels and settings rows get their styles here.

using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using CommunityToolkit.WinUI.Controls;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Documents;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;

namespace PurpleDisc;

internal enum TextKind { PageTitle, HeroTitle, SectionTitle, Strong, Body, Caption, SecondaryBody, SecondaryCaption, TertiaryCaption }

internal enum IconSize { Small, Normal, Large }

internal static class Ui
{
    private static readonly HashSet<object> s_quiet = new();
    private static readonly Dictionary<UIElement, Action> s_fading = new();

    internal static class Styles
    {
        public static Style RowTitle => Get("RowTitleText");
        public static Style RowTitlePlaying => Get("RowTitlePlayingText");
        public static Style RowTitlePlayed => Get("RowTitlePlayedText");
        public static Style LyricsLine => Get("LyricsLineText");
        public static Style LyricsLineInactive => Get("LyricsLineInactiveText");
        public static Style LyricsPlain => Get("LyricsPlainText");
        public static Style IconButton => Get("IconButtonStyle");
        public static Style IconButtonActive => Get("IconButtonActiveStyle");
        public static Style TrackRow => Get("TrackRowStyle");
        public static Style TrackRowHover => Get("TrackRowHoverStyle");
        public static Style TrackListItem => Get("TrackListItemStyle");
        public static Style IconToggle => Get("IconToggleButtonStyle");
        public static Style Subtle => Get("SubtleButtonStyle");
        public static Style SubtleFlush => Get("SubtleFlushButtonStyle");
        public static Style Accent => Get("AccentButtonStyle");
        public static Style Play => Get("PlayButtonStyle");
        public static Style PlayRing => Get("PlayRingStyle");
        public static Style Link => Get("LinkButtonStyle");
        public static Style Skeleton => Get("SkeletonBorder");
        public static Style Card => Get("CardBorder");
        public static Style Bar => Get("BarGrid");

        public static Style Get(string key) => (Style)Application.Current.Resources[key];
    }

    private static readonly string[] s_textStyles =
    [
        "PageTitleText", "HeroTitleText", "SectionTitleText", "StrongText", "BodyText", "CaptionText",
        "SecondaryBodyText", "SecondaryCaptionText", "TertiaryCaptionText",
    ];

    public static Style TextStyle(TextKind kind) => Styles.Get(s_textStyles[(int)kind]);

    // A one-line label scrolls when it is cut off (Labels.Scrolls); wrapped text never does.
    public static TextBlock Text(string text, TextKind kind = TextKind.Body, bool wrap = false)
    {
        var tb = new TextBlock
        {
            Text = text,
            Style = TextStyle(kind),
            TextWrapping = wrap ? TextWrapping.Wrap : TextWrapping.NoWrap,
            TextTrimming = TextTrimming.CharacterEllipsis,
        };
        return wrap ? tb : tb.Scrolls();
    }

    public static TextBlock Time(long ms, TextKind kind = TextKind.SecondaryCaption)
    {
        var tb = Text(Fmt.Time(ms), kind);
        Typography.SetNumeralAlignment(tb, FontNumeralAlignment.Tabular);
        return tb;
    }

    public static void SetText(TextBlock tb, string text)
    {
        if (tb.Text != text) tb.Text = text;
    }

    public static FontIcon Icon(string glyph, IconSize size = IconSize.Normal, bool accent = false) => new()
    {
        Glyph = glyph,
        Style = Styles.Get((accent ? "Accent" : "") + "Icon" + (size == IconSize.Small ? "12" : size == IconSize.Large ? "20" : "16")),
    };

    public static void Name(UIElement e, string name)
    {
        ToolTipService.SetToolTip(e, name);
        AutomationProperties.SetName(e, name);
    }

    // Button and row sizes come from Size here, not from the styles, so each is written once.
    public static T Square<T>(T e, double size) where T : FrameworkElement
    {
        e.Width = size;
        e.Height = size;
        return e;
    }

    // `turn`: the glyph turns once around on a click (the settings gear); `nudge`: pixels it moves
    // toward its direction and back (next, previous).
    public static Button IconButton(string glyph, string nameKey, Action? onClick, IconSize size = IconSize.Normal, double side = Size.IconButton,
        bool turn = false, float nudge = 0)
    {
        var b = Square(new Button { Content = Icon(glyph, size), Style = Styles.IconButton }, side);
        Name(b, Loc.S(nameKey));
        Motion.Press(b);
        if (turn) b.Click += (_, _) => Motion.Turn((UIElement)b.Content);
        if (nudge != 0) b.Click += (_, _) => Motion.Nudge((UIElement)b.Content, nudge);
        if (onClick != null) b.Click += (_, _) => onClick();
        return b;
    }

    // A track's heart; ShowLiked fills it in the accent color. A like (not an unlike) pops it.
    public static Button LikeButton(double size, Action onClick)
    {
        Button? like = null;
        like = IconButton(Icons.Heart, "Menu_Like", () =>
        {
            if (((FontIcon)like!.Content).Glyph == Icons.Heart) Motion.Like(like);
            onClick();
        }, side: size);
        return like;
    }

    public static void ShowLiked(Button like, bool liked)
    {
        var icon = (FontIcon)like.Content;
        icon.Glyph = liked ? Icons.HeartFill : Icons.Heart;
        icon.Style = Styles.Get(liked ? "AccentIcon16" : "Icon16");
        Name(like, Loc.S(liked ? "Menu_Unlike" : "Menu_Like"));
    }

    // The round accent play button.
    public static Button PlayButton(object content)
    {
        var b = Square(new Button { Content = content, Style = Styles.Play }, Size.PlayButton);
        b.CornerRadius = new CornerRadius(Size.PlayButton / 2);
        Motion.Press(b);
        return b;
    }

    // `turn`: the glyph turns once around when the user toggles it (not when the app sets it quietly).
    public static ToggleButton IconToggle(string glyph, string nameKey, Action<bool>? onToggle, bool turn = false)
    {
        var b = Square(new ToggleButton { Content = Icon(glyph), Style = Styles.IconToggle }, Size.IconButton);
        Name(b, Loc.S(nameKey));
        Motion.Press(b);
        // Checked and Unchecked also fire for screen readers' Toggle, which raises no Click.
        if (onToggle == null) return b;
        b.Checked += (_, _) =>
        {
            if (s_quiet.Contains(b)) return;
            if (turn) Motion.Turn((UIElement)b.Content);
            onToggle(true);
        };
        b.Unchecked += (_, _) =>
        {
            if (s_quiet.Contains(b)) return;
            if (turn) Motion.Turn((UIElement)b.Content);
            onToggle(false);
        };
        return b;
    }

    public static Button Button(string text, Action onClick, string? glyph = null, bool accent = false)
    {
        var b = new Button { Content = glyph == null ? text : Row(Space.S, Icon(glyph), Text(text)) };
        // A local Style of null would also drop the Fluent implicit style (square corners).
        if (accent) b.Style = Styles.Accent;
        AutomationProperties.SetName(b, text);
        b.Click += (_, _) => onClick();
        return b;
    }

    // `value`: a link among plain values, in their color and alignment.
    public static HyperlinkButton Link(string text, Action onClick, bool value = false)
    {
        var b = new HyperlinkButton { Content = text, Style = value ? Styles.Get("ValueLinkStyle") : Styles.Link };
        b.Click += (_, _) => onClick();
        return b;
    }

    public static TextBlock ChipText(string text) => new() { Text = text, Style = Styles.Get("ChipText") };

    public static Border Chip(TextBlock text) => new() { Style = Styles.Get("ChipBorder"), Child = text };

    // Its end margin, not column spacing, keeps the gap: a hidden chip leaves none.
    public static Border ExplicitChip()
    {
        var b = Chip(ChipText("E"));
        b.Margin = Pad.InlineEnd;
        Name(b, Loc.S("Tooltip_Explicit"));
        return b;
    }

    public static StackPanel Stack(double spacing, params UIElement?[] elements) => Panel(new StackPanel { Spacing = spacing }, elements);

    public static StackPanel Row(double spacing, params UIElement?[] elements) =>
        Panel(new StackPanel { Orientation = Orientation.Horizontal, Spacing = spacing }, elements);

    private static StackPanel Panel(StackPanel p, UIElement?[] elements)
    {
        foreach (var c in elements)
            if (c != null) p.Children.Add(c);
        return p;
    }

    // `cols` like "auto * auto"; the n-th child goes in the n-th column (null skips one).
    public static Grid Grid(string cols, double spacing, params UIElement?[] elements) =>
        Fill(new Grid { ColumnSpacing = spacing }, cols, elements);

    public static Grid Fill(Grid g, string cols, params UIElement?[] elements)
    {
        foreach (var c in cols.Split(' ', StringSplitOptions.RemoveEmptyEntries))
            g.ColumnDefinitions.Add(new ColumnDefinition { Width = Length(c) });
        Place(g, elements, Microsoft.UI.Xaml.Controls.Grid.SetColumn);
        return g;
    }

    public static Grid Rows(string rows, params UIElement?[] elements)
    {
        var g = new Grid();
        foreach (var r in rows.Split(' ', StringSplitOptions.RemoveEmptyEntries))
            g.RowDefinitions.Add(new RowDefinition { Height = Length(r) });
        Place(g, elements, Microsoft.UI.Xaml.Controls.Grid.SetRow);
        return g;
    }

    private static void Place(Grid g, UIElement?[] elements, Action<FrameworkElement, int> set)
    {
        for (int i = 0; i < elements.Length; i++)
        {
            if (elements[i] is not FrameworkElement e) continue;
            set(e, i);
            g.Children.Add(e);
        }
    }

    private static GridLength Length(string s) => s switch
    {
        "auto" => GridLength.Auto,
        "*" => new GridLength(1, GridUnitType.Star),
        _ when s.EndsWith('*') => new GridLength(double.Parse(s[..^1], System.Globalization.CultureInfo.InvariantCulture), GridUnitType.Star),
        _ => new GridLength(double.Parse(s, System.Globalization.CultureInfo.InvariantCulture)),
    };

    private static ScrollViewer Scroller(UIElement content) => new()
    {
        Content = content,
        HorizontalScrollMode = ScrollMode.Disabled,
        HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled,
    };

    public static Visibility Show(bool visible) => visible ? Visibility.Visible : Visibility.Collapsed;

    // A flyout of a fixed width: what it shows (an opened list, a longer name) never resizes it.
    public static Flyout Flyout(string? titleKey, UIElement content)
    {
        var panel = Stack(Space.S, titleKey == null ? null : Text(Loc.S(titleKey), TextKind.Strong), content);
        panel.Width = Size.FlyoutWidth;
        panel.FlowDirection = Loc.FlowDirection;
        return new Flyout { Content = panel };
    }

    public static Button FlyoutRow(string glyph, string text, string? subtitle, bool active, Action onClick)
    {
        var lines = Stack(0, Text(text), subtitle == null ? null : Text(subtitle, TextKind.SecondaryCaption));
        lines.VerticalAlignment = VerticalAlignment.Center;
        var check = Icon(Icons.Check, IconSize.Normal, accent: true);
        check.Visibility = Show(active);
        var b = new Button
        {
            Style = Styles.Subtle,
            MinHeight = Size.MenuRow,
            HorizontalAlignment = HorizontalAlignment.Stretch,
            Content = Grid("auto * auto", Space.M, Icon(glyph), lines, check),
        };
        AutomationProperties.SetName(b, text);
        b.Click += (_, _) => onClick();
        return b;
    }

    // A settings row.
    public static SettingsCard Setting(string headerKey, string? descKey, string? glyph, FrameworkElement? control)
    {
        var card = Compact(new SettingsCard
        {
            Header = Loc.S(headerKey),
            // The toolkit's properties are non-nullable but accept null.
            Description = descKey == null ? null! : Loc.S(descKey),
            HeaderIcon = glyph == null ? null! : new FontIcon { Glyph = glyph },
            Content = control,
        });
        // A control that stretches (a field row: a Grid with a star column) goes under the title, the
        // card's whole width: beside it, a long value squeezes the title to a few letters a line.
        if (control is Grid g && System.Linq.Enumerable.Any(g.ColumnDefinitions, c => c.Width.IsStar))
        {
            card.ContentAlignment = ContentAlignment.Vertical;
            card.HorizontalContentAlignment = HorizontalAlignment.Stretch;
        }
        return card;
    }

    // The toolkit's templates read their sizes from the toolkit's own dictionary first, so the
    // app's values (App.xaml, keys starting with "Settings") only apply from the card's own resources.
    public static T Compact<T>(T card) where T : FrameworkElement
    {
        foreach (var (key, value) in s_settingsSizes.Value) card.Resources[key] = value;
        // An expander's header is a card of its template that takes neither: its padding is set here.
        if (card is SettingsExpander) card.Loaded += (_, _) =>
        {
            if (HeaderCard(card) is { } header) header.Padding = (Thickness)card.Resources["SettingsExpanderHeaderPadding"];
        };
        return card;

        static SettingsCard? HeaderCard(DependencyObject root)
        {
            for (int i = 0; i < VisualTreeHelper.GetChildrenCount(root); i++)
            {
                var c = VisualTreeHelper.GetChild(root, i);
                if ((c as SettingsCard ?? HeaderCard(c)) is { } found) return found;
            }
            return null;
        }
    }

    private static readonly Lazy<List<(object Key, object Value)>> s_settingsSizes = new(() =>
    {
        var list = new List<(object, object)>();
        foreach (var (key, value) in Application.Current.Resources)
            if (key is string k && k.StartsWith("Settings", StringComparison.Ordinal) && value is not Style) list.Add((key, value));
        return list;
    });

    public static SettingsCard SettingToggle(string headerKey, string onKey, string offKey, string glyph, bool value, Action<bool> changed)
    {
        var card = Setting(headerKey, value ? onKey : offKey, glyph, null);
        card.Content = Switch(value, headerKey, v =>
        {
            card.Description = Loc.S(v ? onKey : offKey);
            changed(v);
        });
        return card;
    }

    // A settings group's heading: a section title, as in the other pages.
    public static TextBlock Group(string key)
    {
        var t = Text(Loc.S(key), TextKind.SectionTitle);
        t.Margin = Pad.GroupTop;
        return t;
    }

    public static ToggleSwitch Switch(bool on, string nameKey, Action<bool> toggled)
    {
        var sw = new ToggleSwitch { IsOn = on, OnContent = "", OffContent = "", MinWidth = 0 };
        AutomationProperties.SetName(sw, Loc.S(nameKey));
        sw.Toggled += (_, _) =>
        {
            if (!s_quiet.Contains(sw)) toggled(sw.IsOn);
        };
        return sw;
    }

    // A switch with its name before it, both centered on the line; `more` follows them.
    public static StackPanel LabeledSwitch(ToggleSwitch sw, string nameKey, params FrameworkElement[] more)
    {
        var row = Row(Space.S, [Text(Loc.S(nameKey), TextKind.SecondaryBody), sw, .. more]);
        foreach (var e in row.Children) ((FrameworkElement)e).VerticalAlignment = VerticalAlignment.Center;
        return row;
    }

    public static Slider Slider(double min, double max, double value, string nameKey, Action<double> changed)
    {
        var s = new Slider { Minimum = min, Maximum = max, Value = value, VerticalAlignment = VerticalAlignment.Center };
        AutomationProperties.SetName(s, Loc.S(nameKey));
        s.ValueChanged += (_, e) =>
        {
            if (!s_quiet.Contains(s)) changed(e.NewValue);
        };
        return s;
    }

    public static ComboBox Combo(string[] items, int selected, Action<int> changed)
    {
        var c = new ComboBox { MinWidth = Size.FieldMin };
        foreach (var i in items) c.Items.Add(i);
        FitWidest(c);
        c.SelectedIndex = selected;
        c.SelectionChanged += (_, _) =>
        {
            if (!s_quiet.Contains(c) && c.SelectedIndex >= 0) changed(c.SelectedIndex);
        };
        return c;
    }

    // A ComboBox measures only its selected item until its list has been open, then its widest
    // item: sized up front to the widest (and again, once, after the items change), opening it never
    // changes its width, or that of the surface around it.
    private static void FitWidest(ComboBox c)
    {
        double floor = c.MinWidth;
        void Fit()
        {
            // The combo draws in the app's font once it is in the tree; its own FontFamily is the
            // system default until then.
            // Not a cast: the Native AOT build does not hand the resource back as a FontFamily
            // (InvalidCastException at startup); the combo's own font stands in.
            var probe = new TextBlock { FontSize = c.FontSize, FontFamily = Application.Current.Resources["ContentControlThemeFontFamily"] as FontFamily ?? c.FontFamily };
            double widest = 0;
            foreach (var item in c.Items)
            {
                probe.Text = item as string ?? (item as ContentControl)?.Content as string ?? "";
                probe.Measure(new Windows.Foundation.Size(double.PositiveInfinity, double.PositiveInfinity));
                widest = Math.Max(widest, probe.DesiredSize.Width);
            }
            c.MinWidth = Math.Max(floor, Math.Ceiling(widest) + Size.ComboChrome);
        }
        // A list filled one item at a time is measured once, after the fill.
        bool queued = false;
        c.Items.VectorChanged += (_, _) =>
        {
            if (queued) return;
            queued = c.DispatcherQueue.TryEnqueue(() =>
            {
                queued = false;
                Fit();
            });
        };
        Fit();
    }

    // A programmatic update that the control's own change handler ignores.
    public static void Quietly(object control, Action update)
    {
        s_quiet.Add(control);
        try { update(); }
        finally { s_quiet.Remove(control); }
    }

    public static void OnWidthClass(FrameworkElement e, Action<WidthClass> changed) => OnWidth(e, Breakpoint.Of, changed);

    public static void OnWidthAbove(FrameworkElement e, double min, Action<bool> changed) => OnWidth(e, w => w >= min, changed);

    public static void OnWidth<T>(FrameworkElement e, Func<double, T> classify, Action<T> changed) where T : struct
    {
        T? last = null;
        e.SizeChanged += (_, args) =>
        {
            var c = classify(args.NewSize.Width);
            if (last is { } l && l.Equals(c)) return;
            last = c;
            changed(c);
        };
    }

    // A page: its title (PageTitle), then the rest, which brings no outer spacing of its own.
    public static ScrollViewer Page(params UIElement?[] children) => Page(double.PositiveInfinity, children);

    public static ScrollViewer Page(double maxWidth, params UIElement?[] children)
    {
        var column = Stack(0, children);
        column.MaxWidth = maxWidth;
        // A grid between scroller and column, so a capped column is centered in the viewport.
        var host = new Grid();
        host.Children.Add(column);
        var scroller = Scroller(host);
        PagePadding(scroller, p => column.Padding = p);
        return scroller;
    }

    public static Grid PageTitle(string text, UIElement? trailing = null) => PageTitle(Text(text, TextKind.PageTitle), trailing);

    // `trailing` (a button or a link) sits at the title's end.
    public static Grid PageTitle(TextBlock title, UIElement? trailing = null)
    {
        title.VerticalAlignment = VerticalAlignment.Center;
        if (trailing is FrameworkElement end) end.VerticalAlignment = VerticalAlignment.Center;
        var g = Grid("* auto", Space.M, title, trailing);
        g.Margin = Pad.Header;
        return g;
    }

    // The page padding for `element`'s width: narrower below the Narrow breakpoint.
    public static void PagePadding(FrameworkElement element, Action<Thickness> set)
    {
        set(Pad.Page);
        OnWidthClass(element, c => set(c == WidthClass.Narrow ? Pad.PageNarrow : Pad.Page));
    }

    // The window's display scale (physical pixels per logical one): set from the window's DPI
    // before its content loads, then followed through its XamlRoot. Pictures decode at shown
    // size times this.
    public static double Scale { get; set; } = 1;

    // A window's display scale: its DPI over the 96 of 100 %.
    public static double DpiScale(IntPtr hwnd) => NativeMethods.GetDpiForWindow(hwnd) is var dpi and > 0 ? dpi / 96.0 : 1;

    public static void FollowScale(XamlRoot root)
    {
        Scale = root.RasterizationScale;
        root.Changed += (r, _) => Scale = r.RasterizationScale;
    }

    // The app's mark, drawn at the size shown.
    public static LogoMark Logo(double size) => new(size);

    // Decoded in physical pixels at the shown size: a picture drawn through a brush (art tiles,
    // PersonPicture) gets no display scale at decode time, so a logical size would blur above 100 %.
    // `reuse` (row and tile art, which recycling binds again and again): the last Count.Bitmaps
    // such pictures up to a tile's size are kept, so a row scrolled back into view does not decode its
    // picture again; one that fails to load is dropped and asked for anew next time. Larger pictures
    // (a widened tile) are not kept.
    public static BitmapImage? Bitmap(string url, double decodeSize, bool reuse = false)
    {
        int pixels = Pixels(decodeSize);
        string sized = SizedArt(url, pixels);
        string key = Key(pixels, sized);
        reuse &= pixels <= Pixels(Size.Tile);
        if (reuse && s_bitmaps.TryGetValue(key, out var node))
        {
            s_bitmapOrder.Remove(node);
            s_bitmapOrder.AddFirst(node);
            return node.Value.Image;
        }
        if (!Uri.TryCreate(sized, UriKind.Absolute, out var uri)) return null;
        var image = new BitmapImage { DecodePixelType = DecodePixelType.Physical, DecodePixelWidth = pixels, UriSource = uri };
        if (!reuse) return image;
        image.ImageFailed += (_, _) =>
        {
            if (!s_bitmaps.TryGetValue(key, out var failed) || !ReferenceEquals(failed.Value.Image, image)) return;
            s_bitmaps.Remove(key);
            s_bitmapOrder.Remove(failed);
        };
        s_bitmaps[key] = s_bitmapOrder.AddFirst((key, image));
        if (s_bitmapOrder.Count > Count.Bitmaps)
        {
            s_bitmaps.Remove(s_bitmapOrder.Last!.Value.Key);
            s_bitmapOrder.RemoveLast();
        }
        return image;
    }

    internal static readonly Dictionary<string, LinkedListNode<(string Key, BitmapImage Image)>> s_bitmaps = new();
    private static readonly LinkedList<(string Key, BitmapImage Image)> s_bitmapOrder = new();

    // The key of a picture as Bitmap fetches and keeps it: its decode pixels (a new display scale
    // makes a new key) and the CDN URL asked for (two URLs that differ only in CDN size match).
    public static string ArtKey(string url, double decodeSize)
    {
        int pixels = Pixels(decodeSize);
        return Key(pixels, SizedArt(url, pixels));
    }

    private static string Key(int pixels, string sizedUrl) => pixels + " " + sizedUrl;

    private static int Pixels(double size) => (int)Math.Ceiling(size * Scale);

    // Deezer's CDN serves an image at any of its sizes ("/250x250-"): ask for the smallest that
    // covers the decode size, up or down from the size in the URL (its first "/<digits>x<digits>-").
    internal static string SizedArt(string url, int pixels)
    {
        if (!url.Contains("dzcdn.net/images/", StringComparison.Ordinal)) return url;
        int side = Array.Find(Size.CdnArt, s => s >= pixels);
        if (side == 0) side = Size.CdnArt[^1];
        for (int slash = url.IndexOf('/'); slash >= 0; slash = url.IndexOf('/', slash + 1))
        {
            int w = Digits(url, slash + 1), x = slash + 1 + w;
            if (w == 0 || x >= url.Length || url[x] != 'x') continue;
            int h = Digits(url, x + 1), dash = x + 1 + h;
            if (h == 0 || dash >= url.Length || url[dash] != '-') continue;
            return string.Concat(url.AsSpan(0, slash), $"/{side}x{side}", url.AsSpan(dash));
        }
        return url;
    }

    private static int Digits(string s, int from)
    {
        int n = 0;
        while (from + n < s.Length && char.IsAsciiDigit(s[from + n])) n++;
        return n;
    }

    public static void Fades(UIElement e, int ms) => e.OpacityTransition = new ScalarTransition { Duration = TimeSpan.FromMilliseconds(ms) };

    public static void Reveal(UIElement e, bool on)
    {
        e.Opacity = on ? 1 : 0;
        e.IsHitTestVisible = on;
    }

    public static async void CrossFade(UIElement e, Action change)
    {
        bool fading = s_fading.ContainsKey(e);
        s_fading[e] = change;
        if (fading) return;
        Fades(e, Timing.CrossFadeMs / 2);
        e.Opacity = 0;
        await System.Threading.Tasks.Task.Delay(Timing.CrossFadeMs / 2);
        s_fading.Remove(e, out var latest);
        latest!();
        e.Opacity = 1;
    }

    // Applies `next` as one insert, remove or replace so the list's transition runs; false when it is not one.
    public static bool TryEdit<T>(ObservableCollection<T> items, IReadOnlyList<T> next) where T : class
    {
        int n = items.Count, m = next.Count;
        int i = 0;
        while (i < n && i < m && ReferenceEquals(items[i], next[i])) i++;
        if (n == m && i == n) return true;
        if (m == n - 1 && SameTail(items, next, i + 1, i)) items.RemoveAt(i);
        else if (m == n + 1 && SameTail(items, next, i, i + 1)) items.Insert(i, next[i]);
        else if (m == n && SameTail(items, next, i + 1, i + 1)) items[i] = next[i];
        else return false;
        return true;
    }

    private static bool SameTail<T>(ObservableCollection<T> items, IReadOnlyList<T> next, int at, int from) where T : class
    {
        for (; at < items.Count && from < next.Count; at++, from++)
            if (!ReferenceEquals(items[at], next[from])) return false;
        return at == items.Count && from == next.Count;
    }

    public static Border Skeleton(double width, double height) => new()
    {
        Style = Styles.Skeleton,
        Width = width,
        Height = height,
        HorizontalAlignment = HorizontalAlignment.Left,
    };
}

internal enum ArtShape { Small, Large, Circle }

// Artwork over a placeholder, so layout holds while the image loads. Without a size it fills a
// 1:1 ConstrainedBox (a tile, which stretches with its grid) and decodes at the widest it has
// been laid out, in Size.TileDecodeStep steps. A circle is a person picture with initials until
// the image arrives. `reuse`: art that recycling binds again and again (rows, tiles), whose
// pictures Ui.Bitmap keeps.
internal sealed class Art
{
    // Art that grows while it shows a picture keeps it; the sharper ones load once no art has
    // changed size for Timing.ArtRestMs (a window, pane or grip being dragged), each over the one
    // shown until it is decoded.
    private static readonly HashSet<Art> s_grown = new();
    private static Microsoft.UI.Dispatching.DispatcherQueueTimer? s_rest;

    private readonly ImageBrush _brush = new() { Stretch = Stretch.UniformToFill };
    private readonly PersonPicture? _person;
    private readonly Grid _box;
    private readonly bool _reuse;
    private readonly string _style = "";
    private double _decode, _wanted;
    private string _key = "", _url = "";

    public FrameworkElement Root { get; }

    public Art(ArtShape shape, double size = double.NaN, bool reuse = false)
    {
        _reuse = reuse;
        _decode = _wanted = double.IsNaN(size) ? Size.Tile : size;
        _box = new Grid { Width = size, Height = size };
        if (double.IsNaN(size))
            _box.SizeChanged += (_, e) =>
            {
                double widest = Math.Ceiling(e.NewSize.Width / Size.TileDecodeStep) * Size.TileDecodeStep;
                if (widest > _wanted)
                {
                    _wanted = widest;
                    // Art that shows no picture yet (laid out for the first time) gets its size at once.
                    if (!Showing)
                    {
                        Sharpen();
                        return;
                    }
                    s_grown.Add(this);
                }
                // While any art still changes size, the grown ones wait on.
                if (s_grown.Count > 0) Rest();
            };
        if (shape == ArtShape.Circle)
        {
            // PersonPicture keeps its first size, so a Viewbox scales it to the box (a resized hero).
            _person = new PersonPicture { Width = _decode, Height = _decode };
            _box.Children.Add(new Viewbox { Child = _person });
        }
        else
        {
            _style = shape == ArtShape.Small ? "SmallArtBorder" : "ArtBorder";
            _box.Children.Add(new Border { Style = Ui.Styles.Get(_style) });
            _box.Children.Add(new Border { Style = Ui.Styles.Get(_style), Background = _brush });
        }
        Root = double.IsNaN(size) ? new ConstrainedBox { AspectRatio = 1.0, Content = _box } : _box;
    }

    private static void Rest()
    {
        if (s_rest == null)
        {
            s_rest = Microsoft.UI.Dispatching.DispatcherQueue.GetForCurrentThread().CreateTimer();
            s_rest.Interval = TimeSpan.FromMilliseconds(Timing.ArtRestMs);
            s_rest.IsRepeating = false;
            s_rest.Tick += (_, _) =>
            {
                var grown = new List<Art>(s_grown);
                s_grown.Clear();
                foreach (var a in grown) a.Sharpen();
            };
        }
        s_rest.Stop();
        s_rest.Start();
    }

    // The picture again at the decode size the art has grown to; a new one decodes over the one shown.
    private void Sharpen()
    {
        if (_wanted <= _decode) return;
        _decode = _wanted;
        string key = Ui.ArtKey(_url, _decode);
        if (_url.Length == 0 || key == _key) return;
        _key = key;
        var image = Ui.Bitmap(_url, _decode, _reuse);
        if (image == null) return;
        // Straight in when it is decoded already, or when nothing decoded is shown to keep.
        if (_person != null || image.PixelWidth > 0 || !Showing)
        {
            Show(image);
            return;
        }
        var layer = new Border { Style = Ui.Styles.Get(_style), Background = new ImageBrush { Stretch = Stretch.UniformToFill, ImageSource = image } };
        _box.Children.Add(layer);
        RoutedEventHandler? opened = null;
        ExceptionRoutedEventHandler? failed = null;
        void Done(bool ok)
        {
            image.ImageOpened -= opened;
            image.ImageFailed -= failed;
            _box.Children.Remove(layer);
            // Not when the art shows another item by now.
            if (ok && _key == key) _brush.ImageSource = image;
        }
        opened = (_, _) => Done(true);
        failed = (_, _) => Done(false);
        image.ImageOpened += opened;
        image.ImageFailed += failed;
    }

    // A decoded picture is on screen.
    private bool Showing => (_person != null ? _person.ProfilePicture : _brush.ImageSource) is BitmapImage { PixelWidth: > 0 };

    private void Show(BitmapImage? image)
    {
        if (_person != null) _person.ProfilePicture = image;
        else _brush.ImageSource = image;
    }

    public void Resize(double size)
    {
        _box.Width = size;
        _box.Height = size;
    }

    public void Set(string url, string name = "")
    {
        if (_person != null && _person.DisplayName != name) _person.DisplayName = name;
        _url = url;
        // A new item decodes at the size the art has: no smaller picture first.
        _decode = Math.Max(_decode, _wanted);
        s_grown.Remove(this);
        // Two URLs that ask the CDN for the same picture at the same size load it once.
        string key = Ui.ArtKey(url, _decode);
        if (key == _key) return;
        _key = key;
        Show(url.Length == 0 ? null : Ui.Bitmap(url, _decode, _reuse));
    }
}