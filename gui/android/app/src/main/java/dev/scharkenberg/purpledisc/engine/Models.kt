package dev.scharkenberg.purpledisc.engine

import org.json.JSONArray
import org.json.JSONObject

// ---- domain models (mirror the engine's JSON wire shapes) ----

data class Artist(val id: String, val name: String)

/**
 * The list a track is played from, as Deezer's listen log names it: a page
 * type ("playlist_page", "dynamic_page_user_radio", ...) and the id of what
 * it shows. The engine reports each play with it, and keeps a list played
 * from Flow or a mix going as it runs low.
 */
data class ListenContext(val type: String, val id: String) {
    companion object {
        val NONE = ListenContext("", "")
    }
}

/**
 * What another session of the account plays. Deezer announces no pause, so it counts
 * as playing there until [untilMs] (Unix ms). [track] is null when that session went on
 * to a track it did not name.
 */
data class Elsewhere(val track: Track?, val untilMs: Long)

data class LastError(val error: String, val network: Boolean)

/** A search history entry or a best result: [type] is "Track", "Album", "Artist", "Playlist", "Podcast", ... */
data class SearchItem(val type: String, val id: String, val title: String, val subtitle: String, val picture: String)

/** An episode's bookmark: where it was left (seconds) and whether it was heard. */
data class Bookmark(val offsetSec: Long, val heard: Boolean)

/** The account's explicit-content level and the levels it may choose. */
data class Explicit(val level: String, val available: List<String>, val canEdit: Boolean)

/** The stream cache's use: all bytes (offline copies included), and the offline copies' share. */
data class CacheStats(val bytes: Long, val pinnedBytes: Long)

/** A pinned album, playlist or the single-track pins (kind "track", id ""). */
data class OfflineCollection(
    val kind: String,
    val id: String,
    val title: String,
    val cover: String,
    val trackCount: Int,
    val fetched: Int,
)

/** The offline fetches: whether any runs, and the tracks they fetched so far. */
data class OfflineProgress(val active: Boolean, val done: Int)

data class Track(
    val id: String,
    val name: String,
    val durationMs: Long,
    val artists: List<Artist>,
    val artistLine: String,
    val albumName: String,
    val artworkUrl: String,
    val explicit: Boolean,
    // Episodes ride the same queue but resolve through a different engine call.
    val isEpisode: Boolean = false,
    // Deezer streams it nowhere and has no other version of it: greyed out and stepped over.
    val unavailable: Boolean = false,
    // "" for a track saved before tracks carried it (queue and history files).
    val albumId: String = "",
)

data class Album(
    val id: String,
    val name: String,
    val artistLine: String,
    val artworkUrl: String,
)

data class Playlist(
    val id: String,
    val name: String,
    val owner: String,
    val trackCount: Int,
    val artworkUrl: String,
    val ownerId: String = "",
)

data class ArtistInfo(
    val id: String,
    val name: String,
    val artworkUrl: String,
    val nbFans: Int,
)

// Full artist profile page (mirrors the engine's ArtistPage: profile info plus
// top tracks, discography and related artists, fetched in one call).
data class ArtistPage(
    val artist: ArtistInfo,
    val top: List<Track>,
    val albums: List<Album>,
    val related: List<ArtistInfo>,
)

data class Podcast(
    val id: String,
    val name: String,
    val description: String,
    val artworkUrl: String,
    val episodeCount: Int,
)

data class Episode(
    val id: String,
    val title: String,
    val description: String,
    val releaseDate: String,
    val durationMs: Long,
    val artworkUrl: String,
) {
    fun asTrack(): Track = Track(
        id = id,
        name = title,
        durationMs = durationMs,
        artists = emptyList(),
        artistLine = releaseDate,
        albumName = "",
        artworkUrl = artworkUrl,
        explicit = false,
        isEpisode = true,
    )
}

data class Account(
    val userId: String,
    val name: String,
    val offer: String,
    val canHq: Boolean,
    val canHifi: Boolean,
    val premium: Boolean,
    val loggedIn: Boolean,
)

data class LyricLine(val timeMs: Long, val text: String, val translated: String = "")

