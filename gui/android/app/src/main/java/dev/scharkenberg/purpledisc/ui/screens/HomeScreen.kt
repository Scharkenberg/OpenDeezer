package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import androidx.compose.material.icons.filled.Pause
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.ui.draw.alpha
import dev.scharkenberg.purpledisc.data.Library
import dev.scharkenberg.purpledisc.ui.components.AppChip
import dev.scharkenberg.purpledisc.ui.components.DISABLED_ALPHA
import dev.scharkenberg.purpledisc.ui.components.LocalOfflineMode
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.BarChart
import androidx.compose.material.icons.filled.Cast
import androidx.compose.material.icons.filled.CastConnected
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Stream
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.heightIn
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.width
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.unit.LayoutDirection
import dev.scharkenberg.purpledisc.ui.theme.Move
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import dev.scharkenberg.purpledisc.ui.components.LocalPlayingId
import dev.scharkenberg.purpledisc.ui.components.LogoMark
import dev.scharkenberg.purpledisc.ui.components.rememberDrawIn
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.Routes
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.HomeData
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.Load
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.MediaCard
import dev.scharkenberg.purpledisc.ui.components.SectionHeader
import dev.scharkenberg.purpledisc.ui.components.TrackRow
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.theme.Space
import java.util.Calendar

private const val TOP_ROWS = 5
private const val HOME_WASH = 0.12f
private const val HOME_WASH_END = 0.35f

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(player: PlayerController, connected: Boolean, onNavigate: (String) -> Unit, onCast: () -> Unit) {
    val (load, retry) = rememberLoad("home") { Engine.homeOrNull() }
    val hour = Calendar.getInstance().get(Calendar.HOUR_OF_DAY)
    val greeting = stringResource(
        when {
            hour < 12 -> R.string.greeting_morning
            hour < 17 -> R.string.greeting_afternoon
            else -> R.string.greeting_evening
        },
    )
    // What plays (Flow, a mood, anything else), while it plays: the tile and the moods follow it.
    val from by remember(player) {
        player.state.map { if (it.state == Engine.PLAYING) player.playingFrom() else null }.distinctUntilChanged()
    }.collectAsState(null)
    val offline = LocalOfflineMode.current
    @Suppress("UNCHECKED_CAST")
    val moods by produceState(Library.peek("flowMoods") as List<Pair<String, String>>? ?: emptyList()) {
        if (value.isEmpty()) value = Engine.flowMoods().also { if (it.isNotEmpty()) Library.store("flowMoods", it) }
    }
    // The title's slide, mirrored in a right-to-left layout.
    val slideSign = if (LocalLayoutDirection.current == LayoutDirection.Rtl) -1f else 1f
    // A faint wash of the primary color at the top, behind the bar and the greeting.
    val wash = MaterialTheme.colorScheme.primary.copy(alpha = HOME_WASH)
    Scaffold(
        modifier = Modifier.background(Brush.verticalGradient(0f to wash, HOME_WASH_END to Color.Transparent)),
        containerColor = Color.Transparent,
        topBar = {
            TopAppBar(
                colors = TopAppBarDefaults.topAppBarColors(containerColor = Color.Transparent),
                title = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        val drawIn = rememberDrawIn()
                        LogoMark(LocalPlayingId.current, Modifier.size(Size.mark), drawIn)
                        Spacer(Modifier.width(Space.s))
                        Text(
                            stringResource(R.string.app_name),
                            Modifier.graphicsLayer {
                                alpha = drawIn.value
                                translationX = (1f - drawIn.value) * Move.titleSlide.toPx() * slideSign
                            },
                        )
                    }
                },
                actions = {
                    AppIconButton(onClick = onCast) {
                        Icon(
                            if (connected) Icons.Filled.CastConnected else Icons.Filled.Cast,
                            contentDescription = stringResource(R.string.cd_connect),
                            tint = if (connected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    AppIconButton(onClick = { onNavigate(Routes.SETTINGS) }, turn = true) {
                        Icon(Icons.Filled.Settings, contentDescription = stringResource(R.string.settings_title))
                    }
                },
            )
        },
    ) { padding ->
        LazyColumn(Modifier.fillMaxSize().padding(padding), contentPadding = listEnd()) {
            item {
                Text(greeting, style = MaterialTheme.typography.headlineMedium, modifier = Modifier.padding(horizontal = Space.l, vertical = Space.s))
            }
            item {
                Row(Modifier.fillMaxWidth().padding(horizontal = Space.l), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    val c = MaterialTheme.colorScheme
                    CompositionLocalProvider(LocalContentColor provides c.onPrimaryContainer) {
                        // Flow plays at once, endlessly; while it plays, the tile pauses it.
                        val flowing = from == player.flowContext()
                        QuickButton("Flow", if (flowing) Icons.Filled.Pause else Icons.Filled.Stream, c.primaryContainer, c.tertiaryContainer, Modifier.weight(1f), enabled = !offline) { player.playFlow() }
                        QuickButton(stringResource(R.string.charts_title), Icons.Filled.BarChart, c.secondaryContainer, c.primaryContainer, Modifier.weight(1f)) { onNavigate(Routes.CHARTS) }
                    }
                }
            }
            // Flow's moods, each one tap as Flow; the one playing is marked.
            if (moods.isNotEmpty()) item {
                LazyRow(contentPadding = PaddingValues(horizontal = Space.l, vertical = Space.s), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    items(moods, key = { it.first }) { (id, title) ->
                        AppChip(selected = from == player.flowContext(id), onClick = { player.playFlow(id) }, label = { Text(title) }, enabled = !offline)
                    }
                }
            }
            when (load) {
                is Load.Ready -> sections(load.value, player, onNavigate)
                else -> item { LoadView(load, retry, Icons.Filled.Home, "", Modifier.fillParentMaxHeight(0.5f)) {} }
            }
        }
    }
}

private fun LazyListScope.sections(data: HomeData, player: PlayerController, onNavigate: (String) -> Unit) {
    val context = ListenContext("tops_track", "0")
    if (data.playlists.isNotEmpty()) {
        item { SectionHeader(stringResource(R.string.section_your_playlists)) }
        item {
            CardRow(data.playlists, { it.id }) { pl ->
                MediaCard(
                    title = pl.name,
                    subtitle = if (pl.trackCount > 0) pluralStringResource(R.plurals.n_tracks, pl.trackCount, pl.trackCount) else pl.owner,
                    artworkUrl = pl.artworkUrl,
                    onClick = { onNavigate(Routes.playlist(pl.id, pl.name)) },
                )
            }
        }
    }
    if (data.topAlbums.isNotEmpty()) {
        item { SectionHeader(stringResource(R.string.section_albums)) }
        item {
            CardRow(data.topAlbums, { it.id }) { a ->
                MediaCard(a.name, a.artistLine, a.artworkUrl, onClick = { onNavigate(Routes.album(a.id, a.name)) })
            }
        }
    }
    if (data.topTracks.isNotEmpty()) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                SectionHeader(stringResource(R.string.section_top_tracks), Modifier.weight(1f))
                TextButton(onClick = { onNavigate(Routes.CHARTS) }, modifier = Modifier.padding(end = Space.s)) {
                    Text(stringResource(R.string.action_show_all))
                }
            }
        }
        itemsIndexed(data.topTracks.take(TOP_ROWS), key = { _, t -> "t-${t.id}" }) { i, t ->
            TrackRow(t, player, context, onClick = { player.playQueue(data.topTracks, i, context) })
        }
    }
}


// A tile washed from one container color into another, the label and its glyph on it.
@Composable
private fun QuickButton(label: String, icon: ImageVector, from: Color, to: Color, modifier: Modifier, enabled: Boolean = true, onClick: () -> Unit) {
    val shape = MaterialTheme.shapes.large
    Row(
        modifier
            .heightIn(min = Size.quickTile)
            .alpha(if (enabled) 1f else DISABLED_ALPHA)
            .clip(shape)
            .background(Brush.linearGradient(listOf(from, to)))
            .clickable(enabled = enabled, onClick = onClick, role = Role.Button)
            .padding(horizontal = Space.l),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, modifier = Modifier.padding(end = Space.s).size(ButtonDefaults.IconSize))
        Text(label, style = MaterialTheme.typography.titleSmall)
    }
}
