// The player bar: now playing on the left, transport and seek in the center, an episode's speed,
// lyrics, queue, Connect, volume and More on the right. It only follows Player.

using System;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Input;

namespace PurpleDisc;

internal sealed class PlayerBar
{
    private static Player Player => App.Player;

    private readonly Art _art = new(ArtShape.Small, Size.BarArt);
    private readonly Button _artButton = new() { Style = Ui.Styles.SubtleFlush };
    private readonly TextBlock _title = Ui.Text("", TextKind.Strong);
    private readonly Border _explicit = Ui.ExplicitChip();
    private readonly TextBlock _artistText = Ui.Text("", TextKind.SecondaryCaption);
    private readonly HyperlinkButton _artist;
    private readonly TextBlock _formatText = Ui.ChipText("");
    internal readonly Border _format;
    private readonly Button _like;
    private readonly FrameworkElement _nowPlaying, _idle;
    internal readonly FrameworkElement _elsewhere;
    private readonly Art _elsewhereArt = new(ArtShape.Small, Size.BarArt);
    private readonly TextBlock _elsewhereTitle = Ui.Text("", TextKind.Strong);
    internal readonly TextBlock _elsewhereArtist = Ui.Text("", TextKind.SecondaryCaption);
    internal readonly FrameworkElement _elsewhereTitleLine;
    internal readonly Button _playHere;
    private readonly DispatcherQueueTimer _elsewhereEnds;
    private double _leftMin, _rightMin, _centerWant;
    // The transport buttons, and the center column: the buttons with the times at its ends, over the slider.
    internal readonly StackPanel _transport;
    private readonly StackPanel _center;

    private readonly ToggleButton _shuffle;
    private readonly Button _prev, _next, _play;
    private readonly ToggleButton _repeat;
    private readonly FontIcon _playIcon = Ui.Icon(Icons.Play);
    private readonly FontIcon _repeatIcon = Ui.Icon(Icons.Repeat), _volumeIcon = Ui.Icon(Icons.Volume);
    private readonly ProgressRing _playRing = Ui.Square(new ProgressRing { Style = Ui.Styles.PlayRing, IsActive = false }, Size.PlayRing);
    internal readonly TextBlock _pos = Ui.Time(0), _dur = Ui.Time(0);
    internal readonly Slider _seek;

    private readonly ToggleButton _lyrics, _queue;
    private readonly Button _connect, _volumeButton, _more, _speed;
    private readonly TextBlock _speedText = Ui.Text("");
    private readonly Slider _volume, _flyoutVolume;
    private readonly Flyout _volumeFlyout, _connectFlyout, _sleepFlyout;
    private readonly StackPanel _devices = Ui.Stack(0);
    private readonly TextBlock _connectStatus = Ui.Text("", TextKind.SecondaryCaption, wrap: true);
    private readonly TextBlock _sleepStatus = Ui.Text("", TextKind.SecondaryCaption, wrap: true);
    private WidthClass _width = WidthClass.Wide;
    private double _unmuted = 1;
    private string _shownId = "";
    private bool _seeking;                 // the seek thumb is held: the clock leaves it alone
    private double _pressedAt;
    private long _shownDur = -1, _shownSec = -1;
    private double _seekTarget;
    private readonly Action _applySeek;

    public Grid Root { get; }

    public event Action<bool>? LyricsToggled;
    public event Action<bool>? QueueToggled;

