package dev.scharkenberg.purpledisc.ui.theme

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/** Spacing steps; a screen's edge padding is [l]. */
object Space {
    val xs = 4.dp
    val s = 8.dp
    val m = 12.dp
    val l = 16.dp
    val xl = 24.dp
}

/** Fixed sizes shared by the screens. */
object Size {
    val row = 64.dp
    val rowArt = 48.dp
    val card = 148.dp
    val miniBar = 64.dp
    // Home's Flow and Charts tiles.
    val quickTile = 56.dp
    val mark = 28.dp
    // A page's large cover or portrait, and the phone remote's QR code.
    val hero = 200.dp
    // The launch mark's box, and the spinner's drop below its center.
    val launchMark = 192.dp
    val launchSpinner = 136.dp
    // The mini player's progress line.
    val barLine = 3.dp
}

/** Motion distances: Home's title sliding in, next and previous moving on a click, a like's ring. */
object Move {
    val titleSlide = 8.dp
    val nudge = 5.dp
    val ring = 2.dp
}

/** The circle of the app's mark (assets/icon.svg). */
val MarkPurple = Color(0xFF9A45FF)
