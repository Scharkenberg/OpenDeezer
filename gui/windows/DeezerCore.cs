// P/Invoke declarations for libdeezercore.dll: cdecl, UTF-8 strings. Every char* result
// is freed with DZFree (TakeJson); the QR PNG with DZFreeBytes.

using System;
using System.Collections.Generic;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Text.Json.Serialization.Metadata;

namespace OpenDeezer;

internal static partial class DeezerCore
{
    private const string Dll = "libdeezercore";

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZInit(string arl);
    // Why the last DZInit failed: 1 ARL expired or invalid, 2 no internet, 4 no audio output, 0 or 3 other.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZLoginErrorKind();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZFavoritesJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZPlaylistsJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZPlaylistTracksJSON(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZAlbumTracksJSON(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZSearchJSON(string q);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZPlay(string trackID, long durationMS);   // 1 started, 0 failed, PlaySuperseded
    // DZPlay's and DZPlayEpisode's answer when another play took over before this one started.
    internal const int PlaySuperseded = 2;
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZPause();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZResume();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZTogglePause();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZStop();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSeek(long ms);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZState();          // 0 stop 1 load 2 play 3 pause 4 err
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial long DZPositionMS();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial long DZDurationMS();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetVolume(double v);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial double DZVolume();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZFinishedCount();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZLastErrorJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZFree(IntPtr s);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZFreeBytes(IntPtr p);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetQuality(int level);   // 0=MP3_128,1=MP3_320,2=FLAC
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZQuality();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZFormat();

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZAccountJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZChartsJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZArtistProfileJSON(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZTrackInfoJSON(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZLyricsJSON(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetReplayGain(int on);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZReplayGain();

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZAddFavorite(string trackID);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZRemoveFavorite(string trackID);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZAddToPlaylist(string playlistID, string trackID);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZRemoveFromPlaylist(string playlistID, string trackID);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZCreatePlaylist(string title);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZRenamePlaylist(string playlistID, string title);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZDeletePlaylist(string playlistID);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZFlowJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZSearchPodcastsJSON(string q);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZPodcastEpisodesJSON(string podcastID);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZPlayEpisode(string episodeID, long durationMS);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZAudioDevicesJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZSetAudioDevice(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZCurrentAudioDevice();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetGapless(int on);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZGapless();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetCrossfadeMS(int ms);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZCrossfadeMS();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZPreload(string trackID, long durationMS);

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetClientInfo(string client, string device);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZDiscoverDevices(int timeoutMS);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZConnectDevice(string addr);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZDisconnectDevice();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZConnectedDevice();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZNowPlayingJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial long DZNowPlayingVersion();

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetRepeat(int mode);   // 0=off,1=all,2=one
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetShuffle(int on);    // 0/1
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetShuffleHostOwned(int on);   // 1: the flag is recorded, never applied

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZWebRemoteSetEnabled(int on);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZWebRemoteInfoJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZWebRemoteQRPNG(out int outLen);

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZControlConfigJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetControlConfig(int enabled, string addr, string token);

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZHomeJSON();

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZCheckUpdateJSON();

    // Pauses after `minutes` with a fade-out, or at the end of the track; 0 and 0 cancels.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetSleepTimer(int minutes, int endOfTrack);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZCancelSleepTimer();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZSleepTimerActive();      // 1/0
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZSleepTimerEndOfTrack();   // 1/0
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial long DZSleepTimerRemainingMS();

    // DZSetEQJSON takes a partial update and returns 0 when any key is refused. The engine persists EQ in eq.json.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZEQJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZSetEQJSON(string js);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZClearPreload();

    // Downloads are Premium only and block on network and decrypt. An empty destDir is the default folder.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZDownloadTrack(string trackID, string destDir);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZDownloadDir();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZSetDownloadDir(string path);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZIsPreview();

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZAdsDisabled();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZSetAdsDisabled(int disabled);

    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZTrackMixJSON(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZArtistMixJSON(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZHistoryRecentJSON(int n);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZHistoryStatsJSON(int sinceDays);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZDownloadAlbum(string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZDownloadPlaylist(string id);
    // The stream cache: a new size applies at once (0 stops caching and removes unpinned entries).
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZMediaCacheMB();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZSetMediaCacheMB(int mb);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZMediaCacheStatsJSON();
    // Cached songs not played for this many days are removed (0: never); applies at once.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZMediaCacheMaxAgeDays();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZSetMediaCacheMaxAgeDays(int days);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZClearMediaCache(int includePinned);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZSetMediaCacheDir(string dir);
    // DZQueueSet resets the cursor to 0. Once the cursor matches the playing track the engine owns
    // auto-advance and DZFinishedCount does not count those finishes.
    // The queue calls take NUL-terminated UTF-8 the caller already holds. DZQueueAppend adds at the end, cursor kept.
    [LibraryImport(Dll), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZQueueSet(in byte js);
    [LibraryImport(Dll), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZQueueAppend(in byte js);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZQueueSetIndex(int i);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZQueueIndex();
    // Bumps on every DZQueueSet and on any other change of the list, not on cursor moves.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial long DZQueueVersion();

    // "off", "all" or "one". When casting both report the remote device.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZGetRepeat();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZGetShuffle();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZFavoriteIDsJSON();

    // The engine refuses to remove its playing row. DZQueueInsertNext inserts after the cursor.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZQueueRemove(int i);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZQueueMove(int from, int to);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZQueueInsertNext(in byte trackJson);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZQueueJSON();
    // Offline music: kind is "track", "album" or "playlist". A pin is recorded at once and fetched in
    // the background (DZOfflineProgressJSON); pins belong to the signed-in account. Offline mode is on
    // after a sign-in that failed for want of a network, or when set: nothing is requested then and
    // only pinned or cached tracks play.
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZPinOffline(string kind, string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZUnpinOffline(string kind, string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZOfflineProgressJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZOfflineCollectionsJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZOfflineTracksJSON(string kind, string id);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial IntPtr DZOfflineIDsJSON();
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial void DZSetOfflineMode(int on);
    [LibraryImport(Dll, StringMarshalling = StringMarshalling.Utf8), UnmanagedCallConv(CallConvs = [typeof(CallConvCdecl)])] internal static partial int DZOfflineMode();

    internal static string TakeJson(IntPtr p)
    {
        if (p == IntPtr.Zero) return "";
        string s = Marshal.PtrToStringUTF8(p) ?? "";
        DZFree(p);
        return s;
    }

    // A reply read straight from the engine's UTF-8 buffer, then freed.
    private static T Read<T>(IntPtr p, JsonTypeInfo<T> type) where T : new()
    {
        try { return Dto.Read(Utf8(p), type); }
        finally { DZFree(p); }
    }

    private static (T[] Items, string Error) ReadArray<T>(IntPtr p, JsonTypeInfo<T[]> type)
    {
        try { return Dto.ReadArray(Utf8(p), type); }
        finally { DZFree(p); }
    }

    // The now-playing reply as bytes: the poll compares them before reading any.
    internal static byte[] NowPlayingUtf8()
    {
        var p = DZNowPlayingJSON();
        try { return Utf8(p).ToArray(); }
        finally { DZFree(p); }
    }

    private static unsafe ReadOnlySpan<byte> Utf8(IntPtr p) =>
        p == IntPtr.Zero ? default : MemoryMarshal.CreateReadOnlySpanFromNullTerminated((byte*)p);

    internal static Account Account() => Read(DZAccountJSON(), WireJson.Default.Account);
    internal static HomeReply Home() => Read(DZHomeJSON(), WireJson.Default.HomeReply);
    internal static CatalogReply Charts() => Read(DZChartsJSON(), WireJson.Default.CatalogReply);
    internal static CatalogReply Search(string q) => Read(DZSearchJSON(q), WireJson.Default.CatalogReply);
    internal static PlaylistsReply Playlists() => Read(DZPlaylistsJSON(), WireJson.Default.PlaylistsReply);
    internal static TracksReply Favorites() => Read(DZFavoritesJSON(), WireJson.Default.TracksReply);
    internal static TracksReply Flow() => Read(DZFlowJSON(), WireJson.Default.TracksReply);
    internal static TracksReply PlaylistTracks(string id) => Read(DZPlaylistTracksJSON(id), WireJson.Default.TracksReply);
    internal static TracksReply AlbumTracks(string id) => Read(DZAlbumTracksJSON(id), WireJson.Default.TracksReply);
    internal static TracksReply TrackMix(string id) => Read(DZTrackMixJSON(id), WireJson.Default.TracksReply);
    internal static TracksReply ArtistMix(string id) => Read(DZArtistMixJSON(id), WireJson.Default.TracksReply);
    internal static ArtistProfile ArtistProfile(string id) => Read(DZArtistProfileJSON(id), WireJson.Default.ArtistProfile);
    internal static TrackInfo TrackInfo(string id) => Read(DZTrackInfoJSON(id), WireJson.Default.TrackInfo);
    internal static Lyrics Lyrics(string id) => Read(DZLyricsJSON(id), WireJson.Default.Lyrics);
    internal static PodcastsReply SearchPodcasts(string q) => Read(DZSearchPodcastsJSON(q), WireJson.Default.PodcastsReply);
    internal static EpisodesReply PodcastEpisodes(string id) => Read(DZPodcastEpisodesJSON(id), WireJson.Default.EpisodesReply);
    internal static CreatedReply CreatePlaylist(string name) => Read(DZCreatePlaylist(name), WireJson.Default.CreatedReply);
    internal static QueueReply Queue() => Read(DZQueueJSON(), WireJson.Default.QueueReply);
    internal static string LastError() => Read(DZLastErrorJSON(), WireJson.Default.Reply).Error;
    internal static DevicesReply AudioDevices() => Read(DZAudioDevicesJSON(), WireJson.Default.DevicesReply);
    internal static ControlConfig ControlConfig() => Read(DZControlConfigJSON(), WireJson.Default.ControlConfig);
    internal static WebRemoteInfo WebRemoteInfo() => Read(DZWebRemoteInfoJSON(), WireJson.Default.WebRemoteInfo);
    internal static EQState EQ() => Read(DZEQJSON(), WireJson.Default.EQState);
    internal static HistoryStats HistoryStats(int sinceDays) => Read(DZHistoryStatsJSON(sinceDays), WireJson.Default.HistoryStats);
    internal static UpdateInfo CheckUpdate() => Read(DZCheckUpdateJSON(), WireJson.Default.UpdateInfo).ForApp(Config.AppVersion());
    internal static SavedReply Download(string id, string dir) => Read(DZDownloadTrack(id, dir), WireJson.Default.SavedReply);
    internal static BatchReply DownloadAlbum(string id) => Read(DZDownloadAlbum(id), WireJson.Default.BatchReply);
    internal static BatchReply DownloadPlaylist(string id) => Read(DZDownloadPlaylist(id), WireJson.Default.BatchReply);
    // "" when the pin was taken, else the reason.
    internal static string PinOffline(string kind, string id, bool on) =>
        Read(on ? DZPinOffline(kind, id) : DZUnpinOffline(kind, id), WireJson.Default.Reply).Error;
    internal static OfflineProgress OfflineProgress() => Read(DZOfflineProgressJSON(), WireJson.Default.OfflineProgress);
    internal static OfflineCollectionsReply OfflineCollections() => Read(DZOfflineCollectionsJSON(), WireJson.Default.OfflineCollectionsReply);
    internal static TracksReply OfflineTracks(string kind, string id) => Read(DZOfflineTracksJSON(kind, id), WireJson.Default.TracksReply);
    internal static OfflineIdsReply OfflineIds() => Read(DZOfflineIDsJSON(), WireJson.Default.OfflineIdsReply);

    internal static (string[] Ids, string Error) FavoriteIds() => ReadArray(DZFavoriteIDsJSON(), WireJson.Default.StringArray);
    internal static ConnectDevice[] DiscoverDevices(int timeoutMs) => ReadArray(DZDiscoverDevices(timeoutMs), WireJson.Default.ConnectDeviceArray).Items;

    internal static List<Track> HistoryRecent(int n)
    {
        var list = new List<Track>();
        foreach (var e in ReadArray(DZHistoryRecentJSON(n), WireJson.Default.HistoryEntryArray).Items)
            if (e.TrackId.Length > 0) list.Add(e.ToTrack());
        return list;
    }

    internal static string Format() => TakeJson(DZFormat());
    internal static string ConnectedDevice() => TakeJson(DZConnectedDevice());
    internal static string CurrentAudioDevice() => TakeJson(DZCurrentAudioDevice());
    internal static string DownloadDir() => TakeJson(DZDownloadDir());
    internal static bool SetDownloadDir(string p) => DZSetDownloadDir(p) == 1;
    internal static bool IsPreview() => DZIsPreview() == 1;
    internal static bool AdsDisabled() => DZAdsDisabled() == 1;
    internal static bool SetAdsDisabled(bool off) => DZSetAdsDisabled(off ? 1 : 0) == 1;
    internal static int MediaCacheMB() => DZMediaCacheMB();
    internal static bool SetMediaCacheMB(int mb) => DZSetMediaCacheMB(mb) == 1;
    internal static MediaCacheStats MediaCacheStats() => Read(DZMediaCacheStatsJSON(), WireJson.Default.MediaCacheStats);
    // "" when the folder was taken, else the reason.
    internal static string SetMediaCacheDir(string dir) => Read(DZSetMediaCacheDir(dir), WireJson.Default.Reply).Error;
    internal static bool GetShuffle() => DZGetShuffle() != 0;

    internal static RepeatMode GetRepeat() => TakeJson(DZGetRepeat()) switch
    {
        "one" or "2" => RepeatMode.One,
        "all" or "1" => RepeatMode.All,
        _ => RepeatMode.Off,
    };

    internal static byte[] WebRemoteQRPng()
    {
        IntPtr p = DZWebRemoteQRPNG(out int len);
        if (p == IntPtr.Zero) return [];
        byte[] data = len > 0 ? new byte[len] : [];
        if (len > 0) Marshal.Copy(p, data, 0, len);
        DZFreeBytes(p);
        return data;
    }
}