    public PlayerBar(DispatcherQueue dq)
    {
        _artButton.Content = _art.Root;
        _artButton.Click += (_, _) => LyricsToggled?.Invoke(true);
        Ui.Name(_artButton, Loc.S("Tooltip_Lyrics"));
        // Now playing scrolls once per track when it doesn't fit, and again on hover.
        _artist = new HyperlinkButton
        {
            Style = Ui.Styles.Link,
            Content = new MarqueeText { PlayOnTap = false, Children = { _artistText } },
            HorizontalAlignment = HorizontalAlignment.Left,
        };
        _artist.Click += (_, _) =>
        {
            if (Player.Current is { ArtistId.Length: > 0 } t) ArtistView.Open(t.ArtistId);
        };
        _like = Ui.LikeButton(Size.IconButton, () =>
        {
            if (Player.Current is { } t) Player.Like(t, !Player.IsLiked(t.Id));
        });
        _format = Ui.Chip(_formatText);
        // The title opens the track's overview page.
        var title = new Button
        {
            Style = Ui.Styles.SubtleFlush,
            HorizontalAlignment = HorizontalAlignment.Left,
            Content = new MarqueeText { PlayOnTap = false, Children = { _title } },
        };
        Ui.Name(title, Loc.S("Menu_TrackDetails"));
        title.Click += (_, _) =>
        {
            if (Player.Current is { } t) TrackView.Open(t);
        };
        _title.FontSize = _elsewhereTitle.FontSize = Size.BarTitleFont;
        _artistText.FontSize = _elsewhereArtist.FontSize = Size.BarArtistFont;
        var lines = Ui.Stack(0, Ui.Grid("auto *", 0, _explicit, title), _artist);
        lines.VerticalAlignment = VerticalAlignment.Center;
        // Like over the codec chip, centered beside the text.
        _format.HorizontalAlignment = HorizontalAlignment.Center;
        var side = Ui.Stack(0, _like, _format);
        side.VerticalAlignment = VerticalAlignment.Center;
        _nowPlaying = Ui.Grid("auto * auto", Space.S, _artButton, lines, side);
        _nowPlaying.VerticalAlignment = VerticalAlignment.Center;

        _shuffle = Ui.IconToggle(Icons.Shuffle, "Tooltip_Shuffle", on => Player.SetShuffle(on), turn: true);
        _prev = Ui.IconButton(Icons.Previous, "Tooltip_Previous", Player.Prev, IconSize.Large, nudge: -Moves.Nudge);
        _next = Ui.IconButton(Icons.Next, "Tooltip_Next", Player.Next, IconSize.Large, nudge: Moves.Nudge);
        var playContent = new Grid();
        playContent.Children.Add(_playIcon);
        playContent.Children.Add(_playRing);
        _play = Ui.PlayButton(playContent);
        _play.Click += (_, _) => Player.TogglePlayPause();
        _repeat = Ui.IconToggle(Icons.Repeat, "Tooltip_RepeatOff", _ => CycleRepeat(), turn: true);
        _repeat.Content = _repeatIcon;
        _transport = Ui.Row(Space.XS, _shuffle, _prev, _play, _next, _repeat);
        _transport.HorizontalAlignment = HorizontalAlignment.Center;
        _seek = Ui.Slider(0, 1, 0, "Tooltip_Seek", v =>
        {
            Ui.SetText(_pos, Fmt.Time((long)v));
            _shownSec = -1;
            // A drag seeks once, on release (every engine seek fades and flushes); keys seek at once.
            if (!_seeking) Player.Seek((long)Math.Round(v));
        });
        _applySeek = () => _seek.Value = _seekTarget;
        _seek.SmallChange = Steps.Seek;
        _seek.LargeChange = Steps.SeekPage;
        _seek.StepFrequency = Steps.SeekGrain;
        _seek.ThumbToolTipValueConverter = new TimeTextConverter();
        // The Slider moves to a clicked spot before this handler runs (and that move seeks), so
        // release seeks only when a drag moved the thumb after the press.
        _seek.AddHandler(UIElement.PointerPressedEvent, new PointerEventHandler((_, _) =>
        {
            _seeking = true;
            _pressedAt = _seek.Value;
        }), true);
        _seek.AddHandler(UIElement.PointerReleasedEvent, new PointerEventHandler((_, _) => EndSeek()), true);
        _seek.AddHandler(UIElement.PointerCaptureLostEvent, new PointerEventHandler((_, _) => EndSeek()), true);
        // The times sit above the slider's ends, beside the buttons: showing them never takes the
        // slider's width. They always show; the center column keeps room for them (ApplyWidth).
        _pos.VerticalAlignment = VerticalAlignment.Center;
        _dur.VerticalAlignment = VerticalAlignment.Center;
        _dur.HorizontalAlignment = HorizontalAlignment.Right;
        _center = Ui.Stack(0, Ui.Grid("* auto *", Space.XS, _pos, _transport, _dur), _seek);
        _center.VerticalAlignment = VerticalAlignment.Center;

        _lyrics = Ui.IconToggle(Icons.Lyrics, "Tooltip_Lyrics", on => LyricsToggled?.Invoke(on));
        _queue = Ui.IconToggle(Icons.Queue, "Tooltip_Queue", on => QueueToggled?.Invoke(on));
        _connectFlyout = Ui.Flyout("Connect_Title", Ui.Stack(Space.S, _connectStatus, _devices));
        _connectFlyout.Opened += (_, _) => RefreshDevices();
        _connect = Ui.IconButton(Icons.Cast, "Tooltip_Connect", null);
        _connect.Flyout = _connectFlyout;
        _volume = Ui.Slider(0, 100, Player.Volume * 100, "Tooltip_Volume", v => Player.SetVolume(v / 100));
        _volume.Width = Size.VolumeSlider;
        _volume.SmallChange = Steps.Volume;
        _volume.LargeChange = Steps.VolumePage;
        _flyoutVolume = Ui.Slider(0, 100, Player.Volume * 100, "Tooltip_Volume", v => Player.SetVolume(v / 100));
        _volumeFlyout = Ui.Flyout("Tooltip_Volume", _flyoutVolume);
        _volumeButton = Ui.IconButton(Icons.Volume, "Tooltip_Mute", OnVolumeButton);
        _volumeButton.Content = _volumeIcon;
        _sleepFlyout = Ui.Flyout("Settings_SleepTimer", BuildSleep(dq));
        _more = Ui.IconButton(Icons.More, "Tooltip_More", null);
        var moreMenu = new MenuFlyout();
        moreMenu.Opening += (_, _) => FillMore(moreMenu);
        _more.Flyout = moreMenu;
        // Episodes only, as in Deezer's app: each click moves on through its speeds.
        _speed = Ui.IconButton(Icons.Clock, "Tooltip_Speed", () =>
        {
            Episodes.NextSpeed();
            RenderSpeed();
        });
        _speedText.VerticalAlignment = VerticalAlignment.Center;
        _speed.Content = _speedText;
        _speed.Width = Size.SpeedButton;
        var right = Ui.Row(0, _speed, _lyrics, _queue, _connect, _volumeButton, _volume, _more);
        right.HorizontalAlignment = HorizontalAlignment.Right;
        right.VerticalAlignment = VerticalAlignment.Center;

        // No fixed height: the transport row plus the seek slider decide it, so nothing is clipped.
        // Now playing sits on a card that holds its place when nothing plays, so the bar stays balanced.
        var idle = Ui.Row(Space.S, Ui.Icon(Icons.Flow), Ui.Text(Loc.S("Bar_NothingPlaying"), TextKind.SecondaryBody));
        idle.VerticalAlignment = VerticalAlignment.Center;
        _idle = idle;
        // While nothing plays here, the track another player of the account plays, marked by the
        // speakers glyph, to play here; a track it did not name shows as the glyph and its label.
        var playingThere = Ui.Icon(Icons.Speakers, IconSize.Small, accent: true);
        playingThere.VerticalAlignment = VerticalAlignment.Center;
        _elsewhereTitleLine = new MarqueeText { PlayOnTap = false, Children = { _elsewhereTitle } };
        var elsewhereLines = Ui.Stack(0, _elsewhereTitleLine,
            Ui.Grid("auto *", Space.XS, playingThere, new MarqueeText { PlayOnTap = false, Children = { _elsewhereArtist } }));
        elsewhereLines.VerticalAlignment = VerticalAlignment.Center;
        Ui.Name(elsewhereLines, Loc.S("Tooltip_PlayingElsewhere"));
        _playHere = Ui.IconButton(Icons.Play, "Tooltip_PlayHere", () =>
        {
            if (Player.Elsewhere.Track is { } t) Player.PlayHere(t);
        });
        _elsewhere = Ui.Grid("auto * auto", Space.M, _elsewhereArt.Root, elsewhereLines, _playHere);
        _elsewhere.VerticalAlignment = VerticalAlignment.Center;
        // Deezer announces no pause: the track counts as playing there until its end.
        _elsewhereEnds = dq.CreateTimer();
        _elsewhereEnds.IsRepeating = false;
        _elsewhereEnds.Tick += (_, _) => RenderSlot();
        var slot = new Grid();
        slot.Children.Add(_nowPlaying);
        slot.Children.Add(idle);
        slot.Children.Add(_elsewhere);
        var card = new Border { Style = Ui.Styles.Get("NowPlayingCard"), Child = slot };
        Root = Ui.Grid($"* {Size.BarCenterWeight}* *", Space.S, card, _center, right);
        Root.ColumnDefinitions[1].MinWidth = Size.BarCenterMin;
        Root.ColumnDefinitions[1].MaxWidth = Size.BarCenterMax;
        Root.Style = Ui.Styles.Bar;
        Root.Padding = Pad.Bar;
        Ui.OnWidthClass(Root, ApplyWidth);
        // The bar's own width grows with the minimums Balance sets: the space it is given decides.
        RoutedEventHandler? loaded = null;
        loaded = (_, _) =>
        {
            Root.Loaded -= loaded;
            if (Root.Parent is FrameworkElement host) host.SizeChanged += (_, e) => Balance(e.NewSize.Width);
        };
        Root.Loaded += loaded;

        Player.TrackChanged += RenderTrack;
        Player.StateChanged += RenderTransport;
        Player.VolumeChanged += RenderVolume;
        Player.CastChanged += RenderCast;
        Player.ElsewhereChanged += RenderSlot;
        Player.PositionChanged += RenderPosition;
        Player.QueueChanged += _ => RenderEnabled();
        Player.LikeChanged += id =>
        {
            if (id.Length == 0 || id == Player.Current?.Id) RenderLike();
        };
        // The heart and the artist link follow offline mode.
        App.Offline.Changed += RenderNowPlaying;
        RenderTrack();
        RenderTransport();
        RenderVolume();
        RenderCast();
    }

