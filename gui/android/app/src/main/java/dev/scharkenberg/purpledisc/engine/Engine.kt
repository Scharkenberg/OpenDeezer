package dev.scharkenberg.purpledisc.engine

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import pdmobile.Pdmobile
import org.json.JSONObject

/**
 * Thin Kotlin facade over the gomobile-bound [Pdmobile] static methods.
 *
 * gomobile maps Go `int` to Java `long`; the getters here ([state], [quality],
 * [crossfadeMs], [finishedCount]) convert to Int. Everything that touches the
 * network is exposed as a suspend function pinned to [Dispatchers.IO].
 */
object Engine {
    // Calls that finish whatever screen started them.
    private val background = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    // Player states (mirror audio.State in the Go core).
    const val STOPPED = 0
    const val LOADING = 1
    const val PLAYING = 2
    const val PAUSED = 3
    const val ERROR = 4

    const val PLAY_FAILED = 0
    const val PLAY_STARTED = 1
    const val PLAY_SUPERSEDED = 2

    // ---- lifecycle / account ----

    suspend fun init(arl: String): Boolean = withContext(Dispatchers.IO) { Pdmobile.init(arl) }

    // The app owns the play order (shuffle only records the flag). Call before [init].
    fun setShuffleHostOwned() = Pdmobile.setShuffleHostOwned(true)

    // Deezer refused the account's login since it signed in.
    fun sessionExpired(): Boolean = Pdmobile.sessionExpired()

    // Every track the account can play offline; null when the reply failed.
    suspend fun offlineIds(): Set<String>? = io { Json.ids(Pdmobile.offlineIDsJSON()) }

    // The account's pins, newest first; null when the reply failed.
    suspend fun offlineCollections(): List<OfflineCollection>? =
        io { Json.offlineCollections(Pdmobile.offlineCollectionsJSON()) }

    // Whether offline fetches run, and how many tracks they have fetched so far.
    suspend fun offlineProgress(): OfflineProgress? =
        io { Json.offlineProgress(Pdmobile.offlineProgressJSON()) }

    // Why the most recent [init] failed: 0 ok, 1 ARL expired/invalid, 2 no
    // internet, 3 other. Only meaningful right after [init] returned false.
    // gomobile maps Go int -> Java long, hence toInt().
    fun loginErrorKind(): Int = Pdmobile.loginErrorKind().toInt()

    suspend fun account(): Account? = withContext(Dispatchers.IO) {
        Json.account(Pdmobile.account())
    }

    fun userId(): String = Pdmobile.userID()

    // Names the list the next plays come from (see [ListenContext]).
    fun setListenContext(context: ListenContext) {
        Pdmobile.setListenContext(context.type, context.id)
    }

    fun setClientInfo(client: String, device: String) {
        Pdmobile.setClientInfo(client, device)
    }

    // Tears the engine session down: stops playback, closes the control server
    // and Connect-host advertising, and forgets the Deezer client so a later
    // init starts fresh. Suspend — it closes network listeners.
    suspend fun logout() = io { Pdmobile.logout() }

    // Checks GitHub for a newer release. Network-bound and best-effort — a
    // failure (offline, rate-limited, etc.) just yields null, never a crash.
    suspend fun checkUpdate(): UpdateInfo? = io { Json.updateInfo(Pdmobile.checkUpdate()) }

    // ---- browse (all network; return parsed models) ----


    // The account's liked track ids in a single call (reuses the same favorites
    // fetch the library view uses), so hearts render truthfully across lists
    // without a per-track lookup. Null on error / not logged in / offline.
    suspend fun favoriteIds(): Set<String>? =
        io { Json.favoriteIds(Pdmobile.favoriteIDsJSON()) }
    // The TV app's lists; an error reads as an empty list there.
    suspend fun playlistTracks(id: String): List<Track> = io { Json.tracks(Pdmobile.playlistTracks(id)) }
    suspend fun albumTracks(id: String): List<Track> = io { Json.tracks(Pdmobile.albumTracks(id)) }
    suspend fun artistTop(id: String): List<Track> = io { Json.tracks(Pdmobile.artistTop(id)) }

