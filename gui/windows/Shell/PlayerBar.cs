// The player bar: now playing on the left, transport and seek in the centre, lyrics, queue,
// Connect, volume and More on the right. It only follows Player.

using System;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Input;

namespace OpenDeezer;

internal sealed class PlayerBar
{
    private static Player Player => App.Player;

    private readonly Art _art = new(ArtShape.Small, Size.BarArt);
    private readonly Button _artButton = new() { Style = Ui.Styles.Subtle, Padding = new Thickness(0) };
    private readonly TextBlock _title = Ui.Text("", TextKind.Strong);
    private readonly Border _explicit = Ui.ExplicitChip();
    private readonly TextBlock _artistText = Ui.Text("", TextKind.SecondaryCaption);
    private readonly HyperlinkButton _artist;
    private readonly TextBlock _formatText = Ui.ChipText("");
    private readonly Border _format;
    private readonly Button _like;
    private readonly FrameworkElement _nowPlaying, _idle;
    private double _leftMin, _rightMin;

    private readonly ToggleButton _shuffle;
    private readonly Button _prev, _next, _play;
    private readonly ToggleButton _repeat;
    private readonly FontIcon _playIcon = Ui.Icon(Icons.Play);
    private readonly FontIcon _repeatIcon = Ui.Icon(Icons.Repeat), _volumeIcon = Ui.Icon(Icons.Volume);
    private readonly ProgressRing _playRing = Ui.Square(new ProgressRing { Style = Ui.Styles.PlayRing, IsActive = false }, Size.PlayRing);
    private readonly TextBlock _pos = Ui.Time(0), _dur = Ui.Time(0);
    internal readonly Slider _seek;