    public void SetLyricsShown(bool shown) => Ui.Quietly(_lyrics, () => _lyrics.IsChecked = shown);

    public void SetQueueOpen(bool open) => Ui.Quietly(_queue, () => _queue.IsChecked = open);

    private void EndSeek()
    {
        if (!_seeking) return;
        _seeking = false;
        // A slider disabled meanwhile (a new track loading) belongs to another stream: no seek.
        if (_seek.IsEnabled && _seek.Value != _pressedAt) Player.Seek((long)Math.Round(_seek.Value));
    }

    // Every position tick: the thumb and the elapsed time; the length only when it changes.
    // An unknown length (0) leaves the thumb at the start and the slider disabled.
    public void RenderPosition()
    {
        if (_seeking) return;
        long pos = Player.PositionMs, dur = Player.DurationMs;
        if (dur != _shownDur)
        {
            _shownDur = dur;
            _seekTarget = 0;
            Ui.Quietly(_seek, _applySeek);
            _seek.Maximum = Math.Max(dur, 1);
            Ui.SetText(_dur, dur > 0 ? Fmt.Time(dur) : "");
            RenderEnabled();
        }
        _seekTarget = dur > 0 ? Math.Min(pos, dur) : 0;
        Ui.Quietly(_seek, _applySeek);
        // The text shows whole seconds: a new string only when the second changes.
        long sec = Math.Max(0, pos) / 1000;
        if (sec == _shownSec) return;
        _shownSec = sec;
        Ui.SetText(_pos, Fmt.Time(pos));
    }

