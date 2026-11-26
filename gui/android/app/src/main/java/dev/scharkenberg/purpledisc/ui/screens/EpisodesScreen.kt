package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Podcasts
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.readableWidth
import dev.scharkenberg.purpledisc.engine.Bookmark
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.Artwork
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.LocalPlayingId
import dev.scharkenberg.purpledisc.ui.components.LocalTrackSheet
import dev.scharkenberg.purpledisc.ui.components.MoreButton
import dev.scharkenberg.purpledisc.ui.components.TrackSheetRequest
import dev.scharkenberg.purpledisc.ui.components.formatDuration
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space

/** A podcast's episodes; a started one shows how far it got, a heard one a check mark. */
@Composable
fun EpisodesScreen(podcastId: String, podcastName: String, player: PlayerController, onBack: () -> Unit) {
    val (load, retry) = rememberLoad("podcast:$podcastId") { Engine.podcastEpisodesOrNull(podcastId) }
    // Read again when the playing episode changes, so the progress follows the listening.
    val playingId = LocalPlayingId.current
    val marks by produceState<Map<String, Bookmark>>(emptyMap(), playingId) { value = Engine.bookmarks() }
    val openSheet = LocalTrackSheet.current
    val context = ListenContext("show_page", podcastId)
    Scaffold(topBar = { PageBar(podcastName, onBack) }) { padding ->
        LoadView(load, retry, Icons.Filled.Podcasts, stringResource(R.string.episodes_none), Modifier.padding(padding), isEmpty = { it.isEmpty() }) { list ->
            val tracks = remember(list) { list.map { it.asTrack() } }
            LazyColumn(Modifier.fillMaxSize().padding(padding).readableWidth(), contentPadding = listEnd()) {
                itemsIndexed(list, key = { _, e -> e.id }) { i, e ->
                    val mark = marks[e.id]
                    val meta = listOf(e.releaseDate, if (e.durationMs > 0) formatDuration(e.durationMs) else "").filter { it.isNotBlank() }.joinToString(" · ")
                    ListItem(
                        onClick = { player.playQueue(tracks, i, context) },
                        supportingContent = {
                            Column {
                                Text(meta)
                                val started = mark != null && !mark.heard && mark.offsetSec > 0 && e.durationMs > 0
                                if (started) {
                                    LinearProgressIndicator(
                                        progress = { (mark.offsetSec * 1000f / e.durationMs).coerceIn(0f, 1f) },
                                        modifier = Modifier.fillMaxWidth().padding(top = Space.xs),
                                        gapSize = 0.dp,
                                        drawStopIndicator = {},
                                    )
                                }
                            }
                        },
                        leadingContent = { Artwork(e.artworkUrl, Modifier.size(Size.rowArt), shown = Size.rowArt) },
                        trailingContent = {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                if (mark?.heard == true) {
                                    Icon(Icons.Filled.CheckCircle, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                                }
                                MoreButton { openSheet(TrackSheetRequest(tracks[i], context)) }
                            }
                        },
                    ) {
                        Text(
                            e.title,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                            color = if (e.id == playingId) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
                        )
                    }
                }
            }
        }
    }
}
