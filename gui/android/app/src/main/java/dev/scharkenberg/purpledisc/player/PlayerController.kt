package dev.scharkenberg.purpledisc.player

import android.content.Context
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.engine.Elsewhere
import dev.scharkenberg.purpledisc.engine.Json
import dev.scharkenberg.purpledisc.data.Library
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.LastError
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.QueueSnapshot
import dev.scharkenberg.purpledisc.engine.Track
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ProcessLifecycleOwner
import org.json.JSONArray
import java.io.File
import org.json.JSONObject

/** A queue row: [uid] tells two rows of the same track apart and survives moves. */
data class QueueEntry(val uid: Long, val track: Track)

data class PlayerState(
    val current: Track? = null,
    val state: Int = Engine.STOPPED,
    val volume: Double = 1.0,
    val format: String = "",
    val queue: List<QueueEntry> = emptyList(),
    val index: Int = -1,
    val connectedDevice: String = "",
    val repeatMode: Int = 0, // 0=off, 1=all, 2=one
    val shuffle: Boolean = false,
    // What another session of the account plays, while it counts as playing there.
    val elsewhere: Elsewhere? = null,
    // The stream is Deezer's 30-second preview.
    val preview: Boolean = false,
) {
    val isPlaying: Boolean get() = state == Engine.PLAYING

    // Nothing plays here (nor on a Connect device) while another session plays.
    val showElsewhere: Boolean get() =
        elsewhere != null && connectedDevice.isBlank() && state != Engine.PLAYING && state != Engine.LOADING

    val hasNext: Boolean get() = index + 1 < queue.size || (repeatMode == 1 && queue.size > 1)
    val hasPrev: Boolean get() = queue.isNotEmpty() && index >= 0
}

/** The play position and the track's length, apart from [PlayerState] as they change every tick. */
data class Progress(val positionMs: Long = 0L, val durationMs: Long = 0L)

/** One-shot outcomes the app shell shows as a snackbar. */
sealed interface PlayerEvent {
    data class DownloadStarted(val name: String) : PlayerEvent

    // Nothing ahead in the queue is available on Deezer; playback stopped.
    data object Unavailable : PlayerEvent
    data class DownloadSaved(val name: String, val path: String) : PlayerEvent
    data class DownloadFailed(val name: String, val error: String) : PlayerEvent

    // An album or playlist download: [name] is the collection's.
    data class BatchDone(val name: String, val saved: Int, val failed: Int, val error: String) : PlayerEvent

    // The engine refused an offline pin; [error] is its reason, blank when the reply could not be read.
    data class OfflineFailed(val name: String, val error: String) : PlayerEvent

    // A track could not play, and playback skips to the next or stops; [several]: it stopped
    // after several failures in a row.
    data class PlayFailed(val reason: String, val skipping: Boolean, val several: Boolean = false) : PlayerEvent
    data object NoInternet : PlayerEvent
    data object RadioEmpty : PlayerEvent
    data object ConnectFailed : PlayerEvent
}

/** The snackbar line for [event]. */
fun eventMessage(context: Context, event: PlayerEvent): String = when (event) {
    PlayerEvent.Unavailable -> context.getString(R.string.track_unavailable)
    is PlayerEvent.DownloadStarted -> context.getString(R.string.download_started, event.name)
    is PlayerEvent.DownloadSaved ->
        if (event.path.isBlank()) context.getString(R.string.download_saved_generic, event.name)
        else context.getString(R.string.download_saved, event.path)
    is PlayerEvent.DownloadFailed, is PlayerEvent.OfflineFailed -> {
        val error = if (event is PlayerEvent.DownloadFailed) event.error else (event as PlayerEvent.OfflineFailed).error
        if (error.isBlank()) context.getString(R.string.download_failed_generic) else context.getString(R.string.download_failed, error)
    }
    is PlayerEvent.BatchDone -> when {
        event.saved == 0 && event.error.isNotBlank() -> context.getString(R.string.download_failed, event.error)
        event.saved == 0 -> context.getString(R.string.download_failed_generic)
        event.failed > 0 -> context.getString(R.string.download_batch_partial, event.saved, event.failed)
        else -> context.getString(R.string.download_batch_saved, event.saved)
    }
    is PlayerEvent.PlayFailed -> {
        val reason = event.reason.ifBlank { context.getString(R.string.playback_failed) }
        when {
            event.several -> context.getString(R.string.playback_stopped)
            event.skipping -> reason + "\n" + context.getString(R.string.playback_skipping)
            else -> reason
        }
    }
    PlayerEvent.NoInternet -> context.getString(R.string.no_internet_title)
    PlayerEvent.RadioEmpty -> context.getString(R.string.radio_failed)
    PlayerEvent.ConnectFailed -> context.getString(R.string.connect_failed)
}

/**
 * Owns the play queue and polls the engine into [state] and [progress]: it starts the next
 * row when the engine reports a finished track, and mirrors the queue into the engine so
 * remotes see it.
 */
class PlayerController(private val scope: CoroutineScope) {

    private val _state = MutableStateFlow(PlayerState())
    val state: StateFlow<PlayerState> = _state.asStateFlow()

    private val _progress = MutableStateFlow(Progress())
    val progress: StateFlow<Progress> = _progress.asStateFlow()

    // Buffered so an emit from the player's own scope never waits for a collector.
    private val _events = MutableSharedFlow<PlayerEvent>(extraBufferCapacity = 8)
    val events: SharedFlow<PlayerEvent> = _events.asSharedFlow()

