package dev.scharkenberg.purpledisc.ui.components

import androidx.compose.foundation.layout.heightIn
import androidx.compose.ui.unit.Dp
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.material3.LocalContentColor
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.ui.util.lerp
import kotlinx.coroutines.launch
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Block
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.Explicit
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.painter.ColorPainter
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import coil3.compose.AsyncImage
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.engine.ConnectDevice
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.Track
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space

// Deezer's CDN serves a cover at any of these sizes ("/250x250-" in the URL): the smallest
// that covers the shown size is asked for, as the Windows app does.
private val cdnSize = Regex("/\\d+x\\d+-")
private val cdnSides = intArrayOf(56, 250, 500, 1000)

private fun sizedArt(url: String, px: Int): String {
    if (!url.contains("dzcdn.net/images/")) return url
    val side = cdnSides.firstOrNull { it >= px } ?: cdnSides.last()
    return cdnSize.replaceFirst(url, "/${side}x$side-")
}

/** A cover at about [shown] across, which picks the CDN's size. */
@Composable
fun Artwork(
    url: String?,
    modifier: Modifier = Modifier,
    shape: Shape = MaterialTheme.shapes.small,
    shown: Dp,
) {
    Box(modifier.clip(shape).background(MaterialTheme.colorScheme.surfaceVariant)) {
        if (!url.isNullOrBlank()) {
            val px = with(LocalDensity.current) { shown.roundToPx() }
            val blank = ColorPainter(MaterialTheme.colorScheme.surfaceVariant)
            AsyncImage(
                model = sizedArt(url, px),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
                placeholder = blank,
                error = blank,
            )
        }
    }
}

/** The id of the track playing, for the rows' playing mark; provided by the app shell. */
val LocalPlayingId = compositionLocalOf { "" }

/** The ids of the tracks kept offline, for the rows' mark; provided by the app shell. */
val LocalOfflineIds = compositionLocalOf { emptySet<String>() }

/** Opens a track's sheet; provided by the app shell, which hosts the one sheet. */
val LocalTrackSheet = staticCompositionLocalOf<(TrackSheetRequest) -> Unit> { {} }

/** Offline mode is on: rows without an offline copy are dimmed and do not play. */
val LocalOfflineMode = staticCompositionLocalOf { false }