    internal void RenderTrack()
    {
        string id = Player.Current?.Id ?? "";
        bool fade = id.Length > 0 && _shownId.Length > 0 && id != _shownId;
        _shownId = id;
        RenderSlot();
        if (fade) Ui.CrossFade(_nowPlaying, RenderNowPlaying);
        else RenderNowPlaying();
        RenderEnabled();
        RenderFormat();
        RenderSpeed();
    }

    // In the bar from the medium width up; below, in More (the bar could not narrow past it).
    private bool SpeedShown(WidthClass c) => Player.Current is { IsEpisode: true } && c >= WidthClass.Medium;

    internal void RenderSpeed()
    {
        Ui.SetText(_speedText, Episodes.SpeedText(Episodes.Speed));
        Ui.Name(_speed, Loc.Format("Tooltip_SpeedFormat", Episodes.SpeedText(Episodes.Speed)));
        if (Ui.Show(SpeedShown(_width)) != _speed.Visibility) ApplyWidth(_width);
    }

    private void RenderNowPlaying()
    {
        if (Player.Current is { } t)
        {
            _art.Set(t.ArtworkUrl);
            Ui.SetText(_title, t.Name);
            _explicit.Visibility = Ui.Show(t.IsExplicit);
            Ui.SetText(_artistText, t.ArtistLine);
            _artist.IsEnabled = t.ArtistId.Length > 0 && !App.Offline.Mode;
        }
        RenderLike();
    }

