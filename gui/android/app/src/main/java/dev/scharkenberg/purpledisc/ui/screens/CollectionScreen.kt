package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import dev.scharkenberg.purpledisc.ui.components.GradientButton
import androidx.compose.foundation.interaction.MutableInteractionSource
import dev.scharkenberg.purpledisc.ui.components.pressScale
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.PlaylistPlay
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.DownloadForOffline
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import androidx.compose.material3.IconToggleButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
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
import dev.scharkenberg.purpledisc.data.Library
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.Playlist
import dev.scharkenberg.purpledisc.engine.Track
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.ActionSheet
import dev.scharkenberg.purpledisc.ui.components.Artwork
import dev.scharkenberg.purpledisc.ui.components.ButtonIcon
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.LocalOfflineMode
import dev.scharkenberg.purpledisc.ui.components.MoreButton
import dev.scharkenberg.purpledisc.ui.components.SheetItem
import dev.scharkenberg.purpledisc.ui.components.TrackRow
import dev.scharkenberg.purpledisc.ui.components.formatTotal
import dev.scharkenberg.purpledisc.ui.components.idKeys
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.components.shareLink
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space
import kotlinx.coroutines.launch

/** An album, a playlist, Liked songs or Flow ([kind]): a header, then the tracks. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CollectionScreen(kind: String, id: String, title: String, player: PlayerController, onBack: () -> Unit) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = {},
                navigationIcon = {
                    AppIconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = stringResource(R.string.action_back))
                    }
                },
            )
        },
    ) { padding ->
        CollectionContent(kind, id, title, player, Modifier.fillMaxSize().padding(padding))
    }
}

/** The page body without its bar, for the fold's book layout too. */
@Composable
fun CollectionContent(kind: String, id: String, title: String, player: PlayerController, modifier: Modifier = Modifier) {
    val offlineMode = LocalOfflineMode.current
    val context = when (kind) {
        "album" -> ListenContext("album_page", id)
        "playlist" -> ListenContext("playlist_page", id)
        else -> ListenContext("profile_loved", Engine.userId())
    }
    val pinnable = kind == "album" || kind == "playlist"
    val (load, retry) = rememberLoad(if (offlineMode && pinnable) "$kind:$id:offline" else "$kind:$id") {
        when {
            offlineMode && pinnable -> Engine.offlineTracksOrNull(kind, id)
            kind == "album" -> Engine.albumTracksOrNull(id)
            kind == "playlist" -> Engine.playlistTracksOrNull(id)
            else -> Engine.favoritesOrNull()
        }
    }
    // The account's playlists tell an own playlist (rename, remove a track) and its cover.
    @Suppress("UNCHECKED_CAST")
    val meta by produceState(Library.peek("playlists") as List<Playlist>?, kind) {
        if (kind == "playlist" && value == null) value = Engine.playlistsOrNull()?.also { Library.store("playlists", it) }
    }
    val playlist = meta?.takeIf { kind == "playlist" }?.firstOrNull { it.id == id }
    val ownId = playlist?.takeIf { it.ownerId.isNotEmpty() && it.ownerId == Engine.userId() }?.id
    LoadView(load, retry, Icons.AutoMirrored.Filled.QueueMusic, stringResource(R.string.tracklist_empty), modifier, isEmpty = { it.isEmpty() }) { tracks ->
        val keys = remember(tracks) { idKeys(tracks) { it.id } }
        val header = @Composable {
            CollectionHeader(kind, id, title, playlist?.artworkUrl ?: tracks.first().artworkUrl, subtitle(kind, playlist, tracks), tracks, player, context, own = ownId != null)
        }
        // A wide window: the header holds still on the left while the tracks scroll on the right.
        Box(modifier) {
            if (LocalWindowLayout.current.twoPane) {
                Row(Modifier.fillMaxSize()) {
                    Column(Modifier.weight(0.4f).fillMaxHeight().verticalScroll(rememberScrollState())) { header() }
                    LazyColumn(Modifier.weight(0.6f).fillMaxHeight().padding(top = Space.l), contentPadding = listEnd()) {
                        itemsIndexed(tracks, key = { i, _ -> keys[i] }) { i, t ->
                            TrackRow(t, player, context, onClick = { player.playQueue(tracks, i, context) }, playlistId = ownId)
                        }
                    }
                }
            } else {
                LazyColumn(Modifier.fillMaxSize(), contentPadding = listEnd()) {
                    item { header() }
                    itemsIndexed(tracks, key = { i, _ -> keys[i] }) { i, t ->
                        TrackRow(t, player, context, onClick = { player.playQueue(tracks, i, context) }, playlistId = ownId)
                    }
                }
            }
        }
    }
}

