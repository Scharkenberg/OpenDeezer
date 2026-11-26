package dev.scharkenberg.purpledisc.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * How much room the window gives, by Material's window size classes: [wide] from 600 dp
 * across (a tablet, an unfolded foldable, a landscape phone), [short] under 480 dp high (a
 * landscape phone). Pages switch to a navigation rail and two panes when either holds.
 */
data class WindowLayout(val wide: Boolean = false, val short: Boolean = false) {
    /** Room for two panes side by side. */
    val twoPane: Boolean get() = wide || short

    companion object {
        fun of(width: Dp, height: Dp) = WindowLayout(wide = width >= 600.dp, short = height < 480.dp)
    }
}

val LocalWindowLayout = staticCompositionLocalOf { WindowLayout() }

/** Lists and text stay at a readable width, centered, on a wide window. */
fun Modifier.readableWidth(): Modifier = fillMaxWidth().wrapContentWidth(Alignment.CenterHorizontally).widthIn(max = 720.dp)

/**
 * Two panes side by side: on a book-posture foldable split at the hinge, else [first] takes
 * [firstShare] of the width.
 */
@Composable
fun TwoPane(
    modifier: Modifier = Modifier,
    firstShare: Float = 0.42f,
    first: @Composable () -> Unit,
    second: @Composable () -> Unit,
) {
    val fold = LocalFoldState.current
    val hinge = fold.hingeBounds
    if (fold.isBook && hinge != null) {
        HingeSplit(hinge, horizontalHinge = false, modifier = modifier, first = first, second = second)
        return
    }
    Row(modifier) {
        Box(Modifier.weight(firstShare).fillMaxHeight()) { first() }
        Box(Modifier.weight(1f - firstShare).fillMaxHeight()) { second() }
    }
}
