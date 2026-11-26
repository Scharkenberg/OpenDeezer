package dev.scharkenberg.purpledisc.ui.screens

import android.os.SystemClock
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.DragInteraction
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lyrics
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import dev.scharkenberg.purpledisc.ui.components.MarqueeText
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.readableWidth
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.LyricLine
import dev.scharkenberg.purpledisc.engine.Lyrics
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.CoverTheme
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.theme.Space

// After a scroll by hand the active line is not followed for this long.
private const val FOLLOW_PAUSE_MS = 5000L

@Composable
fun LyricsScreen(player: PlayerController, onBack: () -> Unit) {
    val state = player.state.collectAsState()
    val track by remember { derivedStateOf { state.value.current } }
    var translate by rememberSaveable { mutableStateOf(false) }
    var hasTranslation by remember { mutableStateOf(false) }
    CoverTheme(track?.artworkUrl) {
    Scaffold(
        containerColor = Color.Transparent,
        contentColor = MaterialTheme.colorScheme.onSurface,
        topBar = {
            BackdropBar(
                title = {
                    Column {
                        MarqueeText(track?.name.orEmpty(), style = MaterialTheme.typography.titleMedium)
                        MarqueeText(
                            track?.artistLine.orEmpty(),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                onBack = onBack,
                actions = {
                    if (hasTranslation) {
                        Text(stringResource(R.string.lyrics_translation), style = MaterialTheme.typography.labelLarge)
                        Switch(checked = translate, onCheckedChange = { translate = it }, modifier = Modifier.padding(horizontal = Space.s))
                    }
                },
            )
        },
    ) { padding ->
        LyricsContent(player, Modifier.fillMaxSize().padding(padding), translate) { hasTranslation = it }
    }
    }
}

/** The current track's lyrics, synced lines following playback; reused by the fold layout. */
@Composable
fun LyricsContent(
    player: PlayerController,
    modifier: Modifier = Modifier,
    translate: Boolean = false,
    onTranslation: (Boolean) -> Unit = {},
) {
    val state = player.state.collectAsState()
    val progress = player.progress.collectAsState()
    val trackId by remember { derivedStateOf { state.value.current?.id.orEmpty() } }
    val (load, retry) = rememberLoad("lyrics:$trackId") { if (trackId.isEmpty()) Lyrics("", emptyList()) else Engine.lyricsOrNull(trackId) }
    LoadView(
        load,
        onRetry = retry,
        emptyIcon = Icons.Filled.Lyrics,
        emptyText = stringResource(R.string.lyrics_none),
        modifier = modifier,
        isEmpty = { it.lines.isEmpty() && it.plain.isBlank() },
    ) { lyrics ->
        LaunchedEffect(lyrics) { onTranslation(lyrics.lines.any { it.translated.isNotBlank() }) }
        if (lyrics.lines.isNotEmpty()) {
            SyncedLyrics(lyrics.lines, { progress.value.positionMs }, player::seek, translate, modifier)
        } else {
            Text(
                lyrics.plain,
                modifier = modifier.verticalScroll(rememberScrollState()).padding(Space.l),
                style = MaterialTheme.typography.bodyLarge,
            )
        }
    }
}

@Composable
private fun SyncedLyrics(
    lines: List<LyricLine>,
    positionMs: () -> Long,
    onSeek: (Long) -> Unit,
    translate: Boolean,
    modifier: Modifier,
) {
    val active by remember(lines) { derivedStateOf { activeLine(lines, positionMs()) } }
    val listState = rememberLazyListState()
    var userScrollAt by remember { mutableLongStateOf(0L) }
    LaunchedEffect(listState) {
        listState.interactionSource.interactions.collect {
            if (it is DragInteraction.Start) userScrollAt = SystemClock.uptimeMillis()
        }
    }
    LaunchedEffect(active) {
        if (active >= 0 && SystemClock.uptimeMillis() - userScrollAt >= FOLLOW_PAUSE_MS) listState.animateScrollToItem(active)
    }
    BoxWithConstraints(modifier) {
        // A third of the viewport above and below: the active line sits a third down, and the
        // first and last lines can reach that position.
        val third = maxHeight / 3
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize().readableWidth(),
            contentPadding = PaddingValues(start = Space.l, end = Space.l, top = third, bottom = third),
            verticalArrangement = Arrangement.spacedBy(Space.l),
        ) {
            itemsIndexed(lines, key = { i, _ -> i }) { i, line ->
                Column(
                    Modifier.fillMaxWidth().clickable {
                        userScrollAt = 0L
                        onSeek(line.timeMs)
                    },
                ) {
                    Text(
                        line.text.ifBlank { "♪" },
                        style = MaterialTheme.typography.headlineSmall,
                        color = if (i == active) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    if (translate && line.translated.isNotBlank()) {
                        Text(line.translated, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
    }
}

// The last line that has started, by binary search; -1 before the first.
private fun activeLine(lines: List<LyricLine>, positionMs: Long): Int {
    var lo = 0
    var hi = lines.lastIndex
    var found = -1
    while (lo <= hi) {
        val mid = (lo + hi) / 2
        if (lines[mid].timeMs <= positionMs) {
            found = mid
            lo = mid + 1
        } else {
            hi = mid - 1
        }
    }
    return found
}
