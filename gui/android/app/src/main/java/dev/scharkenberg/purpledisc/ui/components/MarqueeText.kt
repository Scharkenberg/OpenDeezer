package dev.scharkenberg.purpledisc.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.MotionDurationScale
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay

private const val START_DELAY_MS = 1500L
private const val TURN_PAUSE_MS = 1000L
private val Speed = 36.dp // per second
private val Fade = 16.dp

/**
 * One line of text that, when it does not fit, scrolls once to its end and back after a
 * moment, so a long title can be read without opening anything; it then rests at its start
 * under a fade. Text that fits is a plain [Text].
 */
@Composable
fun MarqueeText(
    text: String,
    modifier: Modifier = Modifier,
    style: TextStyle = LocalTextStyle.current,
    color: Color = Color.Unspecified,
) {
    var boxWidth by remember { mutableIntStateOf(0) }
    var textWidth by remember { mutableIntStateOf(0) }
    val overflow = (textWidth - boxWidth).coerceAtLeast(0)
    val shift = remember(text) { Animatable(0f) }
    val density = LocalDensity.current
    val pxPerSecond = with(density) { Speed.toPx() }
    val fade = with(density) { Fade.toPx() }
    // Right to left, the text starts at the right edge and scrolls the other way.
    val rtl = LocalLayoutDirection.current == LayoutDirection.Rtl
    LaunchedEffect(text, overflow) {
        shift.snapTo(0f)
        if (overflow == 0 || coroutineContext[MotionDurationScale]?.scaleFactor == 0f) return@LaunchedEffect
        delay(START_DELAY_MS)
        val spec = tween<Float>((overflow / pxPerSecond * 1000).toInt(), easing = LinearEasing)
        shift.animateTo(overflow.toFloat(), spec)
        delay(TURN_PAUSE_MS)
        shift.animateTo(0f, spec)
    }
    Box(
        modifier
            .clipToBounds()
            .onSizeChanged { boxWidth = it.width }
            .graphicsLayer(compositingStrategy = if (overflow > 0) CompositingStrategy.Offscreen else CompositingStrategy.Auto)
            .drawWithContent {
                drawContent()
                if (overflow == 0) return@drawWithContent
                // Fades where text runs past an edge: the end at rest, the start once moved.
                val edge = fade / size.width
                val startAlpha = if (shift.value > 0f) 0f else 1f
                val endAlpha = if (shift.value < overflow) 0f else 1f
                drawRect(
                    Brush.horizontalGradient(
                        0f to Color.Black.copy(alpha = if (rtl) endAlpha else startAlpha),
                        edge to Color.Black,
                        1f - edge to Color.Black,
                        1f to Color.Black.copy(alpha = if (rtl) startAlpha else endAlpha),
                    ),
                    blendMode = BlendMode.DstIn,
                )
            },
    ) {
        Text(
            text,
            style = style,
            color = color,
            maxLines = 1,
            softWrap = false,
            overflow = TextOverflow.Clip,
            modifier = Modifier
                .wrapContentWidth(Alignment.Start, unbounded = true)
                .onSizeChanged { textWidth = it.width }
                .graphicsLayer { translationX = if (rtl) shift.value else -shift.value },
        )
    }
}
