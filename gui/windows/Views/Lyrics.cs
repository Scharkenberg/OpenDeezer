// Lyrics of the playing track. Synced lines follow playback and seek on click or Enter (each line
// is a tab stop); scrolling by hand pauses the follow for a few seconds. Where Deezer has a
// translation, a switch shows it under each line.

using System;
using System.Collections.Generic;
using System.Linq;
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
    internal readonly StackPanel _lines = Ui.Stack(Space.M);
    internal readonly ScrollViewer _scroller;
    private readonly StateView _state;
    private readonly List<TextBlock> _blocks = new(), _translations = new();
    private readonly ToggleSwitch _translate;
    private Lyrics _lyrics = new();
    private string _shownId = ""; // the track whose lyrics are on screen, "" while none are
    private int _active = -1;
    private long _userScrollTick;

    public override FrameworkElement Root => _state.Root;
    public override ScrollViewer? Scroller => _scroller;

    public LyricsView()
    {
        _translate = Ui.Switch(false, "Lyrics_Translation", on =>
        {
            foreach (var t in _translations) t.Visibility = Ui.Show(on && t.Text.Length > 0);
        });
        _translate.OnContent = Loc.S("Lyrics_Translation");
        _translate.OffContent = Loc.S("Lyrics_Translation");
        var header = Ui.Grid("auto * auto", Space.M, _art.Root, Ui.Stack(0, _title, _artist), _translate);
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
        if (id.Length > 0 && id == _shownId) return;
        _shownId = "";
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
        // Null when the view was left meanwhile: the next visit loads again.
        if (await LoadAsync<Lyrics>(Library.Key("lyrics", id), _state) is not { } ly || Player.Current?.Id != id) return;
        if (ly.Error.Length > 0)
        {
            _state.ShowFailed(ly.Error, OnTrack);
            return;
        }
        Render(ly);
        _shownId = id;
    }

    private void Render(Lyrics lyrics)
    {
        _lyrics = lyrics;
        _lines.Children.Clear();
        _blocks.Clear();
        _translations.Clear();
        _active = -1;
        _userScrollTick = 0;
        _translate.Visibility = Ui.Show(_lyrics.IsSynced && _lyrics.Synced.Any(l => l.Translated.Length > 0));
        if (_lyrics.IsSynced && _lyrics.Synced.Count > 0)
        {
            foreach (var l in _lyrics.Synced)
            {
                var tb = Ui.Text(l.Text.Length == 0 ? Icons.NoteText : l.Text, TextKind.Body, wrap: true);
                tb.Style = Ui.Styles.LyricsLineInactive;
                var translated = Ui.Text(l.Translated, TextKind.SecondaryBody, wrap: true);
                translated.Visibility = Ui.Show(_translate.IsOn && l.Translated.Length > 0);
                long at = l.TimeMs;
                // A tab stop: Enter or Space seeks there, as a click does.
                var line = new ContentControl { Content = Ui.Stack(0, tb, translated), IsTabStop = true, UseSystemFocusVisuals = true, HorizontalContentAlignment = HorizontalAlignment.Stretch };
                AutomationProperties.SetName(line, l.Text);
                line.Tapped += (_, _) => Seek(at);
                line.KeyDown += (_, e) =>
                {
                    if (e.Key is not (Windows.System.VirtualKey.Enter or Windows.System.VirtualKey.Space)) return;
                    e.Handled = true;
                    Seek(at);
                };
                _lines.Children.Add(line);
                _blocks.Add(tb);
                _translations.Add(translated);
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
        var at = ((FrameworkElement)_lines.Children[active]).TransformToVisual(_lines).TransformPoint(new Windows.Foundation.Point(0, 0));
        double header = _lines.ActualOffset.Y;
        _scroller.ChangeView(null, Math.Max(0, header + at.Y - _scroller.ViewportHeight * Size.LyricsLineAt), null);
    }
}
