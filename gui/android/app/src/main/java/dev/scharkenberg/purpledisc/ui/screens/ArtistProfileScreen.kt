package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import dev.scharkenberg.purpledisc.ui.components.GradientButton
import androidx.compose.foundation.interaction.MutableInteractionSource
import dev.scharkenberg.purpledisc.ui.components.pressScale
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Block
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Radio
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.Icon
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import androidx.compose.material3.IconToggleButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.LocalWindowLayout
import dev.scharkenberg.purpledisc.Routes
import dev.scharkenberg.purpledisc.engine.ArtistPage
import dev.scharkenberg.purpledisc.data.Library
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.ActionSheet
import dev.scharkenberg.purpledisc.ui.components.Artwork
import dev.scharkenberg.purpledisc.ui.components.ButtonIcon
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.LocalOfflineMode
import dev.scharkenberg.purpledisc.ui.components.MediaCard
import dev.scharkenberg.purpledisc.ui.components.MoreButton
import dev.scharkenberg.purpledisc.ui.components.SectionHeader
import dev.scharkenberg.purpledisc.ui.components.SheetItem
import dev.scharkenberg.purpledisc.ui.components.TrackRow
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.components.shareLink
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space
import kotlinx.coroutines.launch

/** An artist: round header, top tracks, albums and related artists, from one engine call. */
@Composable
fun ArtistProfileScreen(artistId: String, artistName: String, player: PlayerController, onBack: () -> Unit, onNavigate: (String) -> Unit) {
    val (load, retry) = rememberLoad("artist:$artistId") { Engine.artistProfile(artistId) }
    Scaffold(topBar = { PageBar("", onBack) }) { padding ->
        LoadView(load, retry, Icons.Filled.Person, stringResource(R.string.artist_load_error), Modifier.padding(padding)) { page ->
            val context = ListenContext("artist_top", artistId)
            // A wide window: the artist holds still on the left, the music scrolls on the right.
            val split = LocalWindowLayout.current.twoPane
            Row(Modifier.fillMaxSize().padding(padding)) {
            if (split) {
                Column(Modifier.weight(0.38f).fillMaxHeight().verticalScroll(rememberScrollState())) { ArtistHeader(page, artistName, player, context) }
            }
            LazyColumn(Modifier.weight(if (split) 0.62f else 1f).fillMaxHeight(), contentPadding = listEnd()) {
                if (!split) item { ArtistHeader(page, artistName, player, context) }
                if (page.top.isNotEmpty()) {
                    item { SectionHeader(stringResource(R.string.section_top_tracks)) }
                    itemsIndexed(page.top, key = { _, t -> t.id }) { i, t ->
                        TrackRow(t, player, context, onClick = { player.playQueue(page.top, i, context) })
                    }
                }
                if (page.albums.isNotEmpty()) {
                    item { SectionHeader(stringResource(R.string.section_albums)) }
                    item { CardRow(page.albums, { it.id }) { a -> MediaCard(a.name, a.artistLine, a.artworkUrl, { onNavigate(Routes.album(a.id, a.name)) }) } }
                }
                if (page.related.isNotEmpty()) {
                    item { SectionHeader(stringResource(R.string.section_related_artists)) }
                    item {
                        CardRow(page.related, { it.id }) { a ->
                            val fans = if (a.nbFans > 0) pluralStringResource(R.plurals.n_fans, a.nbFans, a.nbFans) else ""
                            MediaCard(a.name, fans, a.artworkUrl, { onNavigate(Routes.artist(a.id, a.name)) }, round = true)
                        }
                    }
                }
            }
            }
        }
    }
}

@Composable
private fun ArtistHeader(page: ArtistPage, fallbackName: String, player: PlayerController, context: ListenContext) {
    val a = page.artist
    val scope = rememberCoroutineScope()
    val offlineMode = LocalOfflineMode.current
    var follows by remember { mutableStateOf<Boolean?>(null) }
    var more by remember { mutableStateOf(false) }
    LaunchedEffect(a.id) { if (!offlineMode) follows = Engine.isFavorite("artist", a.id).takeIf { it >= 0 }?.let { it == 1 } }
    Column(Modifier.fillMaxWidth().padding(Space.l), horizontalAlignment = Alignment.CenterHorizontally) {
        Artwork(a.artworkUrl, Modifier.size(Size.hero), shape = CircleShape, shown = Size.hero)
        Spacer(Modifier.height(Space.l))
        Text(a.name.ifBlank { fallbackName }, style = MaterialTheme.typography.headlineSmall, textAlign = TextAlign.Center)
        if (a.nbFans > 0) {
            Text(pluralStringResource(R.plurals.n_fans, a.nbFans, a.nbFans), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (page.top.isNotEmpty()) {
            Spacer(Modifier.height(Space.l))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                GradientButton(onClick = { player.playQueue(page.top, 0, context, shuffle = false) }, modifier = Modifier.weight(1f)) {
                    ButtonIcon(Icons.Filled.PlayArrow)
                    Text(stringResource(R.string.action_play))
                }
                GradientButton(tonal = true, onClick = { player.playQueue(page.top, page.top.indices.random(), context, shuffle = true) }, modifier = Modifier.weight(1f)) {
                    ButtonIcon(Icons.Filled.Shuffle)
                    Text(stringResource(R.string.btn_shuffle))
                }
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            follows?.let { on ->
                val press = remember { MutableInteractionSource() }
                IconToggleButton(checked = on, modifier = Modifier.pressScale(press), interactionSource = press, onCheckedChange = {
                    follows = !on
                    scope.launch { if (Engine.setFavorite("artist", a.id, !on)) Library.drop("library:artist") else follows = on }
                }) {
                    Icon(
                        if (on) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                        contentDescription = stringResource(if (on) R.string.menu_remove_favorite else R.string.menu_add_favorite),
                    )
                }
            }
            if (!offlineMode) {
                AppIconButton(onClick = { player.startArtistRadio(a.id) }) {
                    Icon(Icons.Filled.Radio, contentDescription = stringResource(R.string.action_start_radio))
                }
            }
            MoreButton { more = true }
        }
    }
    if (more) {
        val ctx = LocalContext.current
        ActionSheet(onDismiss = { more = false }) { close ->
            SheetItem(Icons.Filled.Share, stringResource(R.string.menu_share)) { close { shareLink(ctx, "artist", a.id) } }
            if (!offlineMode) {
                SheetItem(Icons.Filled.Block, stringResource(R.string.ban_artist)) {
                    close { scope.launch { Engine.setRecommendationBan("artist", a.id, true) } }
                }
            }
        }
    }
}