    // True once Deezer refused the account's login since it signed in.
    private val _sessionExpired = MutableStateFlow(false)
    val sessionExpired: StateFlow<Boolean> = _sessionExpired.asStateFlow()

    // Set at sign-in: downloads and offline copies are for paid plans.
    var premium: Boolean = false

    // Asked before local playback starts or resumes; set by the Application.
    var focus: AudioFocus? = null

    private val local get() = snap.connectedDevice.isBlank()

    private var queue: List<QueueEntry> = emptyList()
    // The queue in play order while shuffle is on, so turning it off restores it.
    private var natural: List<QueueEntry>? = null
    private var index: Int = -1
    // The row after the tracks added with Add to queue since the list was played: the next
    // addition goes there, ahead of the rest of the list. -1 for none.
    private var addedEnd = -1
    private var nextUid = 0L
    private var lastFinished: Int = Engine.finishedCount()
    private var pollJob: Job? = null
    private var ticks = 0
    private var repeatMode: Int = 0
    private var shuffleEnabled: Boolean = false
    // While a start is in flight the poll must not advance on the previous track's finish.
    private var startGen: Int = 0
    private var startInFlight: Boolean = false
    // The start that began playing, and the one whose error was handled, so an error is
    // handled once and only for the play it belongs to.
    private var playOkGen = -1
    private var errHandledGen = -1
    private var playFailures = 0
    // A row stopped by a lost network starts again where it was.
    private var resumeUid = -1L
    private var resumeMs = 0L
    // The next row armed engine-side with Engine.preload for a gapless swap, null when none.
    private var preloadedId: String? = null

    // Engine queue mirror: edits run one at a time, in order; while any is in flight the
    // poll does not take the engine's queue for a remote's edit.
    private val engineEdits = Mutex()
    private var engineEditsInFlight = 0
    private var mirrored = false
    @Volatile private var lastEngineQueueVersion: Long = Engine.queueVersion()
    private var syncedIndex = Int.MIN_VALUE

    // >0 while a local repeat or shuffle change travels to the engine, so the poll does not
    // take the engine's old value meanwhile.
    private var modeSendInFlight: Int = 0

    // Liked track ids, so hearts render without a lookup per track.
    private val _likedIds = MutableStateFlow<Set<String>>(emptySet())
    val likedIds: StateFlow<Set<String>> = _likedIds.asStateFlow()

    // A failed Like toggle was reverted.
    private val _favoriteFailures = MutableSharedFlow<Unit>(extraBufferCapacity = 4)
    val favoriteFailures: SharedFlow<Unit> = _favoriteFailures.asSharedFlow()

    // Tracks the account can play offline, from the engine.
    private val _offlineIds = MutableStateFlow<Set<String>>(emptySet())
    val offlineIds: StateFlow<Set<String>> = _offlineIds.asStateFlow()
    private var offlineWatch: Job? = null

    // The last ElsewhereJSON read, and the ElsewhereVersion it was read at.
    private var elsewhereVersion = -1L
    private var elsewhere: Elsewhere? = null

    fun start() {
        if (pollJob?.isActive == true) return
        lastFinished = Engine.finishedCount()
        _sessionExpired.value = false
        refreshLikedIds()
        refreshOfflineIds()
        pollJob = scope.launch {
            // A Connect route or host switched on wakes a sleeping poll.
            launch { Engine.connectActive.collect { wakePoll() } }
            // A command's wake, every [SLOW_EVERY]th read and every idle one read the slow fields too.
            var woke = true
            while (isActive) {
                val prev = snap
                val takeQueue = mirrored && engineEditsInFlight == 0
                val slow = woke || ticks++ % SLOW_EVERY == 0 || pollDelay() == POLL_IDLE_MS
                val gen = opGen
                snap = withContext(Dispatchers.Default) { read(slow, prev, takeQueue, gen) }
                poll(snap)
                woke = if (canSleep()) {
                    wake.receive()
                    true
                } else {
                    withTimeoutOrNull(pollDelay()) { wake.receive() } != null
                }
            }
        }
    }

    fun stop() {
        pollJob?.cancel()
        pollJob = null
    }

    // ---- queue ----

    // The list each track was queued from, for Deezer's listen log. A track without one
    // (the engine added it, e.g. to keep Flow going) takes the list the queue was played from.
    private val trackContexts = java.util.WeakHashMap<Track, ListenContext>()
    private var queueContext = ListenContext.NONE

    private fun entries(tracks: List<Track>) = tracks.map { QueueEntry(nextUid++, it) }

    /** Plays [tracks] from [startIndex]; [shuffle] also sets the mode, sent with the queue once. */
    fun playQueue(tracks: List<Track>, startIndex: Int, context: ListenContext, startMs: Long = 0L, shuffle: Boolean? = null) {
        if (tracks.isEmpty()) return
        if (shuffle != null && shuffle != shuffleEnabled) {
            shuffleEnabled = shuffle
            sendMode { Engine.setShuffle(if (shuffle) 1 else 0) }
        }
        queueContext = context
        tracks.forEach { trackContexts[it] = context }
        val list = entries(tracks)
        val start = startIndex.coerceIn(0, list.lastIndex)
        if (shuffleEnabled) {
            natural = list
            queue = shuffledWithFirst(list, list[start])
            index = 0
        } else {
            natural = null
            queue = list
            index = start
        }
        playFailures = 0
        addedEnd = -1
        if (startMs > 0) {
            resumeUid = queue[index].uid
            resumeMs = startMs
        }
        syncEngineQueue()
        startCurrent()
    }