data class Lyrics(val plain: String, val lines: List<LyricLine>) {
    val isSynced: Boolean get() = lines.isNotEmpty()
}

data class SearchResults(
    val tracks: List<Track>,
    val albums: List<Album>,
    val artists: List<ArtistInfo>,
    val playlists: List<Playlist>,
) {
    val isEmpty: Boolean
        get() = tracks.isEmpty() && albums.isEmpty() && artists.isEmpty() && playlists.isEmpty()

    companion object {
        val EMPTY = SearchResults(emptyList(), emptyList(), emptyList(), emptyList())
    }
}

data class ConnectDevice(
    val name: String,
    val addr: String,
    val client: String,
    val version: String,
)
// The client-id -> localized device-type label lives in a @Composable helper
// (ui.components.deviceTypeLabel) so it can resolve string resources.

data class WebRemoteInfo(
    val enabled: Boolean,
    val code: String,
    val url: String,
    val port: Int,
)

// Mirrors the engine's ConnectHostInfo JSON: {enabled, addr, port, name}.
data class ConnectHostInfo(
    val enabled: Boolean,
    val addr: String,
    val port: Int,
    val name: String,
)

// Mirrors the engine's EQJSON wire shape:
// {enabled, mono, preampDb, gainsDb: [10], preset, bands: [10], presets: [...]}.
data class EqState(
    val enabled: Boolean,
    val mono: Boolean,
    val preampDb: Double,
    val gainsDb: List<Double>,
    val preset: String,
    val bands: List<Double>,
    val presets: List<String>,
)

// Mirrors the engine's update.Info JSON: {current, latest, hasUpdate, url, notes}.
data class UpdateInfo(
    val current: String,
    val latest: String,
    val hasUpdate: Boolean,
    val url: String,
    val notes: String,
)

data class HomeData(
    val topTracks: List<Track>,
    val topAlbums: List<Album>,
    val playlists: List<Playlist>,
) {
    companion object {
        val EMPTY = HomeData(emptyList(), emptyList(), emptyList())
    }
}

// One machine-local listening-history entry (mirrors history.Entry's JSON tags).
data class HistoryEntry(
    val trackId: String,
    val title: String,
    val artist: String,
    val album: String,
    // "" / "track" = song, "episode" = podcast episode. Routes replay to the
    // right engine path (episodes resolve through playEpisode, not play).
    val kind: String,
    val startedAt: Long,        // unix seconds
    val durationPlayedSec: Long,
    // Filled in by the engine from the track when the play was recorded without them.
    val artworkUrl: String = "",
    val durationMs: Long = 0L,
)

// A snapshot of the engine-side queue (QueueJSON): the app adopts it when a
// remote controller edits this device's queue so now-playing + auto-advance
// stay correct. version bumps on every content change; index is the cursor.
data class QueueSnapshot(
    val version: Long,
    val index: Int,
    val tracks: List<Track>,
)

// Aggregated listening stats over a window (mirrors HistoryStatsJSON).
data class TrackStat(
    val trackId: String,
    val title: String,
    val artist: String,
    val plays: Int,
    val totalSec: Long,
    val artworkUrl: String = "",
    val durationMs: Long = 0L,
)

data class ArtistStat(
    val artist: String,
    val plays: Int,
    val totalSec: Long,
    val pictureUrl: String = "",
)

data class HistoryStats(
    val topTracks: List<TrackStat>,
    val topArtists: List<ArtistStat>,
    val totalSeconds: Long,
) {
    companion object {
        val EMPTY = HistoryStats(emptyList(), emptyList(), 0L)
    }
}

// ---- parsing (org.json; tolerant of {"error":...} payloads) ----

object Json {

    private fun obj(s: String?): JSONObject? =
        try {
            if (s.isNullOrBlank()) null else JSONObject(s)
        } catch (_: Throwable) {
            null
        }

    private fun arr(s: String?): JSONArray? =
        try {
            if (s.isNullOrBlank()) null else JSONArray(s)
        } catch (_: Throwable) {
            null
        }

    /** True when the payload is missing, unparseable, or carries a non-empty "error". */
    fun hasError(s: String?): Boolean {
        val o = obj(s) ?: return true
        return o.optString("error").isNotEmpty()
    }