    // The card shows now playing, else another session's track while it plays there and nothing
    // plays here, else the idle line.
    private void RenderSlot()
    {
        long now = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
        var away = Player.Elsewhere;
        bool here = Player.State is PlayState.Playing or PlayState.Loading || Player.IsCasting;
        bool showAway = !here && away.PlayingAt(now);
        _elsewhere.Visibility = Ui.Show(showAway);
        _nowPlaying.Visibility = Ui.Show(!showAway && Player.Current != null);
        _idle.Visibility = Ui.Show(!showAway && Player.Current == null);
        _elsewhereEnds.Stop();
        if (!showAway) return;
        var t = away.Track;
        _elsewhereArt.Set(t?.ArtworkUrl ?? "");
        _elsewhereTitleLine.Visibility = Ui.Show(t != null);
        _playHere.Visibility = Ui.Show(t != null);
        Ui.SetText(_elsewhereTitle, t?.Name ?? "");
        Ui.SetText(_elsewhereArtist, t?.ArtistLine ?? Loc.S("Tooltip_PlayingElsewhere"));
        _elsewhereEnds.Interval = TimeSpan.FromMilliseconds(away.UntilMs - now);
        _elsewhereEnds.Start();
    }

    private void RenderLike()
    {
        var t = Player.Current;
        bool liked = t != null && Player.IsLiked(t.Id);
        _like.IsEnabled = t is { IsEpisode: false, Id.Length: > 0 } && !App.Offline.Mode;
        Ui.ShowLiked(_like, liked);
    }

    internal void RenderTransport()
    {
        bool playing = Player.State == PlayState.Playing;
        bool loading = Player.State == PlayState.Loading;
        string glyph = playing ? Icons.Pause : Icons.Play;
        // Only a shown glyph that stays shown swaps with motion; one hidden by or for Loading does not.
        bool swap = _playIcon.Glyph != glyph && _playIcon.Visibility == Visibility.Visible && !loading;
        _playIcon.Glyph = glyph;
        _playIcon.Visibility = Ui.Show(!loading);
        if (swap) Motion.Swap(_playIcon, (float)_playIcon.FontSize);
        _playRing.IsActive = loading;
        _playRing.Visibility = Ui.Show(loading);
        Ui.Name(_play, Loc.S(playing ? "Tooltip_Pause" : "Tooltip_Play"));
        RenderSlot();
        Ui.Quietly(_shuffle, () => _shuffle.IsChecked = Player.Shuffle);
        Ui.Name(_shuffle, Loc.S(Player.Shuffle ? "Tooltip_ShuffleOn" : "Tooltip_ShuffleOff"));
        Ui.Quietly(_repeat, () => _repeat.IsChecked = Player.Repeat != RepeatMode.Off);
        _repeatIcon.Glyph = RepeatGlyph;
        Ui.Name(_repeat, Loc.S(RepeatKey));
        RenderEnabled();
        RenderFormat();
    }

