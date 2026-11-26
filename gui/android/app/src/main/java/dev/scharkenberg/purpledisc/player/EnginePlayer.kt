package dev.scharkenberg.purpledisc.player

import android.net.Uri
import android.os.Looper
import androidx.annotation.OptIn
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.PlaybackParameters
import androidx.media3.common.Player
import androidx.media3.common.SimpleBasePlayer
import androidx.media3.common.util.UnstableApi
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.Track
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

// A position this far from where playback should be is a seek.
private const val SEEK_JUMP_MS = 2000L

// Previous within this much of a track's start goes to the previous row, as PlayerController.prev does.
private const val PREVIOUS_RESTARTS_AFTER_MS = 5000L

/**
 * The app's [PlayerController] as a Media3 [Player], for the media session: the playing row
 * between its neighbors (so next and previous are offered), the state, shuffle and repeat;
 * every command goes back to the controller. It is told again only when what it shows
 * changes, or the position jumps.
 */
@OptIn(UnstableApi::class)
class EnginePlayer(private val c: PlayerController) : SimpleBasePlayer(Looper.getMainLooper()) {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var st: PlayerState = c.state.value
    private var jumpTo: Long? = null
    private var expectedAt = 0L
    private var expectedPos = 0L
    private var shownSpeed = 1f

    init {
        scope.launch {
            c.state.map { s ->
                // The now-playing version: an episode's details arrive after it starts, under the same id.
                listOf(s.current?.id, s.state, s.index, s.queue.size, s.shuffle, s.repeatMode, s.hasNext, s.connectedDevice.isBlank(), Engine.nowPlayingVersion())
            }.distinctUntilChanged().collect {
                st = c.state.value
                mark(c.progress.value.positionMs)
                invalidateState()
            }
        }
        scope.launch {
            c.progress.collect { p ->
                // An episode's speed, so the lock screen's progress keeps pace.
                val speed = if (st.current?.isEpisode == true) Engine.playbackSpeed().toFloat() else 1f
                if (speed != shownSpeed) {
                    shownSpeed = speed
                    invalidateState()
                }
                // Against the last reading, so a long play builds up no drift.
                val now = android.os.SystemClock.elapsedRealtime()
                val expected = expectedPos + if (c.state.value.isPlaying) now - expectedAt else 0L
                val jumped = kotlin.math.abs(p.positionMs - expected) > SEEK_JUMP_MS
                mark(p.positionMs)
                if (jumped) {
                    jumpTo = p.positionMs
                    invalidateState()
                }
            }
        }
    }

    private fun mark(positionMs: Long) {
        expectedPos = positionMs
        expectedAt = android.os.SystemClock.elapsedRealtime()
    }

    override fun getState(): State {
        val s = st
        val cur = s.current
        val rows = mutableListOf<MediaItemData>()
        var at = 0
        if (cur != null) {
            // The neighbors stand in for the queue: the session only needs to know there is one.
            if (s.index > 0 || s.repeatMode == 1) rows += item("prev", s.queue.getOrNull(s.index - 1)?.track ?: cur, 0L)
            at = rows.size
            rows += item("cur", cur, c.progress.value.durationMs.takeIf { it > 0 } ?: cur.durationMs)
            if (s.hasNext) rows += item("next", s.queue.getOrNull(s.index + 1)?.track ?: s.queue.first().track, 0L)
        }
        val b = State.Builder()
            .setAvailableCommands(COMMANDS)
            .setPlayWhenReady(s.state == Engine.PLAYING || s.state == Engine.LOADING, Player.PLAY_WHEN_READY_CHANGE_REASON_USER_REQUEST)
            .setPlaybackState(
                // A row stopped (or restored from the last launch) is ready: play starts it,
                // also from a headset's play key.
                when {
                    cur == null -> Player.STATE_IDLE
                    s.state == Engine.LOADING -> Player.STATE_BUFFERING
                    else -> Player.STATE_READY
                },
            )
            .setShuffleModeEnabled(s.shuffle)
            .setRepeatMode(
                when (s.repeatMode) {
                    1 -> Player.REPEAT_MODE_ALL
                    2 -> Player.REPEAT_MODE_ONE
                    else -> Player.REPEAT_MODE_OFF
                },
            )
            .setMaxSeekToPreviousPositionMs(PREVIOUS_RESTARTS_AFTER_MS)
            .setPlaybackParameters(PlaybackParameters(shownSpeed))
            .setPlaylist(rows)
        if (rows.isNotEmpty()) {
            b.setCurrentMediaItemIndex(at).setContentPositionMs { c.progress.value.positionMs }
        }
        jumpTo?.let {
            b.setPositionDiscontinuity(Player.DISCONTINUITY_REASON_SEEK, it)
            jumpTo = null
        }
        return b.build()
    }

