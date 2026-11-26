package dev.scharkenberg.purpledisc

import android.net.Uri

/** Navigation routes. Display names are URL-encoded into path arguments. */
object Routes {
    const val HOME = "home"
    const val SEARCH = "search"
    const val LIBRARY = "library"
    const val LIKED = "liked"
    const val PLAYLISTS = "playlists"
    const val ALBUMS = "albums"
    const val ARTISTS = "artists"
    const val PODCASTS = "podcasts"
    const val DOWNLOADED = "downloaded"
    const val HISTORY = "history"
    const val CHARTS = "charts"
    const val NOW_PLAYING = "now_playing"
    const val LYRICS = "lyrics"
    const val QUEUE = "queue"
    const val SETTINGS = "settings"
    const val EQUALIZER = "equalizer"

    const val PLAYLIST = "playlist/{id}/{name}"
    const val ALBUM = "album/{id}/{name}"
    const val ARTIST = "artist/{id}/{name}"
    const val PODCAST = "podcast/{id}/{name}"

    /** The tabs of the navigation bar. */
    val TABS = listOf(HOME, SEARCH, LIBRARY)

    /** Full-screen pages: no mini player, no navigation bar. */
    val FULL = setOf(NOW_PLAYING, LYRICS, QUEUE)

    private fun enc(s: String) = Uri.encode(s.ifBlank { " " })

    fun playlist(id: String, name: String) = "playlist/$id/${enc(name)}"
    fun album(id: String, name: String) = "album/$id/${enc(name)}"
    fun artist(id: String, name: String) = "artist/$id/${enc(name)}"
    fun podcast(id: String, name: String) = "podcast/$id/${enc(name)}"
}
