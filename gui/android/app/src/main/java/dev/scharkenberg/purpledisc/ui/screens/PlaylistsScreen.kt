package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.AppChip
import dev.scharkenberg.purpledisc.ui.components.listEnd
import android.graphics.Rect
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.automirrored.filled.Sort
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import dev.scharkenberg.purpledisc.ui.components.Load
import dev.scharkenberg.purpledisc.ui.components.MediaCard
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import dev.scharkenberg.purpledisc.ui.components.MarqueeText
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.Routes
import dev.scharkenberg.purpledisc.data.Library
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.Playlist
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.HingeSplit
import dev.scharkenberg.purpledisc.ui.components.ActionSheet
import dev.scharkenberg.purpledisc.ui.components.Artwork
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.LocalOfflineMode
import dev.scharkenberg.purpledisc.ui.components.MoreButton
import dev.scharkenberg.purpledisc.ui.components.NameDialog
import dev.scharkenberg.purpledisc.ui.components.SheetItem
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/** A top bar with a back arrow, the frame of every Library page. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PageBar(title: String, onBack: () -> Unit, badge: Int? = null, actions: @Composable () -> Unit = {}) {
    TopAppBar(
        title = {
            // A count beside the title, as Deezer's website shows it.
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                MarqueeText(title, Modifier.weight(1f, fill = false))
                if (badge != null) {
                    Surface(color = MaterialTheme.colorScheme.secondaryContainer, shape = MaterialTheme.shapes.small) {
                        Text(badge.toString(), style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(horizontal = Space.s, vertical = Space.xs))
                    }
                }
            }
        },
        navigationIcon = {
            AppIconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = stringResource(R.string.action_back))
            }
        },
        actions = { actions() },
    )
}

/** A list row with a cover: Library lists, the book layout's left page. */
@Composable
fun CoverRow(
    title: String,
    subtitle: String,
    artworkUrl: String?,
    onClick: () -> Unit,
    selected: Boolean = false,
    round: Boolean = false,
    trailing: @Composable () -> Unit = {},
) {
    ListItem(
        selected = selected,
        onClick = onClick,
        supportingContent = if (subtitle.isBlank()) null else { { Text(subtitle, maxLines = 1, overflow = TextOverflow.Ellipsis) } },
        leadingContent = {
            Artwork(artworkUrl, Modifier.size(Size.rowArt), shape = if (round) CircleShape else MaterialTheme.shapes.small, shown = Size.rowArt)
        },
        trailingContent = trailing,
    ) { Text(title.ifBlank { stringResource(R.string.unknown_title) }, maxLines = 1, overflow = TextOverflow.Ellipsis) }
}

