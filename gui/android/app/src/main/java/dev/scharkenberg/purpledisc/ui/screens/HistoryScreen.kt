package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.History
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.readableWidth
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.HistoryEntry
import dev.scharkenberg.purpledisc.engine.HistoryStats
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.Track
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.AppChip
import dev.scharkenberg.purpledisc.ui.components.Load
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.TrackRow
import dev.scharkenberg.purpledisc.ui.components.formatTotal
import dev.scharkenberg.purpledisc.ui.theme.Space

private class History(val recent: List<HistoryEntry>, val stats: HistoryStats)

/**
 * The tracks played on this device: the last 30 days' listening time, then one list at a time,
 * as the Windows app shows them: the recent plays, the top tracks or the top artists.
 */
@Composable
fun HistoryScreen(player: PlayerController, onBack: () -> Unit) {
    val load by produceState<Load<History>>(Load.Loading) { value = Load.Ready(History(Engine.historyRecent(50), Engine.historyStats(30))) }
    val context = ListenContext("history_song", "")
    var shown by rememberSaveable { mutableIntStateOf(0) }
    Scaffold(topBar = { PageBar(stringResource(R.string.section_recently_played), onBack) }) { padding ->
        LoadView(load, {}, Icons.Filled.History, stringResource(R.string.history_empty), Modifier.padding(padding), isEmpty = { it.recent.isEmpty() && it.stats.topTracks.isEmpty() }) { h ->
            val stats = h.stats
            LazyColumn(Modifier.fillMaxSize().padding(padding).readableWidth(), contentPadding = listEnd()) {
                item {
                    Column(Modifier.padding(horizontal = Space.l, vertical = Space.s)) {
                        Text(stringResource(R.string.history_total, formatTotal(stats.totalSeconds)), style = MaterialTheme.typography.titleLarge)
                        Text(stringResource(R.string.history_stats_section), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                item {
                    val tabs = listOf(R.string.section_recently_played, R.string.section_top_tracks, R.string.history_top_artists)
                    LazyRow(contentPadding = PaddingValues(horizontal = Space.l, vertical = Space.s), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                        items(tabs.size) { i -> AppChip(selected = shown == i, onClick = { shown = i }, label = { Text(stringResource(tabs[i])) }) }
                    }
                }
                when (shown) {
                    0 -> items(h.recent, key = { "h-${it.trackId}-${it.startedAt}" }) { e ->
                        val t = track(e.trackId, e.title, e.artist, e.album, e.artworkUrl, e.durationMs, e.kind == "episode")
                        TrackRow(t, player, context, onClick = { player.playSingle(t, context) })
                    }
                    1 -> items(stats.topTracks, key = { "st-${it.trackId}" }) { s ->
                        val plays = pluralStringResource(R.plurals.n_plays, s.plays, s.plays)
                        val t = track(s.trackId, s.title, listOf(s.artist, plays).filter { it.isNotBlank() }.joinToString(" · "), "", s.artworkUrl, s.durationMs, false)
                        TrackRow(t, player, context, onClick = { player.playSingle(t, context) })
                    }
                    else -> items(stats.topArtists, key = { "sa-${it.artist}" }) { a ->
                        ListItem(supportingContent = { Text(pluralStringResource(R.plurals.n_plays, a.plays, a.plays)) }) {
                            Text(a.artist.ifBlank { stringResource(R.string.unknown_title) })
                        }
                    }
                }
            }
        }
    }
}

// History keeps no album id or explicit flag; the engine reads the rest when it plays.
private fun track(id: String, title: String, artistLine: String, album: String, artwork: String, durationMs: Long, episode: Boolean) =
    Track(id, title, durationMs, emptyList(), artistLine, album, artwork, false, isEpisode = episode)
