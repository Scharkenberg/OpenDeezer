// The Windows media overlay and media keys (SMTC), following the Player: transport, seek, shuffle
// and repeat. With nothing to play (a reset) the overlay is cleared.

using System;
using System.IO;
using Microsoft.UI.Dispatching;
using Windows.Media;
using Windows.Storage;
using Windows.Storage.Streams;

namespace PurpleDisc;

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
        _smtc.ShuffleEnabledChangeRequested += (_, a) =>
        {
            bool on = a.RequestedShuffleEnabled;
            dq.TryEnqueue(() => Player.SetShuffle(on));
        };
        _smtc.AutoRepeatModeChangeRequested += (_, a) =>
        {
            var mode = a.RequestedAutoRepeatMode switch
            {
                MediaPlaybackAutoRepeatMode.Track => RepeatMode.One,
                MediaPlaybackAutoRepeatMode.List => RepeatMode.All,
                _ => RepeatMode.Off,
            };
            dq.TryEnqueue(() => Player.SetRepeat(mode));
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

    public void Enable()
    {
        if (_smtc != null) _smtc.IsEnabled = true;
    }

    // The text and the timeline first; then the picture in its own step (a web address, or a local
    // file for a track kept offline), so a picture that fails leaves the overlay on this track.
    private async void OnTrack()
    {
        if (_smtc == null) return;
        var du = _smtc.DisplayUpdater;
        Track t;
        try
        {
            if (Player.Current is not { } current)
            {
                du.ClearAll();
                du.Update();
                _status = MediaPlaybackStatus.Closed;
                _smtc.PlaybackStatus = _status;
                return;
            }
            t = current;
            du.Type = MediaPlaybackType.Music;
            du.MusicProperties.Title = t.Name;
            du.MusicProperties.Artist = t.ArtistLine;
            du.MusicProperties.AlbumTitle = t.AlbumName;
            du.Thumbnail = null;
            du.Update();
            Timeline(0, t.DurationMs);
        }
        catch (Exception ex)
        {
            AppLog.Write("SMTC update: " + ex.Message);
            return;
        }
        try
        {
            RandomAccessStreamReference? thumb = null;
            if (Uri.TryCreate(t.ArtworkUrl, UriKind.Absolute, out var uri) && (uri.Scheme == Uri.UriSchemeHttp || uri.Scheme == Uri.UriSchemeHttps))
                thumb = RandomAccessStreamReference.CreateFromUri(uri);
            else if (Path.IsPathRooted(t.ArtworkUrl))
                thumb = RandomAccessStreamReference.CreateFromFile(await StorageFile.GetFileFromPathAsync(t.ArtworkUrl));
            if (thumb == null || Player.Current != t) return;
            du.Thumbnail = thumb;
            du.Update();
        }
        catch (Exception ex) { AppLog.Write("SMTC picture: " + ex.Message); }
    }

    private void OnState()
    {
        if (_smtc == null) return;
        _smtc.ShuffleEnabled = Player.Shuffle;
        _smtc.AutoRepeatMode = Player.Repeat switch
        {
            RepeatMode.One => MediaPlaybackAutoRepeatMode.Track,
            RepeatMode.All => MediaPlaybackAutoRepeatMode.List,
            _ => MediaPlaybackAutoRepeatMode.None,
        };
        if (Player.Current == null) return;
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