    private void RenderVolume()
    {
        double v = Player.Volume * 100;
        foreach (var s in new[] { _volume, _flyoutVolume })
            if (Math.Abs(s.Value - v) > Steps.VolumeSame * 100) Ui.Quietly(s, () => s.Value = v);
        _volumeIcon.Glyph = Player.Volume <= 0 ? Icons.Mute : Icons.Volume;
        RenderVolumeName();
    }

    private void RenderCast()
    {
        _connect.Style = Player.IsCasting ? Ui.Styles.IconButtonActive : Ui.Styles.IconButton;
        Ui.Name(_connect, Player.IsCasting ? Loc.Format("Connect_PlayingOnFormat", Player.CastName) : Loc.S("Tooltip_Connect"));
    }

    private void RenderEnabled()
    {
        bool any = Player.Current != null || Player.Queue.Count > 0;
        foreach (var c in new Control[] { _shuffle, _prev, _play, _next, _repeat })
            c.IsEnabled = any;
        // While loading the engine still plays the old stream: a seek would move that one.
        _seek.IsEnabled = any && Player.DurationMs > 0 && Player.State != PlayState.Loading;
    }

    internal void RenderFormat()
    {
        string text = Player.IsPreview ? Loc.S("Preview_Badge") : Fmt.FormatChip(Player.Format);
        _formatText.Text = text;
        _format.Visibility = Ui.Show(text.Length > 0 && Player.Current != null);
    }

    // Below Narrow the art, lyrics and Connect make room (the last two move into More), so the
    // title and artist keep at least Size.BarText; a star column alone would squeeze them to nothing.
    private void ApplyWidth(WidthClass c)
    {
        _width = c;
        bool narrow = c == WidthClass.Narrow;
        _artButton.Visibility = Ui.Show(!narrow);
        _elsewhereArt.Root.Visibility = Ui.Show(!narrow);
        _lyrics.Visibility = Ui.Show(!narrow);
        _connect.Visibility = Ui.Show(!narrow);
        _leftMin = LeftMin(narrow);
        // Narrow: the center needs only Previous, Play and Next, and wants room for the slider first,
        // up to what it has at the next width class (Small), so widening across it never narrows it.
        double gaps = 2 * Root.ColumnSpacing + Root.Padding.Left + Root.Padding.Right;
        double smallCenter = Breakpoint.Narrow - gaps - LeftMin(narrow: false) - Count.SmallBarButtons * Size.IconButton;
        // The transport row and the two times beside it, which always show.
        double transport = narrow ? 2 * Size.IconButton + Size.PlayButton + 2 * Space.XS : 4 * Size.IconButton + Size.PlayButton + 4 * Space.XS;
        double centerMin = Math.Max(narrow ? 0 : Size.BarCenterMin, transport + 2 * (Size.TimeText + Space.XS));
        Root.ColumnDefinitions[1].MinWidth = centerMin;
        Root.ColumnDefinitions[1].MaxWidth = narrow ? smallCenter : Size.BarCenterMax;
        _centerWant = Math.Max(centerMin, narrow ? Size.BarCenterNarrow : smallCenter);
        _volume.Visibility = Ui.Show(c >= WidthClass.Medium);
        _speed.Visibility = Ui.Show(SpeedShown(c));
        _shuffle.Visibility = Ui.Show(!narrow);
        _repeat.Visibility = Ui.Show(!narrow);
        // The right group's own minimum: the Grid hands the center its star share first.
        int buttons = 0;
        foreach (var b in new Control[] { _lyrics, _queue, _connect, _volumeButton, _more })
            if (b.Visibility == Visibility.Visible) buttons++;
        _rightMin = buttons * Size.IconButton + (_volume.Visibility == Visibility.Visible ? Size.VolumeSlider : 0)
            + (_speed.Visibility == Visibility.Visible ? Size.SpeedButton : 0);
        Balance((Root.Parent as FrameworkElement)?.ActualWidth ?? 0);
        RenderVolumeName();
        RenderFormat();
    }