    private fun item(uid: String, t: Track, durationMs: Long): MediaItemData =
        MediaItemData.Builder("$uid:${t.id}")
            .setMediaItem(mediaItemOf(t))
            .setDurationUs(if (durationMs > 0) durationMs * 1000 else C.TIME_UNSET)
            .build()

    // Playback resumption hands back the row the controller already restored: nothing to set.
    override fun handleSetMediaItems(mediaItems: List<MediaItem>, startIndex: Int, startPositionMs: Long): ListenableFuture<*> =
        Futures.immediateVoidFuture()

    override fun handleSetPlayWhenReady(playWhenReady: Boolean): ListenableFuture<*> {
        if (playWhenReady) c.resume() else c.pause()
        return Futures.immediateVoidFuture()
    }

    // The controller starts a stopped row itself on play.
    override fun handlePrepare(): ListenableFuture<*> = Futures.immediateVoidFuture()

    override fun handleStop(): ListenableFuture<*> {
        c.stopKeepingQueue()
        return Futures.immediateVoidFuture()
    }

    override fun handleSeek(mediaItemIndex: Int, positionMs: Long, seekCommand: Int): ListenableFuture<*> {
        when (seekCommand) {
            Player.COMMAND_SEEK_TO_NEXT, Player.COMMAND_SEEK_TO_NEXT_MEDIA_ITEM -> c.next()
            Player.COMMAND_SEEK_TO_PREVIOUS, Player.COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM -> c.prev()
            else -> c.seek(positionMs.coerceAtLeast(0L))
        }
        return Futures.immediateVoidFuture()
    }

    override fun handleSetShuffleModeEnabled(shuffleModeEnabled: Boolean): ListenableFuture<*> {
        c.setShuffle(shuffleModeEnabled)
        return Futures.immediateVoidFuture()
    }

    override fun handleSetRepeatMode(repeatMode: Int): ListenableFuture<*> {
        c.setRepeat(
            when (repeatMode) {
                Player.REPEAT_MODE_ONE -> 2
                Player.REPEAT_MODE_ALL -> 1
                else -> 0
            },
        )
        return Futures.immediateVoidFuture()
    }

    override fun handleRelease(): ListenableFuture<*> {
        scope.cancel()
        return Futures.immediateVoidFuture()
    }

    private companion object {
        val COMMANDS: Player.Commands = Player.Commands.Builder().addAll(
            Player.COMMAND_PLAY_PAUSE,
            Player.COMMAND_PREPARE,
            Player.COMMAND_STOP,
            Player.COMMAND_SEEK_IN_CURRENT_MEDIA_ITEM,
            Player.COMMAND_SEEK_TO_NEXT,
            Player.COMMAND_SEEK_TO_NEXT_MEDIA_ITEM,
            Player.COMMAND_SEEK_TO_PREVIOUS,
            Player.COMMAND_SEEK_TO_PREVIOUS_MEDIA_ITEM,
            Player.COMMAND_SET_SHUFFLE_MODE,
            Player.COMMAND_SET_REPEAT_MODE,
            Player.COMMAND_GET_CURRENT_MEDIA_ITEM,
            Player.COMMAND_GET_TIMELINE,
            Player.COMMAND_GET_METADATA,
            Player.COMMAND_RELEASE,
            // Lets Media3 ask the session callback for playback resumption.
            Player.COMMAND_SET_MEDIA_ITEM,
        ).build()
    }
}

/** A track as the session shows it: title, artist, album and cover. */
fun mediaItemOf(t: Track): MediaItem {
    val meta = MediaMetadata.Builder()
        .setTitle(t.name)
        .setArtist(t.artistLine)
        .setAlbumTitle(t.albumName)
        .apply { if (t.artworkUrl.isNotBlank()) setArtworkUri(Uri.parse(t.artworkUrl)) }
        .build()
    return MediaItem.Builder().setMediaId(t.id).setMediaMetadata(meta).build()
}