    private readonly ToggleButton _lyrics, _queue;
    private readonly Button _connect, _volumeButton, _more;
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
            Style = Ui.Styles.Subtle,
            Padding = new Thickness(0),
            HorizontalAlignment = HorizontalAlignment.Left,
            Content = new MarqueeText { PlayOnTap = false, Children = { _title } },
        };
        Ui.Name(title, Loc.S("Menu_TrackDetails"));
        title.Click += (_, _) =>
        {
            if (Player.Current is { } t) TrackView.Open(t);
        };
        var lines = Ui.Stack(0, Ui.Grid("auto *", 0, _explicit, title), Ui.Grid("* auto", Space.S, _artist, _format));
        lines.VerticalAlignment = VerticalAlignment.Center;
        _nowPlaying = Ui.Grid("auto * auto", Space.M, _artButton, lines, _like);
        _nowPlaying.VerticalAlignment = VerticalAlignment.Center;

        _shuffle = Ui.IconToggle(Icons.Shuffle, "Tooltip_Shuffle", on => Player.SetShuffle(on));
        _prev = Ui.IconButton(Icons.Previous, "Tooltip_Previous", Player.Prev, IconSize.Large);
        _next = Ui.IconButton(Icons.Next, "Tooltip_Next", Player.Next, IconSize.Large);
        var playContent = new Grid();
        playContent.Children.Add(_playIcon);
        playContent.Children.Add(_playRing);
        _play = Ui.PlayButton(playContent);
        _play.Click += (_, _) => Player.TogglePlayPause();
        _repeat = Ui.IconToggle(Icons.Repeat, "Tooltip_RepeatOff", _ => CycleRepeat());
        _repeat.Content = _repeatIcon;
        var transport = Ui.Row(Space.S, _shuffle, _prev, _play, _next, _repeat);
        transport.HorizontalAlignment = HorizontalAlignment.Center;
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
        _pos.MinWidth = Size.TimeText;
        _pos.TextAlignment = TextAlignment.Right;
        _pos.VerticalAlignment = VerticalAlignment.Center;
        _dur.MinWidth = Size.TimeText;
        _dur.VerticalAlignment = VerticalAlignment.Center;
        var centre = Ui.Stack(0, transport, Ui.Grid("auto * auto", Space.S, _pos, _seek, _dur));
        centre.VerticalAlignment = VerticalAlignment.Center;

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
        var right = Ui.Row(0, _lyrics, _queue, _connect, _volumeButton, _volume, _more);
        right.HorizontalAlignment = HorizontalAlignment.Right;
        right.VerticalAlignment = VerticalAlignment.Center;

        // No fixed height: the transport row plus the seek slider decide it, so nothing is clipped.
        // Now playing sits on a card that holds its place when nothing plays, so the bar stays balanced.
        var idle = Ui.Row(Space.S, Ui.Icon(Icons.Flow), Ui.Text(Loc.S("Bar_NothingPlaying"), TextKind.SecondaryBody));
        idle.VerticalAlignment = VerticalAlignment.Center;
        _idle = idle;
        var slot = new Grid();
        slot.Children.Add(_nowPlaying);
        slot.Children.Add(idle);
        var card = new Border { Style = Ui.Styles.Get("NowPlayingCard"), Child = slot };
        Root = Ui.Grid($"* {Size.BarCentreWeight}* *", Space.M, card, centre, right);
        Root.ColumnDefinitions[1].MinWidth = Size.BarCentreMin;
        Root.ColumnDefinitions[1].MaxWidth = Size.BarCentreMax;
        Root.Style = Ui.Styles.Bar;
        Root.Padding = Pad.Bar;
        Ui.OnWidthClass(Root, ApplyWidth);
        // The bar's own width grows with the minimums Balance sets: the space it is given decides.
        Root.Loaded += (_, _) =>
        {
            if (Root.Parent is FrameworkElement host) host.SizeChanged += (_, e) => Balance(e.NewSize.Width);
        };

        Player.TrackChanged += RenderTrack;
        Player.StateChanged += RenderTransport;
        Player.VolumeChanged += RenderVolume;
        Player.CastChanged += RenderCast;
        Player.PositionChanged += RenderPosition;
        Player.QueueChanged += _ => RenderEnabled();
        Player.LikeChanged += id =>
        {
            if (id.Length == 0 || id == Player.Current?.Id) RenderLike();
        };
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
        if (_seek.Value != _pressedAt) Player.Seek((long)Math.Round(_seek.Value));
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

    private void RenderTrack()
    {
        string id = Player.Current?.Id ?? "";
        bool fade = id.Length > 0 && _shownId.Length > 0 && id != _shownId;
        _shownId = id;
        _nowPlaying.Visibility = Ui.Show(Player.Current != null);
        _idle.Visibility = Ui.Show(Player.Current == null);
        if (fade) Ui.CrossFade(_nowPlaying, RenderNowPlaying);
        else RenderNowPlaying();
        RenderEnabled();
        RenderFormat();
    }

    private void RenderNowPlaying()
    {
        if (Player.Current is { } t)
        {
            _art.Set(t.ArtworkUrl);
            Ui.SetText(_title, t.Name);
            _explicit.Visibility = Ui.Show(t.IsExplicit);
            Ui.SetText(_artistText, t.ArtistLine);
            _artist.IsEnabled = t.ArtistId.Length > 0;
        }
        RenderLike();
    }

    private void RenderLike()
    {
        var t = Player.Current;
        bool liked = t != null && Player.IsLiked(t.Id);
        _like.IsEnabled = t is { IsEpisode: false, Id.Length: > 0 };
        Ui.ShowLiked(_like, liked);
    }

    internal void RenderTransport()
    {
        bool playing = Player.State == PlayState.Playing;
        bool loading = Player.State == PlayState.Loading;
        _playIcon.Glyph = playing ? Icons.Pause : Icons.Play;
        _playIcon.Visibility = Ui.Show(!loading);
        _playRing.IsActive = loading;
        _playRing.Visibility = Ui.Show(loading);
        Ui.Name(_play, Loc.S(playing ? "Tooltip_Pause" : "Tooltip_Play"));
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

    private void RenderFormat()
    {
        string text = Player.IsPreview ? Loc.S("Preview_Badge") : Fmt.FormatChip(Player.Format);
        _formatText.Text = text;
        _format.Visibility = Ui.Show(text.Length > 0 && Player.Current != null && _width >= WidthClass.Medium);
    }

    // Below Narrow the art, lyrics and Connect make room (the last two move into More), so the
    // title and artist keep at least Size.BarText; a star column alone would squeeze them to nothing.
    private void ApplyWidth(WidthClass c)
    {
        _width = c;
        bool narrow = c == WidthClass.Narrow;
        _artButton.Visibility = Ui.Show(!narrow);
        _lyrics.Visibility = Ui.Show(!narrow);
        _connect.Visibility = Ui.Show(!narrow);
        // The card: its padding and border on both sides.
        _leftMin = (narrow ? 0 : Size.BarArt + Space.M) + Size.BarText + Space.M + Size.IconButton + 2 * (Space.S + Size.CardBorder);
        // Narrow: the centre needs only Previous, Play and Next; it takes whatever else is left.
        Root.ColumnDefinitions[1].MinWidth = narrow ? 2 * Size.IconButton + Size.PlayButton + 2 * Space.S : Size.BarCentreMin;
        _volume.Visibility = Ui.Show(c >= WidthClass.Medium);
        _shuffle.Visibility = Ui.Show(!narrow);
        _repeat.Visibility = Ui.Show(!narrow);
        // The right group's own minimum: the Grid hands the centre its star share first.
        int buttons = 0;
        foreach (var b in new Control[] { _lyrics, _queue, _connect, _volumeButton, _more })
            if (b.Visibility == Visibility.Visible) buttons++;
        _rightMin = buttons * Size.IconButton + (_volume.Visibility == Visibility.Visible ? Size.VolumeSlider : 0);
        Balance((Root.Parent as FrameworkElement)?.ActualWidth ?? 0);
        RenderVolumeName();
        RenderFormat();
    }

    // The two sides get the same width whenever the bar has room, so the transport row sits at
    // the window's centre; only a window too narrow for that lets each side keep its own minimum.
    private void Balance(double width)
    {
        double side = Math.Max(_leftMin, _rightMin);
        double fixedPart = Root.ColumnDefinitions[1].MinWidth + 2 * Root.ColumnSpacing + Root.Padding.Left + Root.Padding.Right;
        bool even = width >= 2 * side + fixedPart;
        Root.ColumnDefinitions[0].MinWidth = even ? side : _leftMin;
        Root.ColumnDefinitions[2].MinWidth = even ? side : _rightMin;
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
            var shuffle = new ToggleMenuFlyoutItem { Text = Loc.S("Tooltip_Shuffle"), Icon = Ui.Icon(Icons.Shuffle), IsChecked = Player.Shuffle };
            shuffle.Click += (_, _) => Player.SetShuffle(shuffle.IsChecked);
            Menus.Group(menu.Items, shuffle,
                Menus.Item(RepeatKey, RepeatGlyph, CycleRepeat),
                Menus.Item("Tooltip_Lyrics", Icons.Lyrics, () => LyricsToggled?.Invoke(true)),
                Menus.Item("Tooltip_Connect", Icons.Cast, () => _connectFlyout.ShowAt(_more)));
        }
        if (Player.Current is { IsEpisode: false, Id.Length: > 0 } t) Menus.Track(menu.Items, t, new TrackMenu(Bar: true));
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

    private StackPanel BuildSleep(DispatcherQueue dq)
    {
        var tick = dq.CreateTimer();
        tick.Interval = TimeSpan.FromMilliseconds(Timing.SleepRefreshMs);
        tick.Tick += (_, _) => RefreshSleep();
        var panel = Ui.Stack(0, _sleepStatus);
        void Choice(string text, Func<System.Threading.Tasks.Task> apply) =>
            panel.Children.Add(Ui.FlyoutRow(Icons.Sleep, text, null, false, async () =>
            {
                await apply();
                RefreshSleep();
            }));
        foreach (int m in Timing.SleepMinutes)
            Choice(Loc.Plural("Minutes", m), () => Player.SetSleepTimerAsync(m, endOfTrack: false));
        Choice(Loc.S("Settings_EndOfTrack"), () => Player.SetSleepTimerAsync(0, endOfTrack: true));
        Choice(Loc.S("Common_Off"), Player.CancelSleepTimerAsync);
        panel.Loaded += (_, _) =>
        {
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