@Composable
private fun subtitle(kind: String, playlist: Playlist?, tracks: List<Track>): String {
    val by = when (kind) {
        "album" -> tracks.first().artistLine
        "playlist" -> playlist?.owner.orEmpty()
        else -> ""
    }
    val count = pluralStringResource(R.plurals.n_tracks, tracks.size, tracks.size)
    return listOf(by, count, formatTotal(tracks.sumOf { it.durationMs } / 1000)).filter { it.isNotBlank() }.joinToString(" · ")
}

@Composable
private fun CollectionHeader(
    kind: String,
    id: String,
    title: String,
    cover: String,
    subtitle: String,
    tracks: List<Track>,
    player: PlayerController,
    context: ListenContext,
    own: Boolean,
) {
    val offlineMode = LocalOfflineMode.current
    val scope = rememberCoroutineScope()
    val pinnable = kind == "album" || kind == "playlist"
    var favorite by remember { mutableStateOf<Boolean?>(null) }
    var pinned by remember { mutableStateOf(false) }
    var more by remember { mutableStateOf(false) }
    LaunchedEffect(kind, id) {
        if (!pinnable) return@LaunchedEffect
        if (!offlineMode && !own) favorite = Engine.isFavorite(kind, id).takeIf { it >= 0 }?.let { it == 1 }
        pinned = Engine.offlineCollections()?.any { it.kind == kind && it.id == id } == true
    }
    Column(Modifier.fillMaxWidth().padding(Space.l), horizontalAlignment = Alignment.CenterHorizontally) {
        Artwork(cover, Modifier.size(Size.hero), shape = MaterialTheme.shapes.large, shown = Size.hero)
        Spacer(Modifier.height(Space.l))
        Text(title.ifBlank { stringResource(R.string.unknown_title) }, style = MaterialTheme.typography.headlineSmall, textAlign = TextAlign.Center)
        Text(subtitle, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        Spacer(Modifier.height(Space.l))
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
            GradientButton(onClick = { player.playQueue(tracks, 0, context, shuffle = false) }, modifier = Modifier.weight(1f)) {
                ButtonIcon(Icons.Filled.PlayArrow)
                Text(stringResource(R.string.action_play))
            }
            GradientButton(tonal = true, onClick = { player.playQueue(tracks, tracks.indices.random(), context, shuffle = true) }, modifier = Modifier.weight(1f)) {
                ButtonIcon(Icons.Filled.Shuffle)
                Text(stringResource(R.string.btn_shuffle))
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            favorite?.let { on ->
                val likePress = remember { MutableInteractionSource() }
                IconToggleButton(checked = on, modifier = Modifier.pressScale(likePress), interactionSource = likePress, onCheckedChange = {
                    favorite = !on
                    scope.launch {
                        // The library's kept list of this kind is stale once it took.
                        if (Engine.setFavorite(kind, id, !on)) Library.drop(if (kind == "album") "library:album" else "playlists") else favorite = on
                    }
                }) {
                    Icon(
                        if (on) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                        contentDescription = stringResource(if (on) R.string.menu_remove_favorite else R.string.menu_add_favorite),
                    )
                }
            }
            if (pinnable && player.premium && (pinned || !offlineMode)) {
                val pinPress = remember { MutableInteractionSource() }
                IconToggleButton(checked = pinned, modifier = Modifier.pressScale(pinPress), interactionSource = pinPress, onCheckedChange = { scope.launch { if (player.pinCollection(kind, id, title, !pinned)) pinned = !pinned } }) {
                    Icon(
                        if (pinned) Icons.Filled.DownloadDone else Icons.Filled.DownloadForOffline,
                        contentDescription = stringResource(if (pinned) R.string.offline_remove else R.string.offline_keep),
                    )
                }
            }
            MoreButton { more = true }
        }
    }
    if (more) CollectionSheet(kind, id, title, tracks, player, context, onDismiss = { more = false })
}

// Play next, Add to queue, Share, and the premium download of an album or playlist.
@Composable
private fun CollectionSheet(kind: String, id: String, title: String, tracks: List<Track>, player: PlayerController, context: ListenContext, onDismiss: () -> Unit) {
    val ctx = LocalContext.current
    val offlineMode = LocalOfflineMode.current
    ActionSheet(onDismiss) { close ->
        SheetItem(Icons.AutoMirrored.Filled.PlaylistPlay, stringResource(R.string.action_play_next)) {
            close { player.playNext(tracks, context) }
        }
        SheetItem(Icons.AutoMirrored.Filled.QueueMusic, stringResource(R.string.action_add_to_queue)) {
            close { player.addToQueue(tracks, context) }
        }
        if (kind == "album" || kind == "playlist") {
            SheetItem(Icons.Filled.Share, stringResource(R.string.menu_share)) { close { shareLink(ctx, kind, id) } }
            if (player.premium && !offlineMode) {
                val label = stringResource(if (kind == "album") R.string.action_download_album else R.string.action_download_playlist)
                SheetItem(Icons.Filled.Download, label) {
                    close { if (kind == "album") player.downloadAlbum(id, title) else player.downloadPlaylist(id, title) }
                }
            }
        }
    }
}
