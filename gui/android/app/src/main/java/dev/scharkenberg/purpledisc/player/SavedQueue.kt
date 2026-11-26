package dev.scharkenberg.purpledisc.player

import dev.scharkenberg.purpledisc.engine.Json
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.Track
import org.json.JSONArray
import org.json.JSONObject

/**
 * The queue kept across launches (queue.json), as the Windows app keeps it: each row with the
 * list it was played from, the playing row and where it stood, shuffle (with the rows' natural
 * order) and repeat, for one account.
 */
class SavedQueue(
    val userId: String,
    val rows: List<Pair<Track, ListenContext>>,
    val index: Int,
    val positionMs: Long,
    val shuffle: Boolean,
    val repeat: Int,
    val natural: List<Int>?,
) {
    fun encode(): String = JSONObject()
        .put("userId", userId)
        .put("tracks", JSONArray(rows.map { (t, ctx) ->
            JSONObject().put("track", Json.trackObject(t)).put("episode", t.isEpisode)
                .put("listenType", ctx.type).put("listenId", ctx.id)
        }))
        .put("index", index)
        .put("positionMs", positionMs)
        .put("shuffle", shuffle)
        .put("repeat", repeat)
        .apply { natural?.let { put("natural", JSONArray(it)) } }
        .toString()

    companion object {
        fun decode(s: String): SavedQueue? = runCatching {
            val o = JSONObject(s)
            val a = o.getJSONArray("tracks")
            val rows = (0 until a.length()).map { i ->
                val r = a.getJSONObject(i)
                Json.track(r.getJSONObject("track"), r.optBoolean("episode")) to
                    ListenContext(r.optString("listenType"), r.optString("listenId"))
            }
            val nat = o.optJSONArray("natural")
            SavedQueue(
                userId = o.optString("userId"),
                rows = rows,
                index = o.optInt("index", -1),
                positionMs = o.optLong("positionMs"),
                shuffle = o.optBoolean("shuffle"),
                repeat = o.optInt("repeat").coerceIn(0, 2),
                natural = nat?.let { n -> (0 until n.length()).map { n.getInt(it) } },
            )
        }.getOrNull()
    }
}