    // Now playing's minimum: the art (on a narrow bar only the gap its empty column keeps), the
    // title's room, Like, and the card's padding and border on both sides.
    private static double LeftMin(bool narrow) =>
        (narrow ? 0 : Size.BarArt) + Space.S + (narrow ? Size.BarTextNarrow : Size.BarText) + Space.S + Size.BarSide
        + 2 * (Space.S + Size.CardBorder);

    // The center first gets the width it wants (_centerWant); what is left past that evens out the
    // sides, the smaller growing to the larger, so the transport row moves to the window's center;
    // beyond that the star shares share the rest. So the center never narrows as the window widens.
    private void Balance(double width)
    {
        double room = width - 2 * Root.ColumnSpacing - Root.Padding.Left - Root.Padding.Right - _centerWant;
        double big = Math.Max(_leftMin, _rightMin), small = Math.Min(_leftMin, _rightMin);
        double grown = Math.Clamp(room - big, small, big);
        bool leftBig = _leftMin >= _rightMin;
        Root.ColumnDefinitions[0].MinWidth = leftBig ? big : grown;
        Root.ColumnDefinitions[2].MinWidth = leftBig ? grown : big;
    }

    // Wide: the button mutes. Narrow: it opens the volume flyout.
    private void RenderVolumeName() =>
        Ui.Name(_volumeButton, Loc.S(_width < WidthClass.Medium ? "Tooltip_Volume" : Player.Volume <= 0 ? "Tooltip_Unmute" : "Tooltip_Mute"));

    private static string RepeatKey => Player.Repeat switch { RepeatMode.All => "Tooltip_RepeatAll", RepeatMode.One => "Tooltip_RepeatOne", _ => "Tooltip_RepeatOff" };

    private static string RepeatGlyph => Player.Repeat == RepeatMode.One ? Icons.RepeatOne : Icons.Repeat;

    private static void CycleRepeat() => Player.SetRepeat((RepeatMode)(((int)Player.Repeat + 1) % 3));

    private void OnVolumeButton()
    {
        if (_width < WidthClass.Medium)
        {
            _volumeFlyout.ShowAt(_volumeButton);
            return;
        }
        if (Player.Volume > 0)
        {
            _unmuted = Player.Volume;
            Player.SetVolume(0);
        }
        else Player.SetVolume(_unmuted > 0 ? _unmuted : 1);
    }

    private void FillMore(MenuFlyout menu)
    {
        menu.Items.Clear();
        if (_width == WidthClass.Narrow)
        {
            Menus.Group(menu.Items, Menus.CheckItem("Tooltip_Shuffle", Icons.Shuffle, Player.Shuffle, Player.SetShuffle),
                Menus.Item(RepeatKey, RepeatGlyph, CycleRepeat),
                Menus.Item("Tooltip_Lyrics", Icons.Lyrics, () => LyricsToggled?.Invoke(true)),
                Menus.Item("Tooltip_Connect", Icons.Cast, () => _connectFlyout.ShowAt(_more)));
        }
        if (Player.Current is { IsEpisode: true } && !SpeedShown(_width))
            Menus.Group(menu.Items, Menus.Item("Tooltip_Speed", Icons.Clock, () =>
            {
                Episodes.NextSpeed();
                RenderSpeed();
                Toast.Info(Loc.Format("Tooltip_SpeedFormat", Episodes.SpeedText(Episodes.Speed)), "");
            }));
        if (Player.Current is { Id.Length: > 0 } t) Menus.Track(menu.Items, t, new TrackMenu(Bar: true));
        Menus.Group(menu.Items, Menus.Item("Settings_SleepTimer", Icons.Sleep, () => _sleepFlyout.ShowAt(_more)));
    }