/**
 * A track row: cover, title (in primary while it plays), artist, and a More button; a track
 * Deezer streams nowhere is greyed out. A long-press opens the sheet. [playlistId] is the
 * account's own playlist the row is in.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun TrackRow(
    track: Track,
    player: PlayerController,
    context: ListenContext,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    playlistId: String? = null,
) {
    val openSheet = LocalTrackSheet.current
    val offline = track.id in LocalOfflineIds.current
    val playable = !track.unavailable && (!LocalOfflineMode.current || offline)
    val sheet = { openSheet(TrackSheetRequest(track, context, playlistId)) }
    Row(
        modifier
            .fillMaxWidth()
            .heightIn(min = Size.row)
            .combinedClickable(enabled = playable, onClick = onClick, onLongClick = sheet)
            .alpha(if (playable) 1f else DISABLED_ALPHA)
            .padding(start = Space.l),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Artwork(track.artworkUrl, Modifier.size(Size.rowArt), shown = Size.rowArt)
        Spacer(Modifier.width(Space.m))
        Column(Modifier.weight(1f)) {
            Text(
                track.name.ifBlank { stringResource(R.string.unknown_title) },
                style = MaterialTheme.typography.bodyLarge,
                color = if (track.id == LocalPlayingId.current) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (track.unavailable) {
                    Icon(
                        Icons.Filled.Block,
                        contentDescription = stringResource(R.string.track_unavailable),
                        modifier = Modifier.size(Space.l).padding(end = Space.xs),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (track.explicit) {
                    Icon(
                        Icons.Filled.Explicit,
                        contentDescription = stringResource(R.string.cd_explicit),
                        modifier = Modifier.size(Space.l).padding(end = Space.xs),
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (offline) {
                    Icon(
                        Icons.Filled.DownloadDone,
                        contentDescription = stringResource(R.string.cd_downloaded),
                        modifier = Modifier.size(Space.l).padding(end = Space.xs),
                        tint = MaterialTheme.colorScheme.primary,
                    )
                }
                Text(
                    track.artistLine.ifBlank { track.albumName },
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        MoreButton(sheet)
    }
}

/** A cover card for the carousels: [Size.card] wide, title and subtitle on one line each. */
@Composable
fun MediaCard(
    title: String,
    subtitle: String,
    artworkUrl: String?,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    round: Boolean = false,
    onLongClick: (() -> Unit)? = null,
) {
    Column(Modifier.width(Size.card).then(modifier).combinedClickable(onClick = onClick, onLongClick = onLongClick)) {
        Artwork(artworkUrl, Modifier.fillMaxWidth().aspectRatio(1f), shape = if (round) CircleShape else MaterialTheme.shapes.medium, shown = Size.card)
        Spacer(Modifier.height(Space.s))
        Text(
            title.ifBlank { stringResource(R.string.unknown_title) },
            style = MaterialTheme.typography.titleSmall,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            subtitle,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** The More button that opens a row's sheet. */
@Composable
fun MoreButton(onClick: () -> Unit) {
    AppIconButton(onClick = onClick) {
        Icon(Icons.Filled.MoreVert, contentDescription = stringResource(R.string.cd_more))
    }
}

private const val PRESSED_SCALE = 0.86f
private const val PRESS_DAMPING = 0.5f
private const val TURN_MS = 350
private const val NUDGE_MS = 120
private const val PLAY_MORPH_MS = 220

/** A full turn: a turned glyph always comes to rest upright. */
const val FULL_TURN = 360f

/** Dips to [PRESSED_SCALE] while [interaction] is pressed and springs back. */
@Composable
fun Modifier.pressScale(interaction: MutableInteractionSource): Modifier {
    val pressed by interaction.collectIsPressedAsState()
    val scale by animateFloatAsState(if (pressed) PRESSED_SCALE else 1f, spring(dampingRatio = PRESS_DAMPING), label = "press")
    return graphicsLayer {
        scaleX = scale
        scaleY = scale
    }
}

/**
 * An icon button that dips while pressed and springs back. A click also turns it once around
 * ([turn]), or moves it [nudge] sideways and back: positive to the right, negative to the
 * left, in every layout direction (media glyphs are not mirrored).
 */
@Composable
fun AppIconButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    turn: Boolean = false,
    nudge: Dp = 0.dp,
    content: @Composable () -> Unit,
) {
    val interaction = remember { MutableInteractionSource() }
    val rotation = remember { Animatable(0f) }
    val shift = remember { Animatable(0f) }
    val scope = rememberCoroutineScope()
    val nudgePx = with(LocalDensity.current) { nudge.toPx() }
    IconButton(
        onClick = {
            if (turn) scope.launch { rotation.animateTo(rotation.targetValue + FULL_TURN, tween(TURN_MS)) }
            if (nudgePx != 0f) scope.launch {
                shift.animateTo(nudgePx, tween(NUDGE_MS))
                shift.animateTo(0f, tween(NUDGE_MS))
            }
            onClick()
        },
        modifier = modifier.pressScale(interaction).graphicsLayer {
            rotationZ = rotation.value
            translationX = shift.value
        },
        enabled = enabled,
        interactionSource = interaction,
        content = content,
    )
}

// The play triangle and the pause bars as the same 8 points, in percent of the box.
private val PlayPoints = floatArrayOf(25f, 12f, 55f, 31f, 55f, 69f, 55f, 69f, 55f, 31f, 85f, 50f, 85f, 50f, 25f, 88f)
private val PausePoints = floatArrayOf(20f, 15f, 42f, 15f, 42f, 85f, 58f, 85f, 58f, 15f, 80f, 15f, 80f, 85f, 20f, 85f)

/** Play when [playing] is false, pause when true; one shape turns into the other. */
@Composable
fun PlayPauseIcon(playing: Boolean, description: String, modifier: Modifier = Modifier, tint: Color = LocalContentColor.current) {
    val t by animateFloatAsState(if (playing) 1f else 0f, tween(PLAY_MORPH_MS), label = "playPause")
    val path = remember { Path() }
    Canvas(modifier.semantics { contentDescription = description }) {
        path.reset()
        for (i in 0 until PlayPoints.size / 2) {
            val x = lerp(PlayPoints[2 * i], PausePoints[2 * i], t) / 100f * size.width
            val y = lerp(PlayPoints[2 * i + 1], PausePoints[2 * i + 1], t) / 100f * size.height
            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
        }
        path.close()
        drawPath(path, tint)
    }
}

@Composable
fun SectionHeader(text: String, modifier: Modifier = Modifier) {
    // A short accent bar leads the title.
    Row(
        modifier.padding(start = Space.l, end = Space.l, top = Space.l, bottom = Space.s),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier.size(width = Space.xs, height = Space.l)
                .background(MaterialTheme.colorScheme.primary, RoundedCornerShape(Space.xs / 2)),
        )
        Spacer(Modifier.width(Space.s))
        Text(text, style = MaterialTheme.typography.titleMedium)
    }
}

/** The room the floating mini player takes over the pages' bottom edge (none when it is hidden). */
val LocalFloatingBar = compositionLocalOf { 0.dp }

/** A scrolling page's end padding: its own gap, and the floating mini player's room. */
@Composable
fun listEnd(): PaddingValues = PaddingValues(bottom = Space.l + LocalFloatingBar.current)

/** The device-type label of a Connect peer. */
@Composable
fun deviceTypeLabel(device: ConnectDevice): String = when (device.client.lowercase()) {
    "tui" -> stringResource(R.string.device_type_terminal)
    "darwin", "macos" -> "macOS"
    "windows" -> "Windows"
    "linux", "gnome", "kde" -> "Linux"
    "android" -> "Android"
    "" -> stringResource(R.string.device_type_generic)
    else -> device.client
}

/** List keys from ids; a repeated id gets its occurrence count ("id#2"). */
fun <T> idKeys(list: List<T>, id: (T) -> String): List<String> {
    val seen = HashMap<String, Int>()
    return list.map {
        val k = id(it)
        val n = seen.merge(k, 1, Int::plus)!!
        if (n == 1) k else "$k#$n"
    }
}

/** A long time as hours and minutes ("6h 53m"). */
@Composable
fun formatTotal(seconds: Long): String {
    val minutes = seconds / 60
    return if (minutes >= 60) stringResource(R.string.dur_hm, minutes / 60, minutes % 60) else stringResource(R.string.dur_m, minutes)
}

fun formatDuration(ms: Long): String {
    if (ms <= 0) return "0:00"
    val totalSec = ms / 1000
    return "%d:%02d".format(totalSec / 60, totalSec % 60)
}