    private fun JSONObject.artists(key: String): List<Artist> {
        val arr = optJSONArray(key) ?: return emptyList()
        return (0 until arr.length()).mapNotNull { i ->
            arr.optJSONObject(i)?.let { Artist(it.optString("id"), it.optString("name")) }
        }
    }

    private fun JSONObject.toTrack(episode: Boolean = false): Track {
        val arts = artists("artists")
        val line = optString("artistLine").ifBlank { arts.joinToString(", ") { it.name } }
        return Track(
            id = optString("id"),
            name = optString("name"),
            durationMs = optLong("durationMs"),
            artists = arts,
            artistLine = line,
            albumName = optString("albumName"),
            artworkUrl = optString("artworkUrl"),
            explicit = optBoolean("explicit"),
            isEpisode = episode,
            unavailable = optBoolean("unavailable"),
            albumId = optString("albumId"),
        )
    }

    private fun JSONObject.toAlbum(): Album {
        val arts = artists("artists")
        return Album(
            id = optString("id"),
            name = optString("name"),
            artistLine = arts.joinToString(", ") { it.name },
            artworkUrl = optString("artworkUrl"),
        )
    }

    private fun JSONObject.toPlaylist() = Playlist(
        id = optString("id"),
        name = optString("name"),
        owner = optString("owner"),
        trackCount = optInt("trackCount"),
        artworkUrl = optString("artworkUrl"),
        ownerId = optString("ownerId"),
    )

    private fun JSONObject.toArtistInfo() = ArtistInfo(
        id = optString("id"),
        name = optString("name"),
        artworkUrl = optString("artworkUrl"),
        nbFans = optInt("nbFans"),
    )

    private fun tracksOf(arr: JSONArray?): List<Track> {
        if (arr == null) return emptyList()
        return (0 until arr.length()).mapNotNull { arr.optJSONObject(it)?.toTrack() }
    }

    private fun albumsOf(arr: JSONArray?): List<Album> {
        if (arr == null) return emptyList()
        return (0 until arr.length()).mapNotNull { arr.optJSONObject(it)?.toAlbum() }
    }

    private fun artistsOf(arr: JSONArray?): List<ArtistInfo> {
        if (arr == null) return emptyList()
        return (0 until arr.length()).mapNotNull { arr.optJSONObject(it)?.toArtistInfo() }
    }

    private fun playlistsOf(arr: JSONArray?): List<Playlist> {
        if (arr == null) return emptyList()
        return (0 until arr.length()).mapNotNull { arr.optJSONObject(it)?.toPlaylist() }
    }

    fun genres(s: String): List<Pair<String, String>> {
        val a = obj(s)?.optJSONArray("genres") ?: return emptyList()
        return (0 until a.length()).mapNotNull { a.optJSONObject(it)?.let { g -> g.optString("id") to g.optString("name") } }
    }

    // FlowMoodsJSON: a bare array of {id, title}.
    fun flowMoods(s: String?): List<Pair<String, String>> {
        val a = arr(s) ?: return emptyList()
        return (0 until a.length()).mapNotNull { a.optJSONObject(it)?.let { m -> m.optString("id") to m.optString("title") } }
    }

    fun tracks(s: String?): List<Track> = tracks(obj(s))
    fun tracks(o: JSONObject?): List<Track> = tracksOf(o?.optJSONArray("tracks"))

    fun track(o: JSONObject, episode: Boolean = false): Track = o.toTrack(episode)

    // A track on the engine's list wire, which [track] reads back.
    fun trackObject(t: Track): JSONObject {
        val artists = JSONArray()
        for (a in t.artists) artists.put(JSONObject().put("id", a.id).put("name", a.name))
        return JSONObject()
            .put("id", t.id)
            .put("name", t.name)
            .put("durationMs", t.durationMs)
            .put("artistLine", t.artistLine)
            .put("artistId", t.artists.firstOrNull()?.id ?: "")
            .put("artists", artists)
            .put("albumName", t.albumName)
            .put("albumId", t.albumId)
            .put("artworkUrl", t.artworkUrl)
            .put("explicit", t.explicit)
            .put("unavailable", t.unavailable)
    }