    // Full artist profile (top tracks + albums + related artists) in one call.
    // Null when the engine returned an error payload so the screen can offer a retry.
    suspend fun artistProfile(id: String): ArtistPage? =
        io { Json.artistPage(Pdmobile.artistProfile(id)) }

    // Null when the engine returned an {"error":...} payload (or nothing), so a failed
    // load is told apart from an empty one and can be retried.
    suspend fun homeOrNull(): HomeData? =
        io { raw(Pdmobile.home())?.let(Json::home) }
    suspend fun searchOrNull(q: String): SearchResults? =
        io { raw(Pdmobile.search(q))?.let(Json::search) }
    suspend fun chartsOrNull(): SearchResults? =
        io { raw(Pdmobile.charts())?.let(Json::search) }
    suspend fun genreChartsOrNull(id: String): SearchResults? =
        io { raw(Pdmobile.genreCharts(id))?.let(Json::search) }
    // The chart genres as (id, name), "All" (id "0") first; null when the reply failed.
    suspend fun genresOrNull(): List<Pair<String, String>>? =
        io { raw(Pdmobile.genres())?.let(Json::genres) }
    // A fresh Flow ([mood]: one of [flowMoods]' ids, "" for Flow itself): its first tracks.
    suspend fun flow(mood: String): List<Track> =
        io { raw(Pdmobile.flow(mood))?.let(Json::tracks) }.orEmpty()
    suspend fun flowMoods(): List<Pair<String, String>> = io { Json.flowMoods(Pdmobile.flowMoodsJSON()) }
    suspend fun favoritesOrNull(): List<Track>? =
        io { raw(Pdmobile.favorites())?.let(Json::tracks) }
    suspend fun playlistsOrNull(): List<Playlist>? =
        io { raw(Pdmobile.playlists())?.let(Json::playlists) }
    suspend fun lyricsOrNull(id: String): Lyrics? =
        io { raw(Pdmobile.lyrics(id))?.let(Json::lyrics) }
    suspend fun playlistTracksOrNull(id: String): List<Track>? =
        io { raw(Pdmobile.playlistTracks(id))?.let(Json::tracks) }
    suspend fun albumTracksOrNull(id: String): List<Track>? =
        io { raw(Pdmobile.albumTracks(id))?.let(Json::tracks) }
    suspend fun searchPodcastsOrNull(q: String): List<Podcast>? =
        io { raw(Pdmobile.searchPodcasts(q))?.let(Json::podcasts) }
    suspend fun podcastEpisodesOrNull(id: String): List<Episode>? =
        io { raw(Pdmobile.podcastEpisodes(id))?.let(Json::episodes) }

    private fun raw(s: String?): String? = if (Json.hasError(s)) null else s

    // ---- radio mixes ----
    // "Song radio" / "artist radio": both return {tracks:[...]} in the same wire
    // shape as Flow, so they parse and play through the exact Flow path.

    suspend fun trackMix(id: String): List<Track> = io { Json.tracks(Pdmobile.trackMixJSON(id)) }
    suspend fun artistMix(id: String): List<Track> = io { Json.tracks(Pdmobile.artistMixJSON(id)) }

    // ---- listening history (machine-local) ----
    // HistoryRecentJSON returns a bare JSON array (newest first); HistoryStatsJSON
    // returns {topTracks,topArtists,totalSeconds}. gomobile maps Go int -> Java
    // long, hence the toLong() on the argument.

    suspend fun historyRecent(n: Int): List<HistoryEntry> =
        io { Json.historyRecent(Pdmobile.historyRecentJSON(n.toLong())) }
    suspend fun historyStats(sinceDays: Int): HistoryStats =
        io { Json.historyStats(Pdmobile.historyStatsJSON(sinceDays.toLong())) }

    // ---- playback ----

    // The engine answers [PLAY_STARTED], [PLAY_FAILED], or [PLAY_SUPERSEDED] when a newer
    // play (another tap, a remote) took over first.
    suspend fun play(trackId: String, durationMs: Long): Int =
        io { Pdmobile.play(trackId, durationMs).toInt() }

    suspend fun playEpisode(id: String, durationMs: Long = 0L): Int =
        io { Pdmobile.playEpisodeMS(id, durationMs).toInt() }

