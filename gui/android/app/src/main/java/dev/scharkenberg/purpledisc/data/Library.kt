package dev.scharkenberg.purpledisc.data

import android.os.SystemClock
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.setValue

/**
 * The replies the screens loaded, by key ("album:<id>", "playlist:<id>", "lyrics:<id>", ...):
 * a screen opened again paints at once, and loads again behind when the reply is older than
 * [MAX_AGE_MS]. The most recently used [COLLECTIONS] replies are kept, and lyrics apart.
 * Used from the main thread only.
 */
object Library {
    private const val MAX_AGE_MS = 5 * 60_000L
    private const val COLLECTIONS = 20
    private const val LYRICS = 20

    private class Entry(val value: Any, val at: Long)

    private fun lru(max: Int) = object : LinkedHashMap<String, Entry>(16, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, Entry>) = size > max
    }

    private val collections = lru(COLLECTIONS)
    private val lyrics = lru(LYRICS)

    private fun map(key: String) = if (key.startsWith("lyrics:")) lyrics else collections

    /** Counts the drops; a screen showing a kept reply reads it again after one. */
    var drops by mutableIntStateOf(0)
        private set

    fun peek(key: String): Any? = map(key)[key]?.value

    fun isFresh(key: String): Boolean = map(key)[key]?.let { SystemClock.elapsedRealtime() - it.at < MAX_AGE_MS } ?: false

    fun store(key: String, value: Any) {
        map(key)[key] = Entry(value, SystemClock.elapsedRealtime())
    }

    /** Forgets a reply an edit made wrong (a renamed or changed playlist). */
    fun drop(key: String) {
        map(key).remove(key)
        drops++
    }
}
