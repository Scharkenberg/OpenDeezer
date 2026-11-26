package dev.scharkenberg.purpledisc.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.clickable
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.drawOutline
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.lerp
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.Speaker
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.engine.Elsewhere
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.Track
import dev.scharkenberg.purpledisc.player.PlayerState
import dev.scharkenberg.purpledisc.player.Progress
import dev.scharkenberg.purpledisc.ui.theme.Move
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space

/** The mini player: the playing track, play/pause and next, the progress along its bottom edge. */
@Composable
fun PlayerBar(
    state: PlayerState,
    progress: State<Progress>,
    onOpen: () -> Unit,
    onToggle: () -> Unit,
    onNext: () -> Unit,
    onPlayHere: (Track) -> Unit,
) {
    if (state.showElsewhere) {
        ElsewhereBar(state.elsewhere!!, onPlayHere)
        return
    }
    val track = state.current ?: return
    BarSurface(track.artworkUrl, Modifier.clickable(onClick = onOpen)) {
        Box(Modifier.fillMaxWidth()) {
            // At least the bar's height; taller when large text needs it.
            Row(Modifier.fillMaxWidth().heightIn(min = Size.miniBar).padding(start = Space.s), verticalAlignment = Alignment.CenterVertically) {
                Artwork(track.artworkUrl, Modifier.size(Size.rowArt), shown = Size.rowArt)
                Spacer(Modifier.width(Space.m))
                Column(Modifier.weight(1f)) {
                    MarqueeText(
                        track.name.ifBlank { stringResource(R.string.unknown_title) },
                        style = MaterialTheme.typography.titleSmall,
                    )
                    MarqueeText(
                        track.artistLine.ifBlank { track.albumName },
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                AppIconButton(onClick = onToggle) {
                    PlayPauseIcon(state.state == Engine.PLAYING, stringResource(R.string.cd_play_pause), Modifier.size(Space.xl))
                }
                AppIconButton(onClick = onNext, enabled = state.hasNext, nudge = Move.nudge) {
                    Icon(Icons.Filled.SkipNext, contentDescription = stringResource(R.string.action_next))
                }
            }
            // Read in the draw phase: a position tick redraws the line, not the bar.
            LinearProgressIndicator(
                progress = {
                    val p = progress.value
                    if (p.durationMs > 0) (p.positionMs.toFloat() / p.durationMs).coerceIn(0f, 1f) else 0f
                },
                modifier = Modifier.fillMaxWidth().height(Size.barLine).align(Alignment.BottomCenter),
                trackColor = Color.Transparent,
                gapSize = 0.dp,
                drawStopIndicator = {},
            )
        }
    }
}

// A card floating over the page, washed from the start side with the cover's color and framed
// by a thin line of it.
@Composable
private fun BarSurface(cover: String?, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    val base = MaterialTheme.colorScheme.surfaceContainerHigh
    val color = rememberCoverColor(cover)
    val wash = animateColorAsState(lerp(base, color, COVER_WASH), tween(COVER_FADE_MS), label = "bar")
    val edge = animateColorAsState(lerp(base, color, COVER_EDGE), tween(COVER_FADE_MS), label = "edge")
    val shape = MaterialTheme.shapes.large
    Surface(
        color = base,
        shape = shape,
        shadowElevation = Space.s,
        modifier = Modifier.fillMaxWidth().padding(horizontal = Space.s, vertical = Space.xs),
    ) {
        // Both colors are read in the draw phase: the fade redraws the bar, it does not recompose it.
        Box(
            Modifier.drawWithContent {
                drawRect(Brush.horizontalGradient(listOf(wash.value, base)))
                drawContent()
                val line = 1.dp.toPx()
                translate(line / 2, line / 2) {
                    val inner = size.copy(width = size.width - line, height = size.height - line)
                    drawOutline(shape.createOutline(inner, layoutDirection, this), edge.value, style = Stroke(line))
                }
            }.then(modifier),
        ) { content() }
    }
}

private const val COVER_WASH = 0.3f
private const val COVER_EDGE = 0.7f

// Another session of the account plays and nothing plays here: its track, marked by the
// speaker glyph, with a button to play it here. A track it did not name shows as the glyph
// and the label only.
@Composable
private fun ElsewhereBar(away: Elsewhere, onPlayHere: (Track) -> Unit) {
    val track = away.track
    BarSurface(track?.artworkUrl) {
        Row(Modifier.fillMaxWidth().heightIn(min = Size.miniBar).padding(start = Space.s), verticalAlignment = Alignment.CenterVertically) {
            Artwork(track?.artworkUrl, Modifier.size(Size.rowArt), shown = Size.rowArt)
            Spacer(Modifier.width(Space.m))
            Column(Modifier.weight(1f)) {
                if (track != null) {
                    MarqueeText(
                        track.name.ifBlank { stringResource(R.string.unknown_title) },
                        style = MaterialTheme.typography.titleSmall,
                    )
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Filled.Speaker, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(Space.m))
                    Spacer(Modifier.width(Space.xs))
                    MarqueeText(
                        track?.artistLine?.ifBlank { null } ?: stringResource(R.string.playing_elsewhere),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            if (track != null) {
                AppIconButton(onClick = { onPlayHere(track) }) {
                    Icon(Icons.Filled.PlayArrow, contentDescription = stringResource(R.string.action_play_here))
                }
            }
        }
    }
}
