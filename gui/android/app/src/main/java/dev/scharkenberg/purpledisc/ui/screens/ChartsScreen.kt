package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import dev.scharkenberg.purpledisc.ui.components.AppChip
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.BarChart
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.readableWidth
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.LocalOfflineMode
import dev.scharkenberg.purpledisc.ui.components.SectionHeader
import dev.scharkenberg.purpledisc.ui.components.TrackRow
import dev.scharkenberg.purpledisc.ui.components.idKeys
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.theme.Space

@Composable
fun ChartsScreen(player: PlayerController, onBack: () -> Unit, onNavigate: (String) -> Unit) {
    val offline = LocalOfflineMode.current
    // The picked genre ("0": all) is not saved.
    var genre by remember { mutableStateOf("0") }
    val genres by produceState(emptyList<Pair<String, String>>(), offline) { if (!offline) value = Engine.genresOrNull().orEmpty() }
    val (load, retry) = rememberLoad(if (genre == "0") "charts" else "charts:$genre") {
        if (genre == "0") Engine.chartsOrNull() else Engine.genreChartsOrNull(genre)
    }
    val context = ListenContext("tops_track", genre)
    Scaffold(topBar = { PageBar(stringResource(R.string.charts_title), onBack) }) { padding ->
        Column(Modifier.padding(padding)) {
            if (genres.size > 1) {
                LazyRow(contentPadding = PaddingValues(horizontal = Space.l), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    items(genres, key = { it.first }) { (id, name) ->
                        AppChip(
                            selected = genre == id,
                            onClick = { genre = id },
                            label = { Text(if (id == "0") stringResource(R.string.charts_all_genres) else name) },
                            enabled = !offline,
                        )
                    }
                }
            }
            LoadView(load, retry, Icons.Filled.BarChart, stringResource(R.string.charts_unavailable), Modifier, isEmpty = { it.isEmpty }) { c ->
                val keys = remember(c) { idKeys(c.tracks) { it.id } }
                LazyColumn(Modifier.fillMaxSize().readableWidth(), contentPadding = listEnd()) {
                    if (c.tracks.isNotEmpty()) item { SectionHeader(stringResource(R.string.section_tracks)) }
                    itemsIndexed(c.tracks, key = { i, _ -> keys[i] }) { i, t ->
                        TrackRow(t, player, context, onClick = { player.playQueue(c.tracks, i, context) })
                    }
                    carousels(c) { _, _, route -> route?.let(onNavigate) }
                }
            }
        }
    }
}