    // The last list shows at once; a fresh discovery replaces it.
    private async void RefreshDevices()
    {
        RenderDevices();
        _connectStatus.Text = Loc.S("Connect_Searching");
        _connectStatus.Visibility = Visibility.Visible;
        var devices = await Player.DiscoverAsync();
        if (!_connectFlyout.IsOpen) return;
        RenderDevices();
        _connectStatus.Text = Loc.S("Connect_NoDevices");
        _connectStatus.Visibility = Ui.Show(devices.Count == 0);
    }

    private void RenderDevices()
    {
        _devices.Children.Clear();
        _devices.Children.Add(Ui.FlyoutRow(Icons.Computer, Loc.S("Connect_ThisComputer"), Loc.S("Connect_LocalPlayback"), !Player.IsCasting, () =>
        {
            _connectFlyout.Hide();
            Player.Disconnect();
        }));
        foreach (var d in Player.Devices)
        {
            string sub = d.Version.Length > 0 ? Loc.Format("Connect_VersionFormat", Fmt.ConnectType(d.Client), d.Version) : Fmt.ConnectType(d.Client);
            _devices.Children.Add(Ui.FlyoutRow(Icons.Device(d.Client), d.Name.Length > 0 ? d.Name : d.Addr, sub, d.Addr == Player.CastAddress, () => Connect(d)));
        }
    }

    // DZConnectDevice can block for about 15 s.
    private async void Connect(ConnectDevice d)
    {
        _connectFlyout.Hide();
        Toast.Info(Loc.S("Connect_Title"), Loc.Format("Connect_ConnectingFormat", d.Name.Length > 0 ? d.Name : d.Addr), sticky: true);
        bool ok = await Player.ConnectAsync(d);
        Toast.Hide();
        if (!ok) Toast.Error(Loc.S("Dialog_ConnectFailTitle"), Loc.S("Dialog_ConnectFailBody"));
    }

    // Deezer's presets, then the end of the track (of the episode, for one), then off; each confirms
    // with Deezer's toast.
    private StackPanel BuildSleep(DispatcherQueue dq)
    {
        var tick = dq.CreateTimer();
        tick.Interval = TimeSpan.FromMilliseconds(Timing.SleepRefreshMs);
        tick.Tick += (_, _) => RefreshSleep();
        var choices = Ui.Stack(0);
        var panel = Ui.Stack(0, _sleepStatus, choices);
        void Choice(string text, bool on, Func<System.Threading.Tasks.Task> apply) =>
            choices.Children.Add(Ui.FlyoutRow(Icons.Sleep, text, null, false, async () =>
            {
                _sleepFlyout.Hide();
                await apply();
                Toast.Info(Loc.S(on ? "Sleep_Set" : "Sleep_Off"), "");
            }));
        panel.Loaded += (_, _) =>
        {
            choices.Children.Clear();
            foreach (int m in Timing.SleepMinutes)
                Choice(Loc.Plural("Minutes", m), true, () => Player.SetSleepTimerAsync(m, endOfTrack: false));
            Choice(Loc.S(Player.Current is { IsEpisode: true } ? "Sleep_EndOfEpisode" : "Settings_EndOfTrack"), true,
                () => Player.SetSleepTimerAsync(0, endOfTrack: true));
            Choice(Loc.S("Sleep_TurnOff"), false, Player.CancelSleepTimerAsync);
            RefreshSleep();
            tick.Start();
        };
        panel.Unloaded += (_, _) => tick.Stop();
        return panel;
    }

    private async void RefreshSleep()
    {
        var s = await Player.SleepStatusAsync();
        _sleepStatus.Text = !s.Active ? Loc.S("Sleep_Inactive")
            : s.EndOfTrack ? Loc.S("Sleep_EndOfTrackActive")
            : Loc.Format("Sleep_RemainingFormat", Fmt.Time(s.RemainingMs));
    }
}