    fun playlists(s: String?): List<Playlist> = playlistsOf(obj(s)?.optJSONArray("playlists"))

    fun search(s: String?): SearchResults {
        val o = obj(s) ?: return SearchResults.EMPTY
        return SearchResults(
            tracks = tracksOf(o.optJSONArray("tracks")),
            albums = albumsOf(o.optJSONArray("albums")),
            artists = artistsOf(o.optJSONArray("artists")),
            playlists = playlistsOf(o.optJSONArray("playlists")),
        )
    }

    fun podcasts(s: String?): List<Podcast> {
        val arr = obj(s)?.optJSONArray("podcasts") ?: return emptyList()
        return (0 until arr.length()).mapNotNull { i ->
            arr.optJSONObject(i)?.let {
                Podcast(
                    id = it.optString("id"),
                    name = it.optString("name"),
                    description = it.optString("description"),
                    artworkUrl = it.optString("artworkUrl"),
                    episodeCount = it.optInt("episodeCount"),
                )
            }
        }
    }

    fun episodes(s: String?): List<Episode> {
        val arr = obj(s)?.optJSONArray("episodes") ?: return emptyList()
        return (0 until arr.length()).mapNotNull { i ->
            arr.optJSONObject(i)?.let {
                Episode(
                    id = it.optString("id"),
                    title = it.optString("title"),
                    description = it.optString("description"),
                    releaseDate = it.optString("releaseDate"),
                    durationMs = it.optLong("durationMs"),
                    artworkUrl = it.optString("artworkUrl"),
                )
            }
        }
    }

    // ---- artist profile ----

    fun artistPage(s: String?): ArtistPage? {
        val o = obj(s) ?: return null
        if (o.has("error")) return null
        val info = o.optJSONObject("artist") ?: return null
        return ArtistPage(
            artist = info.toArtistInfo(),
            top = tracksOf(o.optJSONArray("top")),
            albums = albumsOf(o.optJSONArray("albums")),
            related = artistsOf(o.optJSONArray("related")),
        )
    }

    fun account(s: String?): Account? {
        val o = obj(s) ?: return null
        if (o.has("error")) return null
        return Account(
            userId = o.optString("userId"),
            name = o.optString("name"),
            offer = o.optString("offer"),
            canHq = o.optBoolean("canHq"),
            canHifi = o.optBoolean("canHifi"),
            premium = o.optBoolean("premium"),
            loggedIn = o.optBoolean("loggedIn"),
        )
    }

    fun nowPlaying(s: String?): Track? {
        val o = obj(s) ?: return null
        if (o.optString("id").isBlank()) return null
        return o.toTrack()
    }

    fun lastError(s: String?): LastError {
        val o = obj(s) ?: return LastError("", false)
        return LastError(o.optString("error"), o.optBoolean("network"))
    }

    // {"ids":[...],"error":""} as a set; null when the reply failed.
    fun ids(s: String?): Set<String>? {
        val o = obj(s) ?: return null
        val a = o.optJSONArray("ids")
        if (a == null || o.optString("error").isNotEmpty()) return null
        return (0 until a.length()).map { a.optString(it) }.toSet()
    }

    fun offlineCollections(s: String?): List<OfflineCollection>? {
        val o = obj(s) ?: return null
        val a = o.optJSONArray("collections")
        if (a == null || o.optString("error").isNotEmpty()) return null
        return (0 until a.length()).mapNotNull { i ->
            a.optJSONObject(i)?.let {
                OfflineCollection(
                    it.optString("kind"), it.optString("id"), it.optString("title"), it.optString("cover"),
                    it.optInt("trackCount"), it.optInt("fetched"),
                )
            }
        }
    }

    fun offlineProgress(s: String?): OfflineProgress? {
        val o = obj(s) ?: return null
        val jobs = o.optJSONArray("jobs")
        val done = if (jobs == null) 0 else (0 until jobs.length()).sumOf { jobs.optJSONObject(it)?.optInt("done") ?: 0 }
        return OfflineProgress(o.optBoolean("active"), done)
    }