    fun playSingle(track: Track, context: ListenContext) = playQueue(listOf(track), 0, context)

    private fun shuffledWithFirst(list: List<QueueEntry>, first: QueueEntry?): List<QueueEntry> {
        val rest = list.filter { it !== first }.shuffled()
        return if (first == null) rest else listOf(first) + rest
    }

    fun next() {
        when {
            index + 1 < queue.size -> index++
            repeatMode == 1 && queue.size > 1 -> index = 0
            else -> return
        }
        startCurrent()
    }

    fun prev() {
        // From 5 s in, Previous restarts the track; while a start is in flight the position is
        // the outgoing track's, so it steps back.
        if (!startInFlight && _progress.value.positionMs >= 5000L) {
            control { Engine.seek(0) }
            return
        }
        when {
            index > 0 -> index--
            repeatMode == 1 && queue.size > 1 -> index = queue.lastIndex
            else -> {
                control { Engine.seek(0) }
                return
            }
        }
        startCurrent()
    }

    fun jumpTo(uid: Long) {
        val i = queue.indexOfFirst { it.uid == uid }
        if (i < 0 || i == index) return
        index = i
        startCurrent()
    }

    /** Inserts [tracks] right after the playing row; with nothing queued they play. */
    fun playNext(tracks: List<Track>, context: ListenContext) {
        if (tracks.isEmpty() || playIfIdle(tracks, context)) return
        val at = index + 1
        if (addedEnd >= at) addedEnd += tracks.size
        insert(at, tracks, context, natural?.let { n -> n.indexOfFirst { it.uid == queue[index].uid } + 1 } ?: 0)
    }

    /** Adds [tracks] after the tracks added before them; with nothing queued they play. */
    fun addToQueue(tracks: List<Track>, context: ListenContext) {
        if (tracks.isEmpty() || playIfIdle(tracks, context)) return
        val at = addedEnd.coerceIn(index + 1, queue.size)
        addedEnd = at + tracks.size
        insert(at, tracks, context, natural?.size ?: 0)
    }

    private fun playIfIdle(tracks: List<Track>, context: ListenContext): Boolean {
        if (index in queue.indices) return false
        playQueue(tracks, 0, context)
        return true
    }

    private fun insert(at: Int, tracks: List<Track>, context: ListenContext, naturalAt: Int) {
        tracks.forEach { trackContexts.getOrPut(it) { context } }
        val added = entries(tracks)
        queue = queue.toMutableList().apply { addAll(at, added) }
        natural = natural?.toMutableList()?.apply { addAll(naturalAt.coerceIn(0, size), added) }
        when (at) {
            index + 1 -> editEngineQueue { Engine.queueInsertNext(queueJson(tracks)) }
            queue.size - tracks.size -> editEngineQueue { Engine.queueAppend(queueJson(tracks)) }
            else -> syncEngineQueue()
        }
        reconcilePreload()
        publish()
    }

    /** Moves the row at [from] to [to]; the playing row keeps playing. */
    fun moveInQueue(from: Int, to: Int) {
        if (from == to || from !in queue.indices || to !in queue.indices) return
        val list = queue.toMutableList()
        list.add(to, list.removeAt(from))
        queue = list
        index = when {
            from == index -> to
            from < index && to >= index -> index - 1
            from > index && to <= index -> index + 1
            else -> index
        }
        if (from < addedEnd && to >= addedEnd) addedEnd--
        else if (from >= addedEnd && to < addedEnd && addedEnd >= 0) addedEnd++
        editEngineQueue { Engine.queueMove(from, to) }
        reconcilePreload()
        publish()
    }

    /** Removes a row; removing the playing row starts the one that takes its place. */
    fun removeFromQueue(uid: Long) {
        val i = queue.indexOfFirst { it.uid == uid }
        if (i < 0) return
        val wasCurrent = i == index
        queue = queue.filterNot { it.uid == uid }
        natural = natural?.filterNot { it.uid == uid }
        if (i < addedEnd) addedEnd--
        index = when {
            queue.isEmpty() -> -1
            wasCurrent && index >= queue.size -> queue.lastIndex
            !wasCurrent && i < index -> index - 1
            else -> index
        }
        if (!wasCurrent) {
            editEngineQueue { Engine.queueRemove(i) }
            reconcilePreload()
            publish()
            return
        }
        syncEngineQueue()
        if (index >= 0) startCurrent() else stopPlayback()
    }

    /** Clears the queue but the playing row. */
    fun clearQueue() {
        if (queue.isEmpty()) return
        queue = listOfNotNull(queue.getOrNull(index))
        index = queue.lastIndex
        addedEnd = -1
        natural = if (shuffleEnabled) queue else null
        syncEngineQueue()
        reconcilePreload()
        publish()
    }

    // ---- transport ----

    private val stoppedOrErrored get() = _state.value.state.let { it == Engine.STOPPED || it == Engine.ERROR }

    // The engine's toggle only flips playing and paused: from stopped or errored the row starts.
    fun togglePause() {
        if (_state.value.isPlaying) pause() else resume()
    }

    // A pause while a start resolves: the engine's play clears it, so it is made again after.
    private var pauseAfterStart = false

    fun pause() {
        if (startInFlight) pauseAfterStart = true
        control { Engine.pause() }
        focus?.abandon()
        save()
    }

    fun resume() {
        pauseAfterStart = false
        if (stoppedOrErrored && index in queue.indices && local) startCurrent()
        else if (!local || focus?.request() != false) control { Engine.resume() }
    }

    fun seek(ms: Long) = control { Engine.seek(ms) }

