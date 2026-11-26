package dev.scharkenberg.purpledisc.ui.components

import android.content.Context
import android.content.Intent
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.PlaylistAdd
import androidx.compose.material.icons.automirrored.filled.PlaylistPlay
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Block
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.DownloadForOffline
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Album
import androidx.compose.material.icons.filled.Radio
import androidx.compose.material.icons.filled.RemoveCircleOutline
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.data.Library
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.Playlist
import dev.scharkenberg.purpledisc.engine.Track
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/** What a track sheet is opened for: [playlistId] is the own playlist the row is in, [queueUid] its queue row. */
data class TrackSheetRequest(
    val track: Track,
    val context: ListenContext,
    val playlistId: String? = null,
    val queueUid: Long? = null,
)

/** Shares a deezer.com link (kind: track, album, playlist, artist, show, episode). */
fun shareLink(context: Context, kind: String, id: String) {
    val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, "https://www.deezer.com/$kind/$id")
    context.startActivity(Intent.createChooser(send, null))
}

/** One action of a sheet. */
@Composable
fun SheetItem(icon: ImageVector, label: String, onClick: () -> Unit) {
    ListItem(
        onClick = onClick,
        leadingContent = { Icon(icon, contentDescription = null) },
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        modifier = Modifier.fillMaxWidth(),
    ) { Text(label) }
}

/** A sheet with [header] and its actions; each action closes it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ActionSheet(onDismiss: () -> Unit, header: @Composable () -> Unit = {}, content: @Composable (close: (() -> Unit) -> Unit) -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, containerColor = MaterialTheme.colorScheme.surfaceContainerLow) {
        Column(Modifier.verticalScroll(rememberScrollState()).navigationBarsPadding()) {
            header()
            content { action ->
                onDismiss()
                action()
            }
        }
    }
}

/**
 * The actions on a track, in the Windows app's order; an action a free account or offline
 * mode cannot use is left out. [onMessage] shows a result line. [scope] outlives the sheet,
 * which closes before an action's engine call returns.
 */
@Composable
fun TrackSheet(
    request: TrackSheetRequest,
    player: PlayerController,
    offlineMode: Boolean,
    onDismiss: () -> Unit,
    onArtist: (id: String, name: String) -> Unit,
    onAlbum: (id: String, name: String) -> Unit,
    onMessage: (String) -> Unit,
    scope: CoroutineScope,
) {
    val t = request.track
    val context = LocalContext.current
    val liked by player.likedIds.collectAsState()
    val offlineIds by player.offlineIds.collectAsState()
    var pickPlaylist by remember { mutableStateOf(false) }
    val net = !offlineMode
    val heard by produceState<Boolean?>(null, t.id) { if (t.isEpisode) value = Engine.bookmarks()[t.id]?.heard ?: false }
    if (pickPlaylist) {
        PlaylistPicker(t, scope, onDismiss = onDismiss, onMessage = onMessage)
        return
    }
    ActionSheet(onDismiss, header = { SheetHeader(t) }) { close ->
        if (request.queueUid == null) {
            SheetItem(Icons.AutoMirrored.Filled.PlaylistPlay, stringResource(R.string.action_play_next)) { close { player.playNext(listOf(t), request.context) } }
            SheetItem(Icons.AutoMirrored.Filled.QueueMusic, stringResource(R.string.action_add_to_queue)) { close { player.addToQueue(listOf(t), request.context) } }
        }
        if (t.isEpisode) {
            heard?.let { h ->
                SheetItem(Icons.Filled.CheckCircle, stringResource(if (h) R.string.episode_mark_unplayed else R.string.episode_mark_played)) {
                    close { scope.launch { Engine.markEpisodePlayed(t.id, !h) } }
                }
            }
        } else if (net) {
            val isLiked = t.id in liked
            SheetItem(if (isLiked) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder, stringResource(if (isLiked) R.string.menu_unlike else R.string.menu_like)) {
                close { player.toggleFavorite(t) }
            }
            SheetItem(Icons.AutoMirrored.Filled.PlaylistAdd, stringResource(R.string.menu_add_to_playlist)) { pickPlaylist = true }
            request.playlistId?.let { pl ->
                SheetItem(Icons.Filled.RemoveCircleOutline, stringResource(R.string.menu_remove_from_playlist)) {
                    close {
                        scope.launch {
                            if (Engine.removeFromPlaylist(pl, t.id)) Library.drop("playlist:$pl") else onMessage(context.getString(R.string.load_failed))
                        }
                    }
                }
            }
        }
        request.queueUid?.let { uid -> SheetItem(Icons.Filled.Delete, stringResource(R.string.queue_remove)) { close { player.removeFromQueue(uid) } } }
        if (!t.isEpisode && net) {
            t.artists.firstOrNull()?.let { a -> SheetItem(Icons.Filled.Person, stringResource(R.string.menu_go_to_artist)) { close { onArtist(a.id, a.name) } } }
            // Not on that album's own page.
            val onAlbumPage = request.queueUid == null && request.context == ListenContext("album_page", t.albumId)
            if (t.albumId.isNotEmpty() && !onAlbumPage) {
                SheetItem(Icons.Filled.Album, stringResource(R.string.menu_go_to_album)) { close { onAlbum(t.albumId, t.albumName) } }
            }
            SheetItem(Icons.Filled.Radio, stringResource(R.string.action_start_radio)) { close { player.startTrackRadio(t) } }
        }
        SheetItem(Icons.Filled.Share, stringResource(R.string.menu_share)) { close { shareLink(context, if (t.isEpisode) "episode" else "track", t.id) } }
        if (!t.isEpisode && net) {
            SheetItem(Icons.Filled.Block, stringResource(R.string.ban_track)) {
                close {
                    scope.launch {
                        onMessage(context.getString(if (Engine.setRecommendationBan("track", t.id, true)) R.string.ban_done else R.string.load_failed))
                    }
                }
            }
            if (player.premium) {
                SheetItem(Icons.Filled.Download, stringResource(R.string.action_download)) { close { player.download(t) } }
            }
        }
        if (!t.isEpisode && player.premium) {
            if (t.id in offlineIds) {
                SheetItem(Icons.Filled.DownloadDone, stringResource(R.string.offline_remove)) {
                    close {
                        scope.launch {
                            if (Engine.unpinOffline("track", t.id).isEmpty()) player.refreshOfflineIds() else onMessage(context.getString(R.string.load_failed))
                        }
                    }
                }
            } else if (net) {
                SheetItem(Icons.Filled.DownloadForOffline, stringResource(R.string.offline_keep)) { close { player.downloadForOffline(t) } }
            }
        }
    }
}