    // Why the last play failed, and whether the network was the reason.
    suspend fun lastError(): LastError = io { Json.lastError(Pdmobile.lastErrorJSON()) }

    // ---- gapless preload ----
    // Resolves and buffers the NEXT queue track so the engine swaps into it
    // seamlessly at the track boundary instead of the UI doing a full network
    // re-resolve (mirrors the desktop GUIs' DZPreload). The bound Go func
    // returns an error, which gomobile surfaces as a thrown exception — mapped
    // to false here so callers can fall back to the plain-resolve advance.
    suspend fun preload(id: String): Boolean =
        io { runCatching { Pdmobile.preload(id); true }.getOrDefault(false) }

    // Discards a previously armed preload. Call whenever the upcoming track
    // stops being deterministic (shuffle/repeat toggles, queue edits, stop).
    suspend fun clearPreload() = io { Pdmobile.clearPreload() }

    // ---- engine queue sync (app queue -> engine) ----
    // Mirror the app-owned queue into the engine so remote controllers see the
    // real queue on /status and /next|/prev walk it, and so the engine can own
    // gapless-promote over the synced queue. [json] is a JSON array in the shared
    // list wire shape ({id,name,durationMs,artistLine,artistId,artists,albumName,
    // artworkUrl,explicit}; see PlayerController.queueJson). setQueueJson resets
    // the engine cursor to 0, so always follow it with [setQueueIndex].
    suspend fun setQueueJson(json: String): Boolean =
        io { runCatching { Pdmobile.setQueueJSON(json); true }.getOrDefault(false) }
    suspend fun setQueueIndex(i: Int) = io { Pdmobile.setQueueIndex(i.toLong()) }

    // ---- engine queue adoption (engine -> app) ----
    // QueueVersion bumps on every change of the engine queue's content (a remote controller
    // editing this device's queue, /play/album, …), not on cursor moves; the poll reads it and
    // pulls the full [queueSnapshot] only when it moved, to adopt a remote's edits, and reads
    // the cursor, which a remote's next and previous move. All are synchronous engine reads.
    fun queueVersion(): Long = Pdmobile.queueVersion()
    fun queueIndex(): Int = Pdmobile.queueIndex().toInt()
    fun queueSnapshot(): QueueSnapshot? =
        Json.queueSnapshot(Pdmobile.queueJSON())

    // ---- engine queue edits (app -> engine) ----
    // Mutate the engine-owned queue directly so remote controllers stay
    // consistent; each bumps QueueVersion, which the PlayerController poll adopts
    // to re-sync the app queue + UI. gomobile maps Go int -> Java long, hence the
    // toLong() on the index arguments.

    // Removes the track at [index] (the engine refuses to drop the playing row).
    suspend fun queueRemove(index: Int) = io { Pdmobile.queueRemove(index.toLong()) }
    // Reorders the queue (from -> to); the cursor/history follow. Out-of-range /
    // from==to are engine-side no-ops.
    suspend fun queueMove(from: Int, to: Int) =
        io { Pdmobile.queueMove(from.toLong(), to.toLong()) }
    // Inserts the wire-shaped [trackJson] (single object or 1-element array)
    // immediately after the current row.
    suspend fun queueInsertNext(trackJson: String) =
        io { Pdmobile.queueInsertNext(trackJson) }
    // Appends the wire-shaped tracks (a JSON array) at the end.
    suspend fun queueAppend(json: String) = io { runCatching { Pdmobile.appendQueueJSON(json) }.let {} }

    // ---- downloads (premium-only; the engine rejects the request otherwise) ----
    // Returns the engine's raw JSON: {"path":"..."} on success or {"error":"..."}.
    // Pass "" for [dir] to save into the shared default download folder.
    suspend fun download(id: String, dir: String): String =
        io { Pdmobile.downloadTrack(id, dir) }

    // Album/playlist batch download to the shared folder; returns the engine's
    // batch summary {"saved":N,"failed":N,"dir":"...","error":""}. Premium-only,
    // blocking — kept on Dispatchers.IO like [download].
    suspend fun downloadAlbum(id: String): String =
        io { Pdmobile.downloadAlbum(id) }
    suspend fun downloadPlaylist(id: String): String =
        io { Pdmobile.downloadPlaylist(id) }