    fun setVolume(v: Double) = control { Engine.setVolume(v.coerceIn(0.0, 1.0)) }

    /** Stops playing and keeps the queue where it is: a play starts its row again. */
    fun stopKeepingQueue() {
        preloadedId = null
        control { Engine.stop() }
        focus?.abandon()
        scope.launch { Engine.clearPreload() }
        save()
        publish()
    }

    /** At sign-out: no queue, and none kept for the next start. */
    fun forgetQueue() {
        stopPlayback()
        queueOwner = ""
    }

    // An empty queue, also for the next start.
    private fun stopPlayback() {
        saveJob?.cancel()
        queueFile?.let { file -> scope.launch(Dispatchers.IO) { queueWrites.withLock { file.delete() } } }
        queue = emptyList()
        natural = null
        index = -1
        addedEnd = -1
        preloadedId = null
        control { Engine.stop() }
        focus?.abandon()
        scope.launch { Engine.clearPreload() }
        publish()
    }

    fun setRepeat(mode: Int) {
        repeatMode = mode.coerceIn(0, 2)
        sendMode { Engine.setRepeat(repeatMode) }
        reconcilePreload()
    }

    // The engine only records the flag, for remotes; the order is the app's.
    fun setShuffle(on: Boolean) {
        if (on == shuffleEnabled) return
        sendMode { Engine.setShuffle(if (on) 1 else 0) }
        applyShuffle(on)
    }

    // Reorders the queue for the flag; the poll calls this alone for a remote's change.
    private fun applyShuffle(on: Boolean) {
        shuffleEnabled = on
        addedEnd = -1
        val cur = queue.getOrNull(index)
        if (on) {
            natural = queue
            queue = shuffledWithFirst(queue, cur)
            index = if (cur != null) 0 else -1
        } else {
            queue = natural ?: queue
            natural = null
            index = if (cur != null) queue.indexOfFirst { it.uid == cur.uid } else -1
            if (cur != null && index < 0) {
                queue = listOf(cur) + queue
                index = 0
            }
        }
        if (queue.isNotEmpty()) {
            syncEngineQueue()
            reconcilePreload()
        }
        publish()
    }

    // Sends a repeat or shuffle change off the main thread (a Connect device makes it an HTTP call).
    private fun sendMode(block: () -> Unit) {
        opGen++
        modeSendInFlight++
        scope.launch {
            withContext(Dispatchers.IO) { block() }
            modeSendInFlight--
            wakePoll()
        }
    }

    // Engine calls that become HTTP while a Connect device is selected run off the main thread.
    private fun control(block: () -> Unit) {
        scope.launch {
            withContext(Dispatchers.IO) { block() }
            wakePoll()
        }
    }

    // ---- downloads, offline, radio, Connect ----

    /** Saves [track] to the download folder; the outcome is an event. */
    fun download(track: Track) {
        if (track.isEpisode) return
        scope.launch {
            _events.emit(PlayerEvent.DownloadStarted(track.name))
            val o = runCatching { JSONObject(Engine.download(track.id, "")) }.getOrNull()
            val err = o?.optString("error").orEmpty()
            _events.emit(
                if (o == null || err.isNotBlank()) PlayerEvent.DownloadFailed(track.name, err)
                else PlayerEvent.DownloadSaved(track.name, o.optString("path")),
            )
        }
    }

    /** Downloads a whole album to the download folder. */
    fun downloadAlbum(id: String, name: String) = downloadBatch(name) { Engine.downloadAlbum(id) }

    /** Downloads a whole playlist to the download folder. */
    fun downloadPlaylist(id: String, name: String) = downloadBatch(name) { Engine.downloadPlaylist(id) }

    private fun downloadBatch(name: String, call: suspend () -> String) {
        scope.launch {
            _events.emit(PlayerEvent.DownloadStarted(name))
            val o = runCatching { JSONObject(call()) }.getOrNull()
            _events.emit(
                if (o == null) PlayerEvent.DownloadFailed(name, "")
                else PlayerEvent.BatchDone(name, o.optInt("saved"), o.optInt("failed"), o.optString("error")),
            )
        }
    }

    /** Pins [track] for offline play; the engine fetches it in the background. */
    fun downloadForOffline(track: Track) {
        if (track.isEpisode) return
        scope.launch {
            val err = Engine.pinOffline("track", track.id)
            if (err.isNotBlank()) {
                _events.emit(PlayerEvent.OfflineFailed(track.name, err))
                return@launch
            }
            _events.emit(PlayerEvent.DownloadStarted(track.name))
            watchOffline()
        }
    }

    /** Pins or unpins a whole album or playlist; true when the engine took it. */
    suspend fun pinCollection(kind: String, id: String, name: String, on: Boolean): Boolean {
        val err = if (on) Engine.pinOffline(kind, id) else Engine.unpinOffline(kind, id)
        if (err.isNotEmpty()) {
            _events.emit(PlayerEvent.OfflineFailed(name, err))
            return false
        }
        if (on) {
            _events.emit(PlayerEvent.DownloadStarted(name))
            watchOffline()
        } else {
            refreshOfflineIds()
        }
        return true
    }

    /** Reads the tracks the account can play offline. */
    fun refreshOfflineIds() {
        scope.launch { Engine.offlineIds()?.let { _offlineIds.value = it } }
    }

    // While fetches run, the marks follow each fetched track.
    private fun watchOffline() {
        if (offlineWatch?.isActive == true) return
        offlineWatch = scope.launch {
            var done = -1
            while (isActive) {
                val p = Engine.offlineProgress() ?: break
                if (p.done != done) {
                    done = p.done
                    Engine.offlineIds()?.let { _offlineIds.value = it }
                }
                if (!p.active) break
                delay(OFFLINE_POLL_MS)
            }
        }
    }

