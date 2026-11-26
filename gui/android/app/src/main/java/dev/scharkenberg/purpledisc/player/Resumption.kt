package dev.scharkenberg.purpledisc.player

import android.content.Context
import android.content.Intent
import androidx.annotation.OptIn
import androidx.media3.common.util.UnstableApi
import androidx.media3.session.MediaButtonReceiver
import dev.scharkenberg.purpledisc.data.Prefs
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.Track
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.io.File

/**
 * A play key (headset, Bluetooth, the system's media controls) while the app is not running
 * picks up the last queue where it stopped: the saved sign-in first, without the app's
 * screens, then the saved queue.
 */
internal object Resumption {
    private val lock = Mutex()

    /** The row to play and where in it, or null when there is nothing to resume. */
    suspend fun prepare(context: Context, c: PlayerController): Pair<Track, Long>? = lock.withLock {
        val loaded = c.state.value.current
        if (loaded != null && Engine.userId().isNotEmpty()) return loaded to c.progress.value.positionMs
        run {
            val saved = c.savedQueue() ?: return@run null
            if (Engine.userId().isEmpty()) {
                val prefs = withContext(Dispatchers.IO) { Prefs(context) }
                val arl = withContext(Dispatchers.IO) { prefs.loadArl() }
                if (arl.isNullOrBlank() || !Engine.init(arl)) return@run null
                // Another account signed in since: its queue is not this one.
                if (Engine.userId() != saved.userId) return@run null
                Engine.account()?.let { c.premium = it.premium }
                prefs.applyToEngine()
                c.start()
            }
            c.restore(saved.userId)
            val cur = c.state.first { it.current != null }.current ?: return@run null
            cur to saved.positionMs
        }
    }
}

/**
 * Media3's media button receiver, waking the service only when there is a queue and a
 * sign-in to pick up: a foreground service with nothing to show is ended by the system.
 */
@OptIn(UnstableApi::class)
class ResumeButtonReceiver : MediaButtonReceiver() {
    override fun shouldStartForegroundService(context: Context, intent: Intent): Boolean =
        File(context.filesDir, "queue.json").isFile && Prefs.hasArl(context)
}
