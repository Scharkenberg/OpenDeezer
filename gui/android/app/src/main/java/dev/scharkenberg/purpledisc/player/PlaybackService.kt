package dev.scharkenberg.purpledisc.player

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ServiceInfo
import android.media.AudioManager
import android.os.Bundle
import android.util.Log
import androidx.annotation.OptIn
import androidx.core.app.NotificationChannelCompat
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import androidx.media3.common.util.UnstableApi
import androidx.media3.session.CommandButton
import androidx.media3.session.DefaultMediaNotificationProvider
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import androidx.media3.session.SessionCommand
import androidx.media3.session.SessionResult
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture
import com.google.common.util.concurrent.SettableFuture
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.engine.Engine
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch

/**
 * The media session service (Media3): notification, lock screen, media buttons, Bluetooth
 * and the system's media controls, over [EnginePlayer]. The notification carries a heart for
 * the playing track, as Deezer's and Spotify's do. It keeps the in-process engine out of the
 * cached-app freezer while playing, and while Purple Disc Connect is on so a peer can start a
 * play; headphones unplugged pause playback.
 */
@OptIn(UnstableApi::class)
class PlaybackService : MediaSessionService() {

    private var session: MediaSession? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var noisyRegistered = false

    // Headphones unplugged (or a Bluetooth output gone): pause rather than play aloud.
    private val noisy = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            controller?.pause()
        }
    }

    override fun onCreate() {
        super.onCreate()
        running = true
        val c = checkNotNull(controller)
        setMediaNotificationProvider(
            DefaultMediaNotificationProvider.Builder(this).build().apply { setSmallIcon(R.drawable.ic_notification) },
        )
        val launch = packageManager.getLaunchIntentForPackage(packageName) ?: packageManager.getLeanbackLaunchIntentForPackage(packageName)
        val s = MediaSession.Builder(this, EnginePlayer(c))
            .setCallback(Callback(c))
            .apply {
                launch?.let { setSessionActivity(PendingIntent.getActivity(this@PlaybackService, 0, it, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)) }
            }
            .build()
        session = s
        // No controller of the app's own connects, so the service is told of the session
        // itself: that is what brings up the notification.
        addSession(s)
        // The heart follows the playing track and the account's likes; episodes have none.
        scope.launch {
            c.state.combine(c.likedIds) { st, liked -> st.current?.takeUnless { it.isEpisode }?.let { it.id in liked } }
                .distinctUntilChanged()
                .collect { liked ->
                    val buttons = heart(liked)
                    s.connectedControllers.filter(::trusted).forEach { s.setMediaButtonPreferences(it, buttons) }
                }
        }
        scope.launch {
            c.state.combine(Engine.connectActive) { st, _ -> st.isPlaying && st.connectedDevice.isBlank() }
                .distinctUntilChanged()
                .collect(::setNoisy)
        }
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession? = session

    // Media3 answers START_STICKY: a killed process would come back as an empty service.
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        return START_NOT_STICKY
    }

    // Connect on keeps the service in the foreground while nothing plays, so a peer's play
    // reaches a process that is not frozen.
    override fun onUpdateNotification(session: MediaSession, startInForegroundRequired: Boolean) {
        super.onUpdateNotification(session, startInForegroundRequired || Engine.connectActive.value)
    }

    // Swiped from recents: Media3's rule (the service ends unless it plays), except while
    // Connect is on. The queue stays, for the next start.
    override fun onTaskRemoved(rootIntent: Intent?) {
        if (!Engine.connectActive.value) super.onTaskRemoved(rootIntent)
    }

    override fun onDestroy() {
        running = false
        setNoisy(false)
        scope.cancel()
        session?.run {
            player.release()
            release()
        }
        session = null
        super.onDestroy()
    }

    // The notification and the system's media controls; Bluetooth and other apps get no heart.
    private fun trusted(controller: MediaSession.ControllerInfo) =
        controller.isTrusted || controller.packageName == packageName

    private fun heart(liked: Boolean?): List<CommandButton> = if (liked == null) {
        emptyList()
    } else {
        listOf(
            CommandButton.Builder(if (liked) CommandButton.ICON_HEART_FILLED else CommandButton.ICON_HEART_UNFILLED)
                .setDisplayName(getString(if (liked) R.string.menu_unlike else R.string.menu_like))
                .setSessionCommand(LIKE)
                .setSlots(CommandButton.SLOT_OVERFLOW)
                .build(),
        )
    }

    // A play key that found nothing to resume: the service, started in the foreground for it,
    // ends as Media3 ends one it cannot start (a silent notification, removed at once).
    private fun endUnstarted() {
        val channel = DefaultMediaNotificationProvider.DEFAULT_CHANNEL_ID
        NotificationManagerCompat.from(this).createNotificationChannel(
            NotificationChannelCompat.Builder(channel, NotificationManagerCompat.IMPORTANCE_LOW)
                .setName(getString(androidx.media3.session.R.string.default_notification_channel_name))
                .build(),
        )
        val n = NotificationCompat.Builder(this, channel)
            .setSmallIcon(R.drawable.ic_notification)
            .setVisibility(NotificationCompat.VISIBILITY_SECRET)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_DEFERRED)
            .build()
        try {
            ServiceCompat.startForeground(this, UNSTARTED_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK)
        } catch (e: IllegalStateException) {
            Log.w(TAG, "foreground start refused", e)
        }
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private inner class Callback(private val c: PlayerController) : MediaSession.Callback {
        override fun onConnect(session: MediaSession, controller: MediaSession.ControllerInfo): MediaSession.ConnectionResult {
            if (!trusted(controller)) return MediaSession.ConnectionResult.AcceptedResultBuilder(session).build()
            val liked = c.state.value.current?.takeUnless { it.isEpisode }?.let { it.id in c.likedIds.value }
            return MediaSession.ConnectionResult.AcceptedResultBuilder(session)
                .setAvailableSessionCommands(MediaSession.ConnectionResult.DEFAULT_SESSION_COMMANDS.buildUpon().add(LIKE).build())
                .setMediaButtonPreferences(heart(liked))
                .build()
        }

        // A play key while nothing is loaded (the app was not running): the last queue,
        // where it stopped. Only for playback: the system's resumption card needs a library
        // service, which this is not.
        override fun onPlaybackResumption(
            mediaSession: MediaSession,
            controller: MediaSession.ControllerInfo,
            isForPlayback: Boolean,
        ): ListenableFuture<MediaSession.MediaItemsWithStartPosition> {
            if (!isForPlayback) return Futures.immediateFailedFuture(UnsupportedOperationException())
            val result = SettableFuture.create<MediaSession.MediaItemsWithStartPosition>()
            scope.launch {
                // The sign-in blocks and cannot be interrupted: the wait for it can. Media3 posts
                // nothing until the queue is back, and the service must show one soon.
                val prepare = async { Resumption.prepare(applicationContext, c) }
                val r = withTimeoutOrNull(RESUME_LIMIT_MS) { prepare.await() }
                if (r == null) {
                    prepare.cancel()
                    result.setException(IllegalStateException("nothing to resume"))
                    if (!Engine.connectActive.value) endUnstarted()
                } else {
                    result.set(MediaSession.MediaItemsWithStartPosition(listOf(mediaItemOf(r.first)), 0, r.second))
                }
            }
            return result
        }

        override fun onCustomCommand(
            session: MediaSession,
            controller: MediaSession.ControllerInfo,
            customCommand: SessionCommand,
            args: Bundle,
        ): ListenableFuture<SessionResult> {
            if (customCommand.customAction == LIKE_ACTION && trusted(controller)) c.state.value.current?.let(c::toggleFavorite)
            return Futures.immediateFuture(SessionResult(SessionResult.RESULT_SUCCESS))
        }
    }

    private fun setNoisy(on: Boolean) {
        if (on == noisyRegistered) return
        noisyRegistered = on
        if (on) {
            ContextCompat.registerReceiver(this, noisy, IntentFilter(AudioManager.ACTION_AUDIO_BECOMING_NOISY), ContextCompat.RECEIVER_NOT_EXPORTED)
        } else {
            unregisterReceiver(noisy)
        }
    }

    companion object {
        /** Wired by the Application before the service is started. */
        var controller: PlayerController? = null

        /** Whether the service exists (Android may end an idle one). */
        @Volatile var running = false
            private set

        private const val LIKE_ACTION = "dev.scharkenberg.purpledisc.LIKE"
        private val LIKE = SessionCommand(LIKE_ACTION, Bundle.EMPTY)
        private const val UNSTARTED_ID = 1002
        private const val RESUME_LIMIT_MS = 4000L
        private const val TAG = "PurpleDisc"

        // Playing: in the foreground at once (it may be asked from the background); Connect
        // alone: a plain start, Media3 moves it to the foreground.
        fun start(context: Context, playing: Boolean) {
            val i = Intent(context, PlaybackService::class.java)
            // Refused when the app may not start a service from the background (the subclass for a
            // foreground one included).
            try {
                if (playing) ContextCompat.startForegroundService(context, i) else context.startService(i)
            } catch (e: IllegalStateException) {
                Log.w(TAG, "service start refused", e)
            }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, PlaybackService::class.java))
        }
    }
}