    /** Plays a radio seeded from [track]. */
    fun startTrackRadio(track: Track) = startRadio(ListenContext("track_mix_contextual", track.id)) { Engine.trackMix(track.id) }

    /** Plays a radio seeded from an artist. */
    fun startArtistRadio(artistId: String) = startRadio(ListenContext("artist_smartradio", artistId)) { Engine.artistMix(artistId) }

    /**
     * Flow, or one of its moods ([mood] its id): one tap starts a fresh, endless stream (the engine
     * tops it up); on the Flow or mood already playing, the tap pauses or resumes it.
     */
    fun playFlow(mood: String = "") {
        val context = flowContext(mood)
        if (_state.value.current != null && playingFrom() == context) togglePause()
        else startRadio(context) { Engine.flow(mood) }
    }

    fun flowContext(mood: String = "") =
        if (mood.isEmpty()) ListenContext("dynamic_page_user_radio", Engine.userId()) else ListenContext("dynamic_page_flow_config", mood)

    /** The list the current track is played from. */
    fun playingFrom(): ListenContext = _state.value.current?.let { trackContexts[it] } ?: queueContext

    private fun startRadio(context: ListenContext, mix: suspend () -> List<Track>) {
        scope.launch {
            val tracks = mix()
            if (tracks.isEmpty()) _events.emit(PlayerEvent.RadioEmpty) else playQueue(tracks, 0, context)
        }
    }

    /** Routes playback to a Connect device; a refusal is an event. */
    suspend fun connectDevice(addr: String): Boolean {
        val ok = Engine.connectDevice(addr)
        if (!ok) _events.emit(PlayerEvent.ConnectFailed)
        wakePoll()
        return ok
    }

    // ---- favorites ----

    /** Reads the liked ids (sign-in, and after a toggle). */
    private fun refreshLikedIds() {
        scope.launch { Engine.favoriteIds()?.let { _likedIds.value = it } }
    }

    /** Flips the heart at once, then reverts it with an event when Deezer refuses. */
    fun toggleFavorite(track: Track) {
        if (track.isEpisode) return
        val id = track.id
        val wasLiked = _likedIds.value.contains(id)
        _likedIds.value = if (wasLiked) _likedIds.value - id else _likedIds.value + id
        scope.launch {
            val ok = if (wasLiked) Engine.removeFavorite(id) else Engine.addFavorite(id)
            if (ok) {
                // The kept Liked songs list is stale now.
                Library.drop("liked:")
                Engine.favoriteIds()?.let { _likedIds.value = it }
            } else {
                _likedIds.value = if (wasLiked) _likedIds.value + id else _likedIds.value - id
                _favoriteFailures.emit(Unit)
            }
        }
    }

    // ---- starting a row ----

    private fun startCurrent() {
        val entry = queue.getOrNull(index) ?: return
        if (entry.track.unavailable) {
            skipUnavailable()
            return
        }
        if (local && focus?.request() == false) return
        val t = entry.track
        val gen = ++startGen
        opGen++
        wakePoll()
        startInFlight = true
        pauseAfterStart = false
        preloadedId = null
        val resume = if (entry.uid == resumeUid) resumeMs else 0L
        resumeUid = -1L
        val context = trackContexts[t] ?: queueContext
        syncEngineIndex()
        scope.launch {
            Engine.setListenContext(context)
            val r = if (t.isEpisode) Engine.playEpisode(t.id, t.durationMs) else Engine.play(t.id, t.durationMs)
            if (gen != startGen) return@launch
            startInFlight = false
            // Reads begun before this point still show the old stream.
            startedGen = ++opGen
            wakePoll()
            when (r) {
                Engine.PLAY_FAILED -> onPlayFailed(Engine.lastError())
                Engine.PLAY_SUPERSEDED -> playFailures = 0
                else -> {
                    playOkGen = gen
                    playFailures = 0
                    lastFinished = Engine.finishedCount()
                    if (resume > 0) withContext(Dispatchers.IO) { Engine.seek(resume) }
                    if (pauseAfterStart) {
                        pauseAfterStart = false
                        withContext(Dispatchers.IO) { Engine.pause() }
                    }
                    armPreload()
                }
            }
            publish()
        }
        publish()
    }

    // A lost network stops on the row and keeps its position; another failure skips to the
    // next row, and stops after a few in a row.
    private fun onPlayFailed(err: LastError) {
        errHandledGen = startGen
        if (err.network) {
            val entry = queue.getOrNull(index)
            // Only a row that had started has a position of its own.
            val at = if (playOkGen == startGen) _progress.value.positionMs else 0L
            if (entry != null && !entry.track.isEpisode && at > 0) {
                resumeUid = entry.uid
                resumeMs = at
            }
            control { Engine.stop() }
            _events.tryEmit(PlayerEvent.NoInternet)
            return
        }
        playFailures++
        val canSkip = index + 1 < queue.size || (repeatMode == 1 && queue.size > 1)
        if (playFailures >= PLAY_FAILURES_BEFORE_STOP || !canSkip) {
            playFailures = 0
            control { Engine.stop() }
            _events.tryEmit(PlayerEvent.PlayFailed(err.error, skipping = false, several = canSkip))
            return
        }
        _events.tryEmit(PlayerEvent.PlayFailed(err.error, skipping = true))
        next()
    }

    // ---- the queue kept across launches ----