@Composable
private fun SheetHeader(t: Track) {
    Row(Modifier.fillMaxWidth().padding(horizontal = Space.l, vertical = Space.s), verticalAlignment = Alignment.CenterVertically) {
        Artwork(t.artworkUrl, Modifier.size(Size.rowArt), shown = Size.rowArt)
        Spacer(Modifier.width(Space.m))
        Column(Modifier.weight(1f)) {
            MarqueeText(t.name, style = MaterialTheme.typography.titleMedium)
            MarqueeText(t.artistLine, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
    HorizontalDivider()
}

// The account's playlists, and a new one, to add [t] to.
@Composable
private fun PlaylistPicker(t: Track, scope: CoroutineScope, onDismiss: () -> Unit, onMessage: (String) -> Unit) {
    val context = LocalContext.current
    var naming by remember { mutableStateOf(false) }
    val (load, retry) = rememberLoad("playlists") { Engine.playlistsOrNull() }
    fun add(p: Playlist) = scope.launch {
        val ok = Engine.addToPlaylist(p.id, t.id)
        if (ok) Library.drop("playlist:${p.id}")
        onMessage(if (ok) p.name else context.getString(R.string.load_failed))
    }
    if (naming) {
        NameDialog(stringResource(R.string.dialog_new_playlist), "", onDismiss) { title ->
            scope.launch {
                val id = Engine.createPlaylist(title)
                if (id != null && Engine.addToPlaylist(id, t.id)) {
                    Library.drop("playlists")
                    onMessage(title)
                } else {
                    onMessage(context.getString(R.string.load_failed))
                }
            }
        }
        return
    }
    ActionSheet(onDismiss, header = { SheetHeader(t) }) { close ->
        SheetItem(Icons.Filled.Add, stringResource(R.string.menu_new_playlist)) { naming = true }
        LoadView(load, retry, Icons.AutoMirrored.Filled.QueueMusic, stringResource(R.string.playlists_empty)) { list ->
            Column { list.forEach { p -> SheetItem(Icons.AutoMirrored.Filled.QueueMusic, p.name) { close { add(p) } } } }
        }
    }
}

/** A dialog asking for a name (a new or renamed playlist). */
@Composable
fun NameDialog(title: String, initial: String, onDismiss: () -> Unit, onDone: (String) -> Unit) {
    var name by remember { mutableStateOf(initial) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { OutlinedTextField(name, { name = it }, label = { Text(stringResource(R.string.playlist_title_label)) }, singleLine = true) },
        confirmButton = {
            TextButton(onClick = { onDismiss(); onDone(name.trim()) }, enabled = name.isNotBlank()) {
                Text(stringResource(if (initial.isEmpty()) R.string.action_create else R.string.action_save))
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.action_cancel)) } },
    )
}