    private fun searchItem(o: JSONObject) =
        SearchItem(o.optString("type"), o.optString("id"), o.optString("title"), o.optString("subtitle"), o.optString("picture"))

    // A JSON array of items; null on an error reply.
    fun searchItems(s: String?): List<SearchItem>? {
        val a = arr(s) ?: return null
        return (0 until a.length()).mapNotNull { a.optJSONObject(it)?.let(::searchItem) }
    }

    // {} when there is none.
    fun bestResult(s: String?): SearchItem? = obj(s)?.takeIf { it.optString("id").isNotEmpty() }?.let(::searchItem)

    fun bookmarks(s: String?): Map<String, Bookmark> {
        val o = obj(s) ?: return emptyMap()
        return o.keys().asSequence().filter { it != "error" }.mapNotNull { k ->
            o.optJSONObject(k)?.let { k to Bookmark(it.optLong("offset"), it.optBoolean("heard")) }
        }.toMap()
    }

    fun explicit(s: String?): Explicit? {
        val o = obj(s)?.takeIf { it.has("level") } ?: return null
        val a = o.optJSONArray("available")
        return Explicit(o.optString("level"), if (a == null) emptyList() else (0 until a.length()).map { a.optString(it) }, o.optBoolean("canEdit"))
    }

    fun cacheStats(s: String?): CacheStats? = obj(s)?.takeIf { it.has("bytes") }?.let { CacheStats(it.optLong("bytes"), it.optLong("pinnedBytes")) }

    // LibraryJSON replies; null on an error reply.
    fun libraryAlbums(s: String?): List<Album>? = if (hasError(s)) null else albumsOf(obj(s)?.optJSONArray("albums"))
    fun libraryArtists(s: String?): List<ArtistInfo>? = if (hasError(s)) null else artistsOf(obj(s)?.optJSONArray("artists"))
    fun libraryPodcasts(s: String?): List<Podcast>? = if (hasError(s)) null else podcasts(s)

    // ElsewhereJSON: {"track", "sinceMs", "untilMs"}; null for "{}".
    fun elsewhere(s: String?): Elsewhere? {
        val o = obj(s) ?: return null
        if (!o.has("untilMs")) return null
        return Elsewhere(o.optJSONObject("track")?.toTrack(), o.optLong("untilMs"))
    }

    // Lyrics: {"plain": "...", "synced": [{"timeMs":..,"text":..}], "isSynced": ..}.
    fun lyrics(s: String?): Lyrics {
        val o = obj(s) ?: return Lyrics("", emptyList())
        val plain = o.optString("plain")
        val synced = o.optJSONArray("synced")
        val lines = if (synced == null) emptyList() else (0 until synced.length()).mapNotNull { i ->
            synced.optJSONObject(i)?.let { LyricLine(it.optLong("timeMs"), it.optString("text"), it.optString("translated")) }
        }
        return Lyrics(plain, lines)
    }

    fun devices(s: String?): List<ConnectDevice> {
        val arr = arr(s) ?: return emptyList()
        return (0 until arr.length()).mapNotNull { i ->
            arr.optJSONObject(i)?.let {
                ConnectDevice(
                    name = it.optString("name"),
                    addr = it.optString("addr"),
                    client = it.optString("client"),
                    version = it.optString("version"),
                )
            }
        }
    }

    fun playlistId(s: String?): String? {
        val o = obj(s) ?: return null
        val id = o.optString("id")
        return id.ifBlank { null }
    }

    fun webRemoteInfo(s: String?): WebRemoteInfo? {
        val o = obj(s) ?: return null
        return WebRemoteInfo(
            enabled = o.optBoolean("enabled"),
            code = o.optString("code"),
            url = o.optString("url"),
            port = o.optInt("port"),
        )
    }

    fun connectHostInfo(s: String?): ConnectHostInfo? {
        val o = obj(s) ?: return null
        return ConnectHostInfo(
            enabled = o.optBoolean("enabled"),
            addr = o.optString("addr"),
            port = o.optInt("port"),
            name = o.optString("name"),
        )
    }

    fun home(s: String?): HomeData {
        val o = obj(s) ?: return HomeData.EMPTY
        return HomeData(
            topTracks = tracksOf(o.optJSONArray("topTracks")),
            topAlbums = albumsOf(o.optJSONArray("topAlbums")),
            playlists = playlistsOf(o.optJSONArray("playlists")),
        )
    }