    // The shared default download folder (empty when the engine has none yet).
    suspend fun downloadDir(): String = io { Pdmobile.downloadDir() }

    // Whether the currently-playing stream is a 30s preview rather than the full track.
    fun isPreview(): Boolean = Pdmobile.isPreview()

    // ---- ads (Deezer Free only) ----
    // Whether ad reporting is suppressed. Disabling ads also stops reporting plays,
    // which breaks Deezer's terms — see the disclaimer in SettingsScreen.
    suspend fun adsDisabled(): Boolean = io { Pdmobile.adsDisabled() }
    suspend fun setAdsDisabled(off: Boolean): Boolean =
        io { Pdmobile.setAdsDisabled(off) }

    fun pause() = Pdmobile.pause()
    fun resume() = Pdmobile.resume()
    fun stop() = Pdmobile.stop()
    fun seek(ms: Long) = Pdmobile.seek(ms)
    fun setVolume(v: Double) = Pdmobile.setVolume(v)

    // Suspends/resumes the local OS audio device without touching playback
    // state — used for audio-focus handling (e.g. release audio during calls).
    fun setOutputSuspended(on: Boolean) = Pdmobile.setOutputSuspended(on)

    fun volume(): Double = Pdmobile.volume()
    fun state(): Int = Pdmobile.state().toInt()
    fun positionMs(): Long = Pdmobile.positionMS()
    fun durationMs(): Long = Pdmobile.durationMS()
    fun format(): String = Pdmobile.format()
    fun finishedCount(): Int = Pdmobile.finishedCount().toInt()
    fun nowPlayingVersion(): Long = Pdmobile.nowPlayingVersion()
    fun nowPlaying(): Track? = Json.nowPlaying(Pdmobile.nowPlaying())
    fun elsewhereVersion(): Long = Pdmobile.elsewhereVersion()
    fun elsewhere(): Elsewhere? = Json.elsewhere(Pdmobile.elsewhereJSON())

    // ---- settings ----

    fun setQuality(level: Int) = Pdmobile.setQuality(level.toLong())
    fun quality(): Int = Pdmobile.quality().toInt()
    fun setReplayGain(on: Boolean) = Pdmobile.setReplayGain(on)
    fun replayGain(): Boolean = Pdmobile.replayGain()
    fun setGapless(on: Boolean) = Pdmobile.setGapless(on)
    fun gapless(): Boolean = Pdmobile.gapless()
    fun setCrossfadeMs(ms: Int) = Pdmobile.setCrossfadeMS(ms.toLong())
    fun crossfadeMs(): Int = Pdmobile.crossfadeMS().toInt()

    // Raw-stream on-disk cache budget in MB (0 = off, the default); the engine applies a change
    // at once and keeps it.
    fun mediaCacheMB(): Int = Pdmobile.mediaCacheMB().toInt()
    suspend fun setMediaCacheMB(mb: Int): Boolean =
        io { Pdmobile.setMediaCacheMB(mb.toLong()) }

    // ---- equalizer + mono downmix ----
    // The engine owns all EQ state and persistence (manual band edits flip the
    // preset to "custom" engine-side); these only read state / forward changes.

    fun eqState(): EqState? = Json.eqState(Pdmobile.eqjson())
    fun setEqEnabled(on: Boolean) = setEq("""{"enabled":$on}""")
    fun setEqMono(on: Boolean) = setEq("""{"mono":$on}""")
    fun setEqPreset(name: String) = setEq("""{"preset":${JSONObject.quote(name)}}""")
    fun setEqBand(index: Int, gainDb: Double) = setEq("""{"band":{"index":$index,"gainDb":$gainDb}}""")
    fun setEqPreamp(db: Double) = setEq("""{"preampDb":$db}""")

    private fun setEq(js: String) = Pdmobile.setEQJSON(js)

    // ---- sleep timer ----
    // Pause after [minutes] (auto fade-out), or when the current track ends when
    // [endOfTrack] is true. minutes <= 0 with endOfTrack == false cancels the timer.
    fun setSleepTimer(minutes: Int, endOfTrack: Boolean) =
        Pdmobile.setSleepTimer(minutes.toLong(), if (endOfTrack) 1L else 0L)
    fun cancelSleepTimer() = Pdmobile.cancelSleepTimer()
    fun sleepActive(): Boolean = Pdmobile.sleepActive() != 0L
    fun sleepEndOfTrack(): Boolean = Pdmobile.sleepEndOfTrack() != 0L
    fun sleepRemainingMs(): Long = Pdmobile.sleepRemainingMS()