    // Where the queue is kept; set by the Application.
    var queueFile: File? = null
    // The account whose queue is kept: "" until one is restored.
    private var queueOwner = ""
    private var savedKey: List<Any?>? = null
    private var saveJob: Job? = null
    private val queueWrites = Mutex()

    /** The queue the last launch kept, from its file; null when there is none. */
    suspend fun savedQueue(): SavedQueue? {
        val file = queueFile ?: return null
        return withContext(Dispatchers.IO) { runCatching { SavedQueue.decode(file.readText()) }.getOrNull() }
            ?.takeIf { it.userId.isNotEmpty() && it.index in it.rows.indices }
    }


    /** Restores the account's saved queue when nothing is queued yet; from then on it is kept. */
    fun restore(userId: String) {
        if (userId.isEmpty() || queueOwner == userId) return
        queueOwner = userId
        if (queueFile == null || queue.isNotEmpty()) return
        scope.launch {
            val saved = savedQueue()
            if (saved == null || saved.userId != userId || queue.isNotEmpty()) return@launch
            val list = saved.rows.map { (t, ctx) ->
                trackContexts[t] = ctx
                QueueEntry(nextUid++, t)
            }
            queue = list
            index = saved.index
            shuffleEnabled = saved.shuffle
            natural = saved.natural?.takeIf { saved.shuffle && it.size == list.size && it.all { i -> i in list.indices } }?.map { list[it] }
            if (saved.shuffle) sendMode { Engine.setShuffle(1) }
            if (saved.repeat != repeatMode) setRepeat(saved.repeat)
            // The first play starts where the row was left; an episode at its bookmark.
            val cur = list[index]
            if (!cur.track.isEpisode && saved.positionMs > 0) {
                resumeUid = cur.uid
                resumeMs = saved.positionMs
            }
            syncEngineQueue()
            publish()
        }
    }

    // A change of rows, cursor, modes or state is kept a moment later.
    private fun saveSoon() {
        if (queueOwner.isEmpty()) return
        saveJob?.cancel()
        saveJob = scope.launch {
            delay(QUEUE_SAVE_MS)
            save()
        }
    }

    /** Writes the queue now; also as the app goes to the background, for the position. */
    fun save() {
        val file = queueFile ?: return
        // An empty queue is never written: the file is the next start's, until a sign-out.
        if (queueOwner.isEmpty() || !local || queue.isEmpty()) return
        val entry = queue.getOrNull(index)
        val position = if (entry != null && entry.uid == resumeUid) resumeMs else _progress.value.positionMs
        val rowOf = queue.withIndex().associate { it.value.uid to it.index }
        val saved = SavedQueue(
            queueOwner, queue.map { it.track to (trackContexts[it.track] ?: queueContext) }, index, position,
            shuffleEnabled, repeatMode, natural?.map { rowOf[it.uid] ?: 0 },
        )
        scope.launch(Dispatchers.IO) {
            val text = saved.encode()
            queueWrites.withLock {
                runCatching {
                    val tmp = File(file.path + ".tmp")
                    tmp.writeText(text)
                    tmp.renameTo(file)
                }
            }
        }
    }

    /**
     * Plays here what another Purple Disc of the account on the network plays: its whole queue,
     * from where it stands. False when no such host plays [track] (the caller then plays the
     * track alone).
     */
    suspend fun takeOver(track: Track): Boolean {
        val o = withContext(Dispatchers.IO) { Engine.takeOverJson(track.id) } ?: return false
        val tracks = Json.tracks(o)
        if (tracks.isEmpty()) return false
        val i = o.optInt("index").coerceIn(0, tracks.lastIndex)
        setRepeat(when (o.optString("repeat")) { "all" -> 1; "one" -> 2; else -> 0 })
        if (shuffleEnabled) setShuffle(false)
        playQueue(tracks, i, ListenContext.NONE, startMs = o.optLong("positionMs"))
        return true
    }

    // A track Deezer streams nowhere is stepped over without a try, as Deezer's players grey
    // it out; with nothing playable ahead, playback stops and says why.
    private fun skipUnavailable() {
        val ahead = (1 until queue.size).map { (index + it) % queue.size }.filter { repeatMode == 1 || it > index }
        val i = ahead.firstOrNull { !queue[it].track.unavailable }
        if (i == null) {
            control { Engine.stop() }
            _events.tryEmit(PlayerEvent.Unavailable)
            publish()
            return
        }
        index = i
        startCurrent()
    }

    // ---- gapless preload ----

    // The row the queue goes to when the current one ends, or null when it is not known.
    private fun deterministicNextIndex(): Int? = when {
        queue.isEmpty() || repeatMode == 2 -> null
        index + 1 < queue.size -> index + 1
        repeatMode == 1 -> 0
        else -> null
    }

    // The track worth preloading: only when the engine swaps seamlessly (gapless or crossfade),
    // never around episodes or while a Connect device plays.
    private fun nextTrackForPreload(): Track? {
        if (!Engine.gapless() && Engine.crossfadeMs() == 0) return null
        if (queue.getOrNull(index)?.track?.isEpisode == true) return null
        if (Engine.connectedDevice().isNotBlank()) return null
        val next = queue.getOrNull(deterministicNextIndex() ?: return null)?.track ?: return null
        return if (next.isEpisode || next.unavailable) null else next
    }

    private suspend fun armPreload() {
        val next = nextTrackForPreload()
        if (next != null) {
            if (next.id == preloadedId) return
            preloadedId = next.id
            // A skip meanwhile may arm another id; clear only when this one is still armed.
            if (!Engine.preload(next.id) && preloadedId == next.id) preloadedId = null
        } else if (preloadedId != null) {
            preloadedId = null
            Engine.clearPreload()
        }
    }

