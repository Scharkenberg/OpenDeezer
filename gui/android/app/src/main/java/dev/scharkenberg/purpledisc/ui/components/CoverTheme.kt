package dev.scharkenberg.purpledisc.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.platform.LocalContext
import coil3.SingletonImageLoader
import coil3.request.ImageRequest
import coil3.request.SuccessResult
import coil3.request.allowHardware
import coil3.toBitmap
import dev.scharkenberg.purpledisc.ui.theme.CoverFallback
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

private const val SAMPLE_PX = 16

/** A cover's color fading into the next track's, behind Now Playing and in the mini player. */
internal const val COVER_FADE_MS = 600

// The cover's average color, from a 16 px copy of it; the fallback until it is read.
@Composable
internal fun rememberCoverColor(url: String?): Color {
    val context = LocalContext.current
    val color by produceState(CoverFallback, url) {
        if (url.isNullOrBlank()) return@produceState
        val request = ImageRequest.Builder(context).data(url).size(SAMPLE_PX).allowHardware(false).build()
        val bitmap = (SingletonImageLoader.get(context).execute(request) as? SuccessResult)?.image?.toBitmap() ?: return@produceState
        value = withContext(Dispatchers.Default) {
            var r = 0L
            var g = 0L
            var b = 0L
            val n = bitmap.width * bitmap.height
            for (y in 0 until bitmap.height) for (x in 0 until bitmap.width) {
                val p = bitmap.getPixel(x, y)
                r += (p shr 16) and 0xFF
                g += (p shr 8) and 0xFF
                b += p and 0xFF
            }
            Color((r / n).toInt(), (g / n).toInt(), (b / n).toInt())
        }
    }
    return color
}

/**
 * A dark scheme made of the cover's color, drawn as a deep gradient behind [content], as
 * Now Playing and the lyrics show it in light and dark mode alike (the shell turns the status bar
 * icons light over it). The color follows the track with a fade.
 */
@Composable
fun CoverTheme(url: String?, content: @Composable () -> Unit) {
    val target = rememberCoverColor(url)
    val cover = animateColorAsState(target, tween(COVER_FADE_MS), label = "cover")
    val scheme = remember(target) {
        val top = lerp(target, Color.Black, TOP_SHADE)
        val bottom = lerp(target, Color.Black, BOTTOM_SHADE)
        darkColorScheme(
            primary = Color.White,
            onPrimary = bottom,
            primaryContainer = lerp(target, Color.White, 0.25f),
            onPrimaryContainer = Color.White,
            secondaryContainer = lerp(top, Color.White, 0.18f),
            onSecondaryContainer = Color.White,
            surface = bottom,
            onSurface = Color.White,
            onSurfaceVariant = Color.White.copy(alpha = 0.72f),
            surfaceContainerLow = lerp(bottom, Color.White, 0.06f),
            surfaceContainer = lerp(bottom, Color.White, 0.08f),
            surfaceContainerHigh = lerp(bottom, Color.White, 0.12f),
            surfaceContainerHighest = lerp(bottom, Color.White, 0.16f),
            outline = Color.White.copy(alpha = 0.4f),
            outlineVariant = Color.White.copy(alpha = 0.16f),
            tertiary = lerp(target, Color.White, 0.85f),
        )
    }
    MaterialTheme(colorScheme = scheme, typography = MaterialTheme.typography, shapes = MaterialTheme.shapes) {
        // The fade is read in the draw phase: it redraws the gradient, not the page.
        Box(
            Modifier.fillMaxSize().drawBehind {
                val c = cover.value
                drawRect(Brush.verticalGradient(listOf(lerp(c, Color.Black, TOP_SHADE), lerp(c, Color.Black, BOTTOM_SHADE))))
            },
        ) { content() }
    }
}

private const val TOP_SHADE = 0.35f
private const val BOTTOM_SHADE = 0.8f