// A colored square with an icon and a title, in the grid's card size: Liked songs, history.
@Composable
private fun ShortcutTile(icon: ImageVector, title: String, color: Color, onClick: () -> Unit) {
    Column(Modifier.fillMaxWidth().clickable(onClick = onClick)) {
        Surface(color = color, shape = MaterialTheme.shapes.medium, modifier = Modifier.fillMaxWidth().aspectRatio(1f)) {
            Box(contentAlignment = Alignment.Center) { Icon(icon, contentDescription = null, modifier = Modifier.size(Space.xl * 2)) }
        }
        Spacer(Modifier.height(Space.s))
        Text(title, style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun playlistLine(p: Playlist) =
    if (p.trackCount > 0) pluralStringResource(R.plurals.n_tracks, p.trackCount, p.trackCount) else p.owner

/**
 * The account's playlists: a new one, and rename and delete on its own ones. The edits run on
 * the shell's [scope], so Back does not cancel them before the kept list is dropped.
 */
@Composable
fun PlaylistsScreen(onBack: () -> Unit, onNavigate: (String) -> Unit, onMessage: (String) -> Unit, scope: CoroutineScope) {
    val context = LocalContext.current
    val offlineMode = LocalOfflineMode.current
    val (load, retry) = rememberLoad("playlists") { Engine.playlistsOrNull() }
    var creating by remember { mutableStateOf(false) }
    var actionsFor by remember { mutableStateOf<Playlist?>(null) }
    var renaming by remember { mutableStateOf<Playlist?>(null) }
    var deleting by remember { mutableStateOf<Playlist?>(null) }
    // A change drops the kept list, which loads it again.
    fun changed(ok: Boolean) {
        if (ok) {
            Library.drop("playlists")
        } else {
            onMessage(context.getString(R.string.load_failed))
        }
    }
    var query by rememberSaveable { mutableStateOf("") }
    var owner by rememberSaveable { mutableIntStateOf(0) } // 0 all, 1 the account's own, 2 others'
    var byName by rememberSaveable { mutableStateOf(false) }
    val count = (load as? Load.Ready)?.value?.size
    Scaffold(
        topBar = {
            PageBar(stringResource(R.string.playlists_title), onBack, badge = count) {
                if (!offlineMode) {
                    AppIconButton(onClick = { creating = true }) {
                        Icon(Icons.Filled.Add, contentDescription = stringResource(R.string.menu_new_playlist))
                    }
                }
            }
        },
    ) { padding ->
        val me = Engine.userId()
        LoadView(load, retry, Icons.AutoMirrored.Filled.QueueMusic, stringResource(R.string.playlists_empty), Modifier.padding(padding), isEmpty = { it.isEmpty() }) { all ->
            fun own(p: Playlist) = p.ownerId.isNotEmpty() && p.ownerId == me
            // The owner chips only when both kinds are there, as on Windows.
            val both = all.any(::own) && all.any { !own(it) }
            val shown = remember(all, query, owner, byName, both) {
                all.filter { p ->
                    (!both || owner == 0 || own(p) == (owner == 1)) &&
                        (query.isBlank() || p.name.contains(query, ignoreCase = true) || p.owner.contains(query, ignoreCase = true))
                }.let { if (byName) it.sortedBy { p -> p.name.lowercase() } else it }
            }
            LazyVerticalGrid(
                GridCells.Adaptive(Size.card),
                Modifier.fillMaxSize().padding(padding).padding(horizontal = Space.l),
                contentPadding = listEnd(),
                horizontalArrangement = Arrangement.spacedBy(Space.m),
                verticalArrangement = Arrangement.spacedBy(Space.l),
            ) {
                item(span = { GridItemSpan(maxLineSpan) }) {
                    Column(verticalArrangement = Arrangement.spacedBy(Space.s)) {
                        OutlinedTextField(
                            query,
                            { query = it },
                            placeholder = { Text(stringResource(R.string.list_search)) },
                            leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null) },
                            singleLine = true,
                            shape = MaterialTheme.shapes.large,
                            modifier = Modifier.fillMaxWidth(),
                        )
                        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                            if (both) {
                                listOf(R.string.filter_all, R.string.filter_by_you, R.string.filter_by_others).forEachIndexed { i, label ->
                                    AppChip(selected = owner == i, onClick = { owner = i }, label = { Text(stringResource(label)) })
                                }
                            }
                            AppChip(
                                selected = byName,
                                onClick = { byName = !byName },
                                label = { Text(stringResource(if (byName) R.string.sort_title_az else R.string.sort_added)) },
                                leadingIcon = { Icon(Icons.AutoMirrored.Filled.Sort, contentDescription = null, modifier = Modifier.size(FilterChipDefaults.IconSize)) },
                            )
                        }
                    }
                }
                // Liked songs and the listening history lead, as on Deezer's website.
                if (query.isBlank() && owner == 0) {
                    item { ShortcutTile(Icons.Filled.Favorite, stringResource(R.string.home_liked), MaterialTheme.colorScheme.primaryContainer) { onNavigate(Routes.LIKED) } }
                    item { ShortcutTile(Icons.Filled.History, stringResource(R.string.section_recently_played), MaterialTheme.colorScheme.tertiaryContainer) { onNavigate(Routes.HISTORY) } }
                }
                items(shown, key = { it.id }) { p ->
                    MediaCard(
                        p.name,
                        listOf(p.owner, playlistLine(p)).filter { it.isNotBlank() }.distinct().joinToString(" · "),
                        p.artworkUrl,
                        onClick = { onNavigate(Routes.playlist(p.id, p.name)) },
                        onLongClick = if (!offlineMode && own(p)) { { actionsFor = p } } else null,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                if (shown.isEmpty()) {
                    item(span = { GridItemSpan(maxLineSpan) }) {
                        Text(stringResource(R.string.list_no_match), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(vertical = Space.xl))
                    }
                }
            }
        }
    }
    actionsFor?.let { p ->
        ActionSheet(onDismiss = { actionsFor = null }) { close ->
            SheetItem(Icons.Filled.Edit, stringResource(R.string.action_rename)) { close { renaming = p } }
            SheetItem(Icons.Filled.Delete, stringResource(R.string.action_delete)) { close { deleting = p } }
        }
    }
    if (creating) {
        NameDialog(stringResource(R.string.dialog_new_playlist), "", onDismiss = { creating = false }) { title ->
            scope.launch { changed(Engine.createPlaylist(title) != null) }
        }
    }
    renaming?.let { p ->
        NameDialog(stringResource(R.string.action_rename), p.name, onDismiss = { renaming = null }) { title ->
            scope.launch { changed(Engine.renamePlaylist(p.id, title)) }
        }
    }
    deleting?.let { p ->
        AlertDialog(
            onDismissRequest = { deleting = null },
            title = { Text(stringResource(R.string.dialog_delete_playlist)) },
            text = { Text(stringResource(R.string.dialog_delete_playlist_body, p.name)) },
            confirmButton = {
                TextButton(onClick = {
                    deleting = null
                    scope.launch { changed(Engine.deletePlaylist(p.id)) }
                }) { Text(stringResource(R.string.action_delete)) }
            },
            dismissButton = { TextButton(onClick = { deleting = null }) { Text(stringResource(R.string.action_cancel)) } },
        )
    }
}

/**
 * Book posture (half-opened, vertical hinge): Liked songs and the playlists on the left
 * page, the selected one on the right page, split at the hinge.
 */
@Composable
fun PlaylistsBookScreen(player: PlayerController, hingeBounds: Rect, onBack: () -> Unit) {
    val (load, retry) = rememberLoad("playlists") { Engine.playlistsOrNull() }
    // null selects Liked songs, listed first.
    var selected by remember { mutableStateOf<Playlist?>(null) }
    Scaffold(topBar = { PageBar(stringResource(R.string.playlists_title), onBack) }) { padding ->
        HingeSplit(
            hingeBounds = hingeBounds,
            horizontalHinge = false,
            modifier = Modifier.fillMaxSize().padding(padding),
            first = {
                LoadView(load, retry, Icons.AutoMirrored.Filled.QueueMusic, stringResource(R.string.playlists_empty)) { list ->
                    LazyColumn(Modifier.fillMaxSize(), contentPadding = listEnd()) {
                        item(key = "liked") {
                            ListItem(
                                selected = selected == null,
                                onClick = { selected = null },
                                leadingContent = { Icon(Icons.Filled.Favorite, contentDescription = null) },
                            ) { Text(stringResource(R.string.home_liked)) }
                        }
                        items(list, key = { it.id }) { p ->
                            CoverRow(p.name, playlistLine(p), p.artworkUrl, onClick = { selected = p }, selected = selected?.id == p.id)
                        }
                    }
                }
            },
            second = {
                val sel = selected
                Column(Modifier.fillMaxSize()) {
                    if (sel == null) {
                        CollectionContent("liked", "", stringResource(R.string.home_liked), player)
                    } else {
                        CollectionContent("playlist", sel.id, sel.name, player)
                    }
                }
            },
        )
    }
}
