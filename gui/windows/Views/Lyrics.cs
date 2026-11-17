// Lyrics of the playing track. Synced lines follow playback and seek on click; scrolling by
// hand pauses the follow for a few seconds.

using System;
using System.Collections.Generic;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Automation.Peers;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;

namespace OpenDeezer;

internal sealed class LyricsView : View
{
    private readonly Art _art = new(ArtShape.Small, Size.RowArt);
    private readonly TextBlock _title = Ui.Text("", TextKind.Strong);
    private readonly TextBlock _artist = Ui.Text("", TextKind.SecondaryCaption);
    private readonly StackPanel _lines = Ui.Stack(Space.M);
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;
    private readonly List<TextBlock> _blocks = new();
    private Lyrics _lyrics = new();
    private string _trackId = "";
    private int _active = -1;
    private long _userScrollTick;

    public override FrameworkElement Root => _state.Root;
    public override ScrollViewer? Scroller => _scroller;

    public LyricsView()
    {
        var header = Ui.Grid("auto *", Space.M, _art.Root, Ui.Stack(0, _title, _artist));
        header.Margin = Pad.Header;
        _scroller = Ui.Page(Size.ReadingMax, header, _lines);
        void User() => _userScrollTick = Environment.TickCount64;
        foreach (var e in new[] { UIElement.PointerWheelChangedEvent, UIElement.PointerPressedEvent })
            _scroller.AddHandler(e, new PointerEventHandler((_, _) => User()), true);
        _scroller.AddHandler(UIElement.KeyDownEvent, new KeyEventHandler((_, _) => User()), true);
        _scroller.DirectManipulationStarted += (_, _) => User();
        _state = new StateView(_scroller, SkeletonShape.Rows);
    }

    public override void Show(object? arg, bool isBack)
    {
        Player.TrackChanged += OnTrack;
        Player.PositionChanged += OnPosition;
        OnTrack();
    }

    public override void Hide()
    {
        Player.TrackChanged -= OnTrack;
        Player.PositionChanged -= OnPosition;
    }

    private async void OnTrack()
    {
        var t = Player.Current;
        _title.Text = t?.Name ?? "";
        _artist.Text = t?.ArtistLine ?? "";
        _art.Set(t?.ArtworkUrl ?? "");
        string id = t?.Id ?? "";
        if (id.Length > 0 && id == _trackId) return;
        _trackId = id;
        // Episode ids are another namespace, and episodes have no lyrics.
        if (t == null || id.Length == 0 || (t.IsEpisode && !Player.IsCasting))
        {
            _state.ShowEmpty(Icons.Lyrics, Loc.S(id.Length == 0 ? "Lyrics_PlayPrompt" : "Lyrics_None"));
            return;
        }
        if (!App.Session.CanShowLyrics)
        {
            _state.ShowEmpty(Icons.Lyrics, Loc.S("Lyrics_PaidOnly"));
            return;
        }
        if (await LoadAsync<Lyrics>(Library.Key("lyrics", t.Id), _state) is not { } ly || t.Id != _trackId) return;
        Render(ly);
    }

    private void Render(Lyrics lyrics)
    {
        _lyrics = lyrics;
        _lines.Children.Clear();
        _blocks.Clear();
        _active = -1;
        _userScrollTick = 0;
        if (_lyrics.IsSynced && _lyrics.Synced.Count > 0)
        {
            foreach (var l in _lyrics.Synced)
            {
                var tb = Ui.Text(l.Text.Length == 0 ? Icons.NoteText : l.Text, TextKind.Body, wrap: true);
                tb.Style = Ui.Styles.LyricsLineInactive;
                long at = l.TimeMs;
                tb.Tapped += (_, _) => Seek(at);
                _lines.Children.Add(tb);
                _blocks.Add(tb);
            }
            OnPosition();
        }
        else if (_lyrics.Plain.Length > 0)
        {
            var tb = Ui.Text(_lyrics.Plain, TextKind.Body, wrap: true);
            tb.Style = Ui.Styles.LyricsPlain;
            _lines.Children.Add(tb);
        }
        else
        {
            _state.ShowEmpty(Icons.Lyrics, Loc.S("Lyrics_None"));
            return;
        }
        _state.ShowContent();
        _scroller.ChangeView(null, 0, null, disableAnimation: true);
    }

    private void Seek(long ms)
    {
        _userScrollTick = 0;
        Player.Seek(ms);
    }

    // The last line that has started, by binary search.
    private void OnPosition()
    {
        if (_blocks.Count == 0) return;
        long pos = Player.PositionMs;
        int lo = 0, hi = _lyrics.Synced.Count - 1, active = -1;
        while (lo <= hi)
        {
            int mid = (lo + hi) / 2;
            if (_lyrics.Synced[mid].TimeMs <= pos)
            {
                active = mid;
                lo = mid + 1;
            }
            else hi = mid - 1;
        }
        if (active == _active) return;
        if (_active >= 0) _blocks[_active].Style = Ui.Styles.LyricsLineInactive;
        _active = active;
        if (active < 0) return;
        var line = _blocks[active];
        line.Style = Ui.Styles.LyricsLine;
        AutomationProperties.SetLiveSetting(line, AutomationLiveSetting.Polite);
        FrameworkElementAutomationPeer.FromElement(line)?.RaiseAutomationEvent(AutomationEvents.LiveRegionChanged);
        if (Environment.TickCount64 - _userScrollTick < Timing.LyricsFollowPauseMs) return;
        var at = line.TransformToVisual(_lines).TransformPoint(new Windows.Foundation.Point(0, 0));
        double header = _lines.ActualOffset.Y;
        _scroller.ChangeView(null, Math.Max(0, header + at.Y - _scroller.ViewportHeight * Size.LyricsLineAt), null);
    }
}
