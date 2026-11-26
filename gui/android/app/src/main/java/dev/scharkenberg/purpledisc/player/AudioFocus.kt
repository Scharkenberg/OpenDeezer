package dev.scharkenberg.purpledisc.player

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.os.Build
import dev.scharkenberg.purpledisc.engine.Engine

/**
 * Audio focus for local playback: asked before a start or resume, given up on pause and
 * stop. A transient loss (a call) pauses and frees the output; its end resumes, unless the
 * user paused or played meanwhile. Below Android 8 a loss that allows ducking lowers the
 * volume until the gain (later versions duck by themselves).
 */
class AudioFocus(context: Context, private val player: () -> PlayerController) {
    private val am = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private var held = false
    private var pausedByLoss = false
    private var losing = false
    private var duckedFrom: Double? = null

    private val listener = AudioManager.OnAudioFocusChangeListener { change ->
        when (change) {
            AudioManager.AUDIOFOCUS_LOSS -> {
                held = false
                pausedByLoss = false
                player().pause()
            }
            AudioManager.AUDIOFOCUS_LOSS_TRANSIENT -> {
                // The output is not ours until the gain: a play meanwhile asks again.
                held = false
                // A track still loading would start into the call.
                val st = player().state.value
                if (st.isPlaying || st.state == Engine.LOADING) {
                    losing = true
                    player().pause()
                    losing = false
                    pausedByLoss = true
                    Engine.setOutputSuspended(true)
                }
            }
            AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK -> if (Build.VERSION.SDK_INT < 26 && duckedFrom == null) {
                val v = Engine.volume()
                duckedFrom = v
                Engine.setVolume(v * DUCK)
            }
            AudioManager.AUDIOFOCUS_GAIN -> {
                held = true
                duckedFrom?.let(Engine::setVolume)
                duckedFrom = null
                Engine.setOutputSuspended(false)
                if (pausedByLoss) {
                    pausedByLoss = false
                    player().resume()
                }
            }
        }
    }

    private val request = if (Build.VERSION.SDK_INT >= 26) {
        AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_MEDIA)
                    .setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
                    .build(),
            )
            .setOnAudioFocusChangeListener(listener)
            .build()
    } else {
        null
    }

    /** True when local playback may start. */
    fun request(): Boolean {
        // A play by the user: the end of a loss no longer resumes.
        pausedByLoss = false
        if (held) return true
        val granted = if (request != null) {
            am.requestAudioFocus(request)
        } else {
            @Suppress("DEPRECATION")
            am.requestAudioFocus(listener, AudioManager.STREAM_MUSIC, AudioManager.AUDIOFOCUS_GAIN)
        }
        held = granted == AudioManager.AUDIOFOCUS_REQUEST_GRANTED
        if (held) Engine.setOutputSuspended(false)
        return held
    }

    // Kept through a pause the transient loss itself caused, so its end can resume; a pause or
    // stop by the user cancels that resume.
    fun abandon() {
        if (losing) return
        pausedByLoss = false
        if (!held) return
        if (request != null) {
            am.abandonAudioFocusRequest(request)
        } else {
            @Suppress("DEPRECATION")
            am.abandonAudioFocus(listener)
        }
        held = false
    }

    private companion object {
        const val DUCK = 0.2
    }
}