    // ---- library writes ----

    suspend fun addFavorite(id: String): Boolean = io { Pdmobile.addFavorite(id) }
    suspend fun removeFavorite(id: String): Boolean = io { Pdmobile.removeFavorite(id) }
    suspend fun addToPlaylist(playlistId: String, trackId: String): Boolean =
        io { Pdmobile.addToPlaylist(playlistId, trackId) }
    suspend fun removeFromPlaylist(playlistId: String, trackId: String): Boolean =
        io { Pdmobile.removeFromPlaylist(playlistId, trackId) }
    suspend fun createPlaylist(title: String): String? = io { Json.playlistId(Pdmobile.createPlaylist(title, "")) }
    suspend fun renamePlaylist(id: String, title: String): Boolean =
        io { Pdmobile.renamePlaylist(id, title) }
    suspend fun deletePlaylist(id: String): Boolean = io { Pdmobile.deletePlaylist(id) }

    // ---- Purple Disc Connect ----

    suspend fun discoverDevices(timeoutMs: Long = 700L): List<ConnectDevice> =
        io { Json.devices(Pdmobile.discoverDevices(timeoutMs)) }
    suspend fun connectDevice(addr: String): Boolean =
        io { Pdmobile.connectDevice(addr) }.also { updateConnectActive() }
    // Suspend: sends a final Stop to the remote over HTTP, so keep it off main.
    suspend fun disconnectDevice() = io { Pdmobile.disconnectDevice() }.also { updateConnectActive() }
    fun connectedDevice(): String = Pdmobile.connectedDevice()

    // The queue another Purple Disc of the account on the network plays with trackId current;
    // null when none does. Blocking (discovery): call off the main thread.
    fun takeOverJson(trackId: String): JSONObject? =
        Pdmobile.takeOverJSON(trackId)?.takeIf { it.isNotEmpty() }?.let { runCatching { JSONObject(it) }.getOrNull() }

    // ---- repeat / shuffle (shuffle reaches a Connect device; repeat stays on this queue) ----

    // mode: 0=off, 1=all, 2=one
    fun setRepeat(mode: Int) = Pdmobile.setRepeat(mode.toLong())
    // on: 0=off, 1=on
    fun setShuffle(on: Int) = Pdmobile.setShuffle(on.toLong())

    // Engine-owned repeat/shuffle truth. While routed to a Connect device these
    // return the remote host's mode (its routed snapshot), so the poll reflects
    // the host's real mode when casting and picks up external/remote changes.
    // mode: 0=off, 1=all, 2=one.
    fun repeatMode(): Int = when (Pdmobile.getRepeat()) {
        "all" -> 1
        "one" -> 2
        else -> 0
    }
    fun shuffleOn(): Boolean = Pdmobile.getShuffle()

    // ---- phone web remote ----

    suspend fun setWebRemoteEnabled(on: Boolean) =
        io { Pdmobile.webRemoteSetEnabled(if (on) 1L else 0L) }

    fun webRemoteInfo(): WebRemoteInfo? =
        Json.webRemoteInfo(Pdmobile.webRemoteInfo())

    suspend fun webRemoteQRPng(): ByteArray =
        io { Pdmobile.webRemoteQRPNG() ?: ByteArray(0) }

    // ---- Purple Disc Connect host ----
    // Advertise this device so other same-account Purple Disc apps can discover
    // and control it. Mirrors the desktop "make this device reachable" toggle.

    suspend fun setConnectHostEnabled(on: Boolean) {
        io { Pdmobile.connectHostSetEnabled(if (on) 1L else 0L) }
        updateConnectActive()
    }

    // The Connect host is on or a device is selected: the Application keeps the playback
    // service running, so a play a peer starts can begin while the app is in the background.
    private val connectOn = MutableStateFlow(false)
    val connectActive: StateFlow<Boolean> = connectOn.asStateFlow()

