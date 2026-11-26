package dev.scharkenberg.purpledisc

import android.app.Application
import android.content.Context
import android.util.Log
import coil3.ImageLoader
import coil3.SingletonImageLoader
import coil3.network.NetworkClient
import coil3.network.NetworkFetcher
import coil3.network.NetworkRequest
import coil3.network.NetworkResponse
import coil3.network.NetworkResponseBody
import coil3.request.crossfade
import coil3.serviceLoaderEnabled
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import java.io.File
import java.io.IOException
import okio.Buffer
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.player.AudioFocus
import dev.scharkenberg.purpledisc.player.PlaybackService
import dev.scharkenberg.purpledisc.player.PlayerController
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import pdmobile.Pdmobile

/**
 * Owns the one [PlayerController] for the life of the process, so playback, its poll and
 * the playback service outlive the Activity (a swipe from recents included).
 */
class PurpleDiscApplication : Application(), SingletonImageLoader.Factory {

    // Main.immediate: the controller drives Compose state and moves engine calls to IO itself.
    private val appScope: CoroutineScope = CoroutineScope(
        SupervisorJob() + Dispatchers.Main.immediate +
            CoroutineExceptionHandler { _, e -> Log.e("PurpleDisc", "uncaught in the app scope", e) },
    )

    val player: PlayerController by lazy { PlayerController(appScope) }

    override fun onCreate() {
        super.onCreate()
        // The engine's config and stream cache live in the app's private directories; set
        // before any engine call.
        Pdmobile.setDataDir(filesDir.absolutePath)
        Pdmobile.setCacheDir(cacheDir.absolutePath)
        Engine.setShuffleHostOwned()
        // Advertise this client to Purple Disc Connect peers, with or without the screens.
        Engine.setClientInfo("android", getString(R.string.device_self_phone))
        player.focus = AudioFocus(this) { player }
        player.queueFile = File(filesDir, "queue.json")
        // Leaving the app keeps the queue with the position it stopped at.
        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStop(owner: LifecycleOwner) = player.save()
            override fun onStart(owner: LifecycleOwner) = player.wakeUp()
        })
        PlaybackService.controller = player
        // The playback service is started each time playing starts (Android may have ended it
        // while paused) and while Connect is on (host or a selected device), so it runs when a
        // peer starts a play in the background. It is stopped when nothing is queued, only if
        // this started it: one a play key woke is resuming the last queue.
        appScope.launch {
            var started = false
            player.state.combine(Engine.connectActive) { st, connect ->
                val playing = st.state == Engine.PLAYING || st.state == Engine.LOADING
                val idle = st.queue.isEmpty() && (st.state == Engine.STOPPED || st.state == Engine.ERROR)
                when {
                    playing && st.connectedDevice.isBlank() -> Want.PLAYING
                    connect || playing -> Want.CONNECT
                    idle -> Want.NONE
                    else -> Want.KEEP
                }
            }.distinctUntilChanged().collect { want ->
                when (want) {
                    Want.PLAYING, Want.CONNECT -> {
                        started = true
                        PlaybackService.start(this@PurpleDiscApplication, playing = want == Want.PLAYING)
                    }
                    Want.NONE -> if (started && PlaybackService.running) PlaybackService.stop(this@PurpleDiscApplication)
                    Want.KEEP -> {}
                }
            }
        }
    }

    // Coil's one image loader. Its network fetcher, added here instead of found through
    // ServiceLoader, downloads through the engine, so the app carries one HTTP client.
    override fun newImageLoader(context: Context): ImageLoader =
        ImageLoader.Builder(context)
            .serviceLoaderEnabled(false)
            .components { add(NetworkFetcher.Factory({ EngineNetworkClient })) }
            // Covers fade in rather than pop.
            .crossfade(true)
            .build()
}

private enum class Want { PLAYING, CONNECT, KEEP, NONE }

// Coil's requests are plain GETs of covers: the engine fetches the bytes, a failure (offline
// mode included) is the request's error and Coil's disk cache still answers for covers it has.
private object EngineNetworkClient : NetworkClient {
    override suspend fun <T> executeRequest(request: NetworkRequest, block: suspend (NetworkResponse) -> T): T {
        val sent = System.currentTimeMillis()
        val bytes = Engine.fetch(request.url) ?: throw IOException("fetch failed: ${request.url}")
        val body = NetworkResponseBody(Buffer().write(bytes))
        return block(NetworkResponse(code = 200, requestMillis = sent, responseMillis = System.currentTimeMillis(), body = body))
    }
}