    // HistoryRecentJSON returns a bare JSON array (not an envelope), newest first.
    fun historyRecent(s: String?): List<HistoryEntry> {
        val arr = arr(s) ?: return emptyList()
        return (0 until arr.length()).mapNotNull { i ->
            arr.optJSONObject(i)?.let {
                HistoryEntry(
                    trackId = it.optString("trackId"),
                    title = it.optString("title"),
                    artist = it.optString("artist"),
                    album = it.optString("album"),
                    kind = it.optString("kind"),
                    startedAt = it.optLong("startedAt"),
                    durationPlayedSec = it.optLong("durationPlayedSec"),
                    artworkUrl = it.optString("artworkUrl"),
                    durationMs = it.optLong("durationMs"),
                )
            }
        }
    }

    // FavoriteIDsJSON returns a bare JSON array of track-id strings (e.g.
    // ["123","456"]); {"error"}, a missing reply or unparseable yields null.
    fun favoriteIds(s: String?): Set<String>? {
        val arr = arr(s) ?: return null
        return buildSet {
            for (i in 0 until arr.length()) arr.optString(i).takeIf { it.isNotBlank() }?.let { add(it) }
        }
    }

    // QueueJSON returns {"version":N,"index":I,"tracks":[..]} in the shared list
    // wire shape; null when missing/unparseable.
    fun queueSnapshot(s: String?): QueueSnapshot? {
        val o = obj(s) ?: return null
        return QueueSnapshot(
            version = o.optLong("version"),
            index = o.optInt("index", -1),
            tracks = tracksOf(o.optJSONArray("tracks")),
        )
    }

    fun historyStats(s: String?): HistoryStats {
        val o = obj(s) ?: return HistoryStats.EMPTY
        val tt = o.optJSONArray("topTracks")
        val ta = o.optJSONArray("topArtists")
        return HistoryStats(
            topTracks = if (tt == null) emptyList() else (0 until tt.length()).mapNotNull { i ->
                tt.optJSONObject(i)?.let {
                    TrackStat(
                        trackId = it.optString("trackId"),
                        title = it.optString("title"),
                        artist = it.optString("artist"),
                        plays = it.optInt("plays"),
                        totalSec = it.optLong("totalSec"),
                        artworkUrl = it.optString("artworkUrl"),
                        durationMs = it.optLong("durationMs"),
                    )
                }
            },
            topArtists = if (ta == null) emptyList() else (0 until ta.length()).mapNotNull { i ->
                ta.optJSONObject(i)?.let {
                    ArtistStat(
                        artist = it.optString("artist"),
                        plays = it.optInt("plays"),
                        totalSec = it.optLong("totalSec"),
                        pictureUrl = it.optString("pictureUrl"),
                    )
                }
            },
            totalSeconds = o.optLong("totalSeconds"),
        )
    }

    private fun doublesOf(arr: JSONArray?): List<Double> {
        if (arr == null) return emptyList()
        return (0 until arr.length()).map { arr.optDouble(it, 0.0) }
    }

    fun eqState(s: String?): EqState? {
        val o = obj(s) ?: return null
        if (o.has("error")) return null
        val presets = o.optJSONArray("presets")
        return EqState(
            enabled = o.optBoolean("enabled"),
            mono = o.optBoolean("mono"),
            preampDb = o.optDouble("preampDb", 0.0),
            gainsDb = doublesOf(o.optJSONArray("gainsDb")),
            preset = o.optString("preset"),
            bands = doublesOf(o.optJSONArray("bands")),
            presets = if (presets == null) emptyList() else (0 until presets.length()).map { presets.optString(it) },
        )
    }

    fun updateInfo(s: String?): UpdateInfo? {
        val o = obj(s) ?: return null
        if (o.has("error")) return null
        return UpdateInfo(
            current = o.optString("current"),
            latest = o.optString("latest"),
            hasUpdate = o.optBoolean("hasUpdate"),
            url = o.optString("url"),
            notes = o.optString("notes"),
        )
    }
}