    private fun reconcilePreload() {
        scope.launch { armPreload() }
    }

    // ---- engine queue mirror ----

    // Runs [edit] on the engine queue after the edits before it, unless the queue holds an
    // episode (those queues are not mirrored); the first edit after one resends the queue.
    private fun editEngineQueue(edit: suspend () -> Unit) {
        if (queue.any { it.track.isEpisode }) {
            mirrored = false
            return
        }
        if (!mirrored) {
            syncEngineQueue()
            return
        }
        runEdit(edit)
    }

    private fun syncEngineQueue() {
        if (queue.any { it.track.isEpisode }) {
            mirrored = false
            return
        }
        mirrored = true
        val tracks = queue.map { it.track }
        val i = index
        syncedIndex = i
        runEdit {
            Engine.setQueueJson(queueJson(tracks))
            Engine.setQueueIndex(i)
        }
    }

    private fun syncEngineIndex() {
        if (!mirrored || index == syncedIndex) return
        val i = index
        syncedIndex = i
        runEdit { Engine.setQueueIndex(i) }
    }

    private fun runEdit(edit: suspend () -> Unit) {
        opGen++
        engineEditsInFlight++
        scope.launch {
            engineEdits.withLock {
                edit()
                lastEngineQueueVersion = Engine.queueVersion()
            }
            engineEditsInFlight--
        }
    }

    // A remote controller edited this device's engine queue (the read fetched it): take it,
    // and start its current row unless the engine already plays it. True when it took over.
    private fun adoptEngineQueue(s: Snap): Boolean {
        val q = s.engineQueue ?: return false
        if (engineEditsInFlight > 0 || q.version <= lastEngineQueueVersion) return false
        lastEngineQueueVersion = q.version
        if (q.tracks.isEmpty()) return false
        queue = entries(q.tracks)
        natural = if (shuffleEnabled) queue else null
        index = q.index.coerceIn(0, queue.lastIndex)
        syncedIndex = index
        addedEnd = -1
        preloadedId = null
        val live = s.state == Engine.PLAYING || s.state == Engine.LOADING
        if (s.nowPlaying?.id == queue[index].track.id && live) {
            lastFinished = s.finished
            reconcilePreload()
            publish()
        } else {
            startCurrent()
        }
        return true
    }

    // ---- poll ----

    // One read of the engine, taken off the main thread and applied on it.
    private class Snap(
        val state: Int = Engine.STOPPED,
        val positionMs: Long = 0L,
        val durationMs: Long = 0L,
        val finished: Int = 0,
        val connectedDevice: String = "",
        val volume: Double = 1.0,
        val format: String = "",
        val repeat: Int = 0,
        val shuffle: Boolean = false,
        val nowPlaying: Track? = null,
        val elsewhere: Elsewhere? = null,
        val engineQueue: QueueSnapshot? = null,
        val preview: Boolean = false,
        val expired: Boolean = false,
        // The engine queue's cursor, which a remote's next or previous moves.
        val queueIndex: Int = -1,
        // [opGen] when the read began: what the app did since is newer than this read.
        val opGen: Int = 0,
    )

    @Volatile private var snap = Snap()

    // Bumped by every engine edit, mode change and start, on the main thread.
    private var opGen = 0

    // [opGen] when the last start completed.
    private var startedGen = 0

    // Only the read touches these: now-playing and elsewhere are parsed when their version moved.
    private var readNowVersion = -1L
    private var readNow: Track? = null
    private var readAwayVersion = -1L
    private var readAway: Elsewhere? = null

    // The slow fields (volume, format, modes, preview, session state) are read on [slow] reads.
    private fun read(slow: Boolean, prev: Snap, takeQueue: Boolean, gen: Int): Snap {
        // The finish count before the state: a finish between the two reads is not taken for
        // a swap.
        val finished = Engine.finishedCount()
        val state = Engine.state()
        val nv = Engine.nowPlayingVersion()
        if (nv != readNowVersion) {
            readNowVersion = nv
            readNow = Engine.nowPlaying()
        }
        val av = Engine.elsewhereVersion()
        if (av != readAwayVersion) {
            readAwayVersion = av
            readAway = Engine.elsewhere()
        }
        val queue = if (takeQueue && Engine.queueVersion() != lastEngineQueueVersion) Engine.queueSnapshot() else null
        return Snap(
            state = state,
            positionMs = Engine.positionMs(),
            durationMs = Engine.durationMs(),
            finished = finished,
            connectedDevice = Engine.connectedDevice(),
            volume = if (slow) Engine.volume() else prev.volume,
            format = if (slow) Engine.format() else prev.format,
            repeat = if (slow) Engine.repeatMode() else prev.repeat,
            shuffle = if (slow) Engine.shuffleOn() else prev.shuffle,
            nowPlaying = readNow,
            elsewhere = readAway,
            engineQueue = queue,
            preview = if (slow) Engine.isPreview() else prev.preview,
            expired = slow && Engine.sessionExpired(),
            queueIndex = Engine.queueIndex(),
            opGen = gen,
        )
    }

