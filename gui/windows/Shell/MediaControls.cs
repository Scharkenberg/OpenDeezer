// The Windows media overlay and media keys (SMTC), following the Player.

using System;
using Microsoft.UI.Dispatching;
using Windows.Media;
using Windows.Storage.Streams;

namespace OpenDeezer;

internal sealed class MediaControls
{
    private static Player Player => App.Player;
    private readonly SystemMediaTransportControls? _smtc;
    private MediaPlaybackStatus _status = MediaPlaybackStatus.Closed;
    private long _timelineTick;

    public MediaControls(IntPtr hwnd, DispatcherQueue dq)
    {
        try
        {
            _smtc = SystemMediaTransportControlsInterop.GetForWindow(hwnd);
            _smtc.IsEnabled = true;
            _smtc.IsPlayEnabled = true;
            _smtc.IsPauseEnabled = true;
            _smtc.IsNextEnabled = true;
            _smtc.IsPreviousEnabled = true;
            _smtc.DisplayUpdater.Type = MediaPlaybackType.Music;
        }
        catch (Exception ex)
        {
            AppLog.Write("SMTC: " + ex.Message);
            _smtc = null;
            return;
        }
        // Both events arrive on a worker thread.
        _smtc.ButtonPressed += (_, a) =>
        {
            var b = a.Button;
            dq.TryEnqueue(() =>
            {
                if (b == SystemMediaTransportControlsButton.Play) Player.Resume();
                else if (b == SystemMediaTransportControlsButton.Pause) Player.Pause();
                else if (b == SystemMediaTransportControlsButton.Next) Player.Next();
                else if (b == SystemMediaTransportControlsButton.Previous) Player.Prev();
            });
        };
        _smtc.PlaybackPositionChangeRequested += (_, a) =>
        {
            long ms = (long)a.RequestedPlaybackPosition.TotalMilliseconds;
            dq.TryEnqueue(() =>
            {
                Player.Seek(ms);
                Timeline(ms, Player.DurationMs);
            });
        };
        Player.TrackChanged += OnTrack;
        Player.StateChanged += OnState;
        Player.PositionChanged += () =>
        {
            if (Environment.TickCount64 - _timelineTick > Timing.SmtcTimelineMs) Timeline(Player.PositionMs, Player.DurationMs);
        };
    }

    public void Disable()
    {
        if (_smtc != null) _smtc.IsEnabled = false;
    }

    private void OnTrack()
    {
        if (_smtc == null || Player.Current is not { } t) return;
        try
        {
            var du = _smtc.DisplayUpdater;
            du.Type = MediaPlaybackType.Music;
            du.MusicProperties.Title = t.Name;
            du.MusicProperties.Artist = t.ArtistLine;
            du.MusicProperties.AlbumTitle = t.AlbumName;
            du.Thumbnail = Uri.TryCreate(t.ArtworkUrl, UriKind.Absolute, out var uri) ? RandomAccessStreamReference.CreateFromUri(uri) : null;
            du.Update();
            Timeline(0, t.DurationMs);
        }
        catch (Exception ex) { AppLog.Write("SMTC update: " + ex.Message); }
    }

    private void OnState()
    {
        if (_smtc == null) return;
        var status = Player.State switch
        {
            PlayState.Playing => MediaPlaybackStatus.Playing,
            PlayState.Paused => MediaPlaybackStatus.Paused,
            PlayState.Loading => MediaPlaybackStatus.Changing,
            _ => MediaPlaybackStatus.Stopped,
        };
        if (status == _status) return;
        _status = status;
        _smtc.PlaybackStatus = status;
    }

    private void Timeline(long posMs, long durMs)
    {
        if (_smtc == null) return;
        _timelineTick = Environment.TickCount64;
        var end = TimeSpan.FromMilliseconds(Math.Max(0, durMs));
        _smtc.UpdateTimelineProperties(new SystemMediaTransportControlsTimelineProperties
        {
            StartTime = TimeSpan.Zero,
            EndTime = end,
            Position = TimeSpan.FromMilliseconds(Math.Max(0, posMs)),
            MinSeekTime = TimeSpan.Zero,
            MaxSeekTime = end,
        });
    }
}
