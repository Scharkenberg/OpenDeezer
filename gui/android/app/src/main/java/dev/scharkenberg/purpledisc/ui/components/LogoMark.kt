package dev.scharkenberg.purpledisc.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.AnimationVector1D
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Matrix
import kotlinx.coroutines.flow.first
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.drawscope.scale
import dev.scharkenberg.purpledisc.ui.theme.MarkPurple

// The mark of assets/icon.svg, in its 120-unit box.
private const val BOX = 120f
private const val FAINT = 0.65f

// The draw-in: outer arc 0-450 ms, inner arc 100-500 ms, disc 0-400 ms
// with overshoot, dot 300-600 ms.
const val DRAW_IN_MS = 600
private const val SPIN_MS = 1100

// Played once per process; later marks are drawn whole.
private var drawnIn = false

// True while the launch mark covers the app.
var launchMarkShown by mutableStateOf(false)

/** The draw-in's progress, 0 to 1 over [DRAW_IN_MS]; it runs the first time a mark is shown. */
@Composable
fun rememberDrawIn(): Animatable<Float, AnimationVector1D> {
    val drawIn = remember { Animatable(if (drawnIn) 1f else 0f) }
    LaunchedEffect(Unit) {
        if (drawIn.value < 1f) {
            snapshotFlow { launchMarkShown }.first { !it }
            drawnIn = true
            drawIn.animateTo(1f, tween(DRAW_IN_MS, easing = LinearEasing))
        }
    }
    return drawIn
}

/** The app's mark; its dot turns once around it when [playingId] changes. */
@Composable
fun LogoMark(playingId: String?, modifier: Modifier = Modifier, drawIn: Animatable<Float, AnimationVector1D> = rememberDrawIn()) {
    val spin = remember { Animatable(0f) }
    val shown = remember { arrayOf(playingId) }
    LaunchedEffect(playingId) {
        if (playingId == shown[0]) return@LaunchedEffect
        shown[0] = playingId
        spin.snapTo(0f)
        spin.animateTo(360f, tween(SPIN_MS))
    }
    val disc = remember {
        Path().apply {
            fillType = PathFillType.EvenOdd
            addOval(Rect(39f, 39f, 81f, 81f))
            moveTo(57.43f, 50.06f)
            lineTo(70.57f, 57.94f)
            quadraticTo(74f, 60f, 70.57f, 62.06f)
            lineTo(57.43f, 69.94f)
            quadraticTo(54f, 72f, 54f, 68f)
            lineTo(54f, 52f)
            quadraticTo(54f, 48f, 57.43f, 50.06f)
            close()
        }
    }
    val discPx = remember { Path().apply { fillType = PathFillType.EvenOdd } }
    val toPx = remember { Matrix() }
    Canvas(modifier) {
        val ms = drawIn.value * DRAW_IN_MS
        val outer = FastOutSlowInEasing.transform(part(ms, 0f, 450f))
        val inner = FastOutSlowInEasing.transform(part(ms, 100f, 400f))
        val discScale = overshoot(part(ms, 0f, 400f))
        val dot = part(ms, 300f, 300f)
        val center = Offset(BOX / 2, BOX / 2)
        val unit = size.minDimension / BOX
        scale(unit, pivot = Offset.Zero) {
            drawCircle(MarkPurple, 54f, center)
            if (outer > 0f) drawArc(Color.White, -15f, 310f * outer, false, Offset(18f, 18f), Size(84f, 84f), style = Stroke(9f, cap = StrokeCap.Round))
            if (inner > 0f) drawArc(Color.White, 80f, 135f * inner, false, Offset(30.5f, 30.5f), Size(59f, 59f), alpha = FAINT, style = Stroke(5f, cap = StrokeCap.Round))
            rotate(spin.value, center) {
                drawCircle(Color.White, 5f * dot, Offset(92.17f, 33f), alpha = FAINT)
            }
        }
        // In pixels: before API 28 a path under a canvas scale is rasterized at scale 1 and stretched.
        toPx.reset()
        toPx[0, 0] = unit * discScale
        toPx[1, 1] = unit * discScale
        toPx[3, 0] = unit * center.x * (1f - discScale)
        toPx[3, 1] = unit * center.y * (1f - discScale)
        discPx.rewind()
        discPx.addPath(disc)
        discPx.transform(toPx)
        drawPath(discPx, Color.White)
    }
}

private fun part(ms: Float, start: Float, length: Float) = ((ms - start) / length).coerceIn(0f, 1f)

// The platform's overshoot interpolator (tension 2).
private fun overshoot(t: Float): Float {
    val u = t - 1f
    return u * u * (3f * u + 2f) + 1f
}