    // Fast while playing on screen, slower hidden or loading, slowest when idle (the
    // Windows rates); a user action wakes it at once.
    private fun pollDelay(): Long {
        val visible = ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)
        return when {
            snap.state == Engine.PLAYING && visible -> POLL_FAST_MS
            snap.state == Engine.PLAYING || snap.state == Engine.LOADING || startInFlight -> POLL_SLOW_MS
            else -> POLL_IDLE_MS
        }
    }

    // Nothing plays or loads, the app is hidden and no Connect route, host or phone remote is
    // on: nothing the poll would show changes until an action, a return to the foreground or a
    // Connect change wakes it.
    private fun canSleep(): Boolean {
        val s = snap
        if (s.state == Engine.PLAYING || s.state == Engine.LOADING || startInFlight || s.connectedDevice.isNotBlank()) return false
        if (ProcessLifecycleOwner.get().lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) return false
        return !Engine.connectActive.value && Engine.webRemoteInfo()?.enabled != true
    }

    /** The app came back to the foreground. */
    fun wakeUp() = wakePoll()

    private val wake = Channel<Unit>(Channel.CONFLATED)

    private fun wakePoll() {
        wake.trySend(Unit)
    }

    private fun poll(s: Snap) {
        if (s.expired) _sessionExpired.value = true
        if (startInFlight) {
            publish()
            return
        }
        // The queue, the modes and an error are taken only from a read begun after the app's
        // last edit, mode change or start; an older one would undo them.
        val fresh = s.opGen == opGen
        // Repeat and shuffle follow the engine, which a remote or a cast device may change.
        if (fresh && modeSendInFlight == 0) {
            if (s.repeat != repeatMode) {
                repeatMode = s.repeat
                reconcilePreload()
            }
            if (s.shuffle != shuffleEnabled) applyShuffle(s.shuffle)
        }
        if (fresh && adoptEngineQueue(s)) return
        // A remote's next or previous moved the engine's cursor to the row it now plays.
        val eidx = s.queueIndex
        if (fresh && mirrored && engineEditsInFlight == 0 && eidx != index && eidx in queue.indices &&
            queue[eidx].track.id == s.nowPlaying?.id
        ) {
            index = eidx
            syncedIndex = eidx
            preloadedId = null
            lastFinished = s.finished
            reconcilePreload()
            publish()
            return
        }
        // A stream that failed after it started, once per play.
        if (fresh && s.state == Engine.ERROR && s.connectedDevice.isBlank() && playOkGen == startGen && errHandledGen != startGen) {
            errHandledGen = startGen
            scope.launch { onPlayFailed(Engine.lastError()) }
            publish()
            return
        }
        if (s.finished > lastFinished) {
            lastFinished = s.finished
            // A preloaded next row the engine already swapped into: move the cursor without a
            // new play (it would cut the audio), then arm the next.
            val n = deterministicNextIndex()
            if (n != null && preloadedId != null && preloadedId == queue.getOrNull(n)?.track?.id && s.state == Engine.PLAYING) {
                index = n
                preloadedId = null
                syncEngineIndex()
                reconcilePreload()
                publish()
                return
            }
            when {
                repeatMode == 2 -> {}
                index + 1 < queue.size -> index++
                repeatMode == 1 && queue.isNotEmpty() -> index = 0
                else -> {
                    publish()
                    return
                }
            }
            startCurrent()
            return
        }
        publish()
    }

    // State from the last read and the app's queue; position and length go to [progress]
    // alone, so a position tick does not touch [state].
    private fun publish() {
        val s = snap
        val queueTrack = queue.getOrNull(index)?.track
        // A Connect device's track, or a local episode the engine fills in meanwhile, comes
        // from the engine's now-playing.
        val current = when {
            s.connectedDevice.isNotBlank() -> s.nowPlaying
            queueTrack?.isEpisode == true -> s.nowPlaying?.copy(isEpisode = true) ?: queueTrack
            else -> queueTrack ?: s.nowPlaying
        }
        // While a start resolves, and until a read begun after it, the engine's state is the old stream's.
        val starting = (startInFlight || s.opGen < startedGen) && queueTrack != null
        _state.value = PlayerState(
            current = current,
            state = if (starting) Engine.LOADING else s.state,
            volume = s.volume,
            format = s.format,
            queue = queue,
            index = index,
            connectedDevice = s.connectedDevice,
            repeatMode = repeatMode,
            shuffle = shuffleEnabled,
            elsewhere = s.elsewhere?.takeIf { System.currentTimeMillis() < it.untilMs },
            preview = s.preview,
        )
        // A restored or network-stopped row shows where its first play will start.
        val resumeAt = queue.getOrNull(index)?.takeIf { it.uid == resumeUid && s.state == Engine.STOPPED }
        _progress.value = when {
            starting -> Progress(0L, queueTrack.durationMs)
            resumeAt != null -> Progress(resumeMs, resumeAt.track.durationMs)
            else -> Progress(s.positionMs, s.durationMs)
        }
        // The rows and the modes; the position is written on pause, on stop and in the background.
        val key = listOf(System.identityHashCode(queue), index, shuffleEnabled, repeatMode)
        if (key != savedKey) {
            savedKey = key
            saveSoon()
        }
    }

    // The engine's list wire shape (only id is required; durationMs serves a remote's
    // end-of-track detection).
    private fun queueJson(tracks: List<Track>): String = JSONArray(tracks.map(Json::trackObject)).toString()

    companion object {
        private const val POLL_FAST_MS = 250L
        private const val POLL_SLOW_MS = 1000L
        private const val POLL_IDLE_MS = 5000L
        private const val SLOW_EVERY = 4
        private const val OFFLINE_POLL_MS = 2000L
        private const val PLAY_FAILURES_BEFORE_STOP = 3
        private const val QUEUE_SAVE_MS = 1000L
    }
}