    private fun updateConnectActive() {
        connectOn.value = connectHostInfo()?.enabled == true || connectedDevice().isNotBlank()
    }

    fun connectHostInfo(): ConnectHostInfo? =
        Json.connectHostInfo(Pdmobile.connectHostInfo())

    // ---- library, favorites, recommendations ----

    suspend fun libraryAlbums(): List<Album>? = io { Json.libraryAlbums(Pdmobile.libraryJSON("album")) }
    suspend fun libraryArtists(): List<ArtistInfo>? = io { Json.libraryArtists(Pdmobile.libraryJSON("artist")) }
    suspend fun libraryPodcasts(): List<Podcast>? = io { Json.libraryPodcasts(Pdmobile.libraryJSON("podcast")) }

    // An album, artist, playlist or podcast among the favorites (kind "album", "artist", ...).
    suspend fun setFavorite(kind: String, id: String, on: Boolean): Boolean =
        io { Pdmobile.setFavorite(kind, id, on) }

    // 1 yes, 0 no, -1 unknown.
    suspend fun isFavorite(kind: String, id: String): Int = io { Pdmobile.isFavorite(kind, id).toInt() }

    // Keeps a track ("track") or an artist ("artist") out of the recommendations.
    suspend fun setRecommendationBan(kind: String, id: String, on: Boolean): Boolean =
        io { Pdmobile.setRecommendationBan(kind, id, on) }

    // ---- search ----

    suspend fun searchHistory(n: Int): List<SearchItem>? = io { Json.searchItems(Pdmobile.searchHistoryJSON(n.toLong())) }
    // Records a pick from a search; it runs on past the screen that made it, which navigation closes.
    fun addSearchHistory(type: String, id: String) {
        background.launch { Pdmobile.addSearchHistory(type, id) }
    }
    suspend fun clearSearchHistory(): Boolean = io { Pdmobile.clearSearchHistory() }
    suspend fun bestResult(q: String): SearchItem? = io { Json.bestResult(Pdmobile.bestResultJSON(q)) }

    // ---- podcasts ----

    suspend fun bookmarks(): Map<String, Bookmark> = io { Json.bookmarks(Pdmobile.episodeBookmarksJSON()) }
    suspend fun markEpisodePlayed(id: String, played: Boolean): Boolean =
        io { Pdmobile.markEpisodePlayed(id, played) }
    fun playbackSpeed(): Double = Pdmobile.playbackSpeed()
    fun setPlaybackSpeed(speed: Double) = Pdmobile.setPlaybackSpeed(speed)

    // ---- offline ----

    // {"error":""} or {"error":"reason"}: the reason, empty when the pin was taken.
    suspend fun pinOffline(kind: String, id: String): String =
        io { runCatching { JSONObject(Pdmobile.pinOffline(kind, id)).optString("error") }.getOrDefault("failed") }
    suspend fun unpinOffline(kind: String, id: String): String =
        io { runCatching { JSONObject(Pdmobile.unpinOffline(kind, id)).optString("error") }.getOrDefault("failed") }
    suspend fun offlineTracksOrNull(kind: String, id: String): List<Track>? =
        io { raw(Pdmobile.offlineTracksJSON(kind, id))?.let(Json::tracks) }
    fun offlineMode(): Boolean = Pdmobile.offlineMode()
    fun setOfflineMode(on: Boolean) = Pdmobile.setOfflineMode(on)

    // ---- account settings, cache ----

    suspend fun explicit(): Explicit? = io { Json.explicit(Pdmobile.explicitJSON()) }
    fun singleDevice(): Boolean = Pdmobile.singleDevice()
    fun setSingleDevice(on: Boolean) = Pdmobile.setSingleDevice(on)
    suspend fun cacheStats(): CacheStats? = io { Json.cacheStats(Pdmobile.mediaCacheStatsJSON()) }
    // Clears the stream cache, keeping the offline copies.
    suspend fun clearCache(): Boolean = io { Pdmobile.clearMediaCache(0L) != 0L }

    // The bytes at url (a cover); null on any failure and in offline mode.
    suspend fun fetch(url: String): ByteArray? = io { Pdmobile.fetch(url) }

    private suspend inline fun <T> io(crossinline block: () -> T): T =
        withContext(Dispatchers.IO) { block() }
}
