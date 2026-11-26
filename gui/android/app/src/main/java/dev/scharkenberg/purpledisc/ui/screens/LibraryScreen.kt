package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Album
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Podcasts
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.readableWidth
import dev.scharkenberg.purpledisc.Routes
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.OfflineCollection
import dev.scharkenberg.purpledisc.engine.Track
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.Load
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.SectionHeader
import dev.scharkenberg.purpledisc.ui.components.TrackRow
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.theme.Space

/** The Library tab: one row per page. Offline mode keeps the pages that need no network. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LibraryScreen(offlineMode: Boolean, onNavigate: (String) -> Unit) {
    val rows = buildList {
        if (!offlineMode) {
            add(Triple(Icons.Filled.Favorite, R.string.home_liked, Routes.LIKED))
            add(Triple(Icons.AutoMirrored.Filled.QueueMusic, R.string.playlists_title, Routes.PLAYLISTS))
            add(Triple(Icons.Filled.Album, R.string.section_albums, Routes.ALBUMS))
            add(Triple(Icons.Filled.Person, R.string.section_artists, Routes.ARTISTS))
            add(Triple(Icons.Filled.Podcasts, R.string.podcasts_title, Routes.PODCASTS))
        }
        add(Triple(Icons.Filled.DownloadDone, R.string.library_downloaded, Routes.DOWNLOADED))
        add(Triple(Icons.Filled.History, R.string.section_recently_played, Routes.HISTORY))
    }
    Scaffold(topBar = { TopAppBar(title = { Text(stringResource(R.string.nav_library)) }) }) { padding ->
        LazyColumn(Modifier.fillMaxSize().padding(padding).readableWidth(), contentPadding = listEnd()) {
            items(rows, key = { it.third }) { (icon, label, route) -> LibraryRow(icon, stringResource(label)) { onNavigate(route) } }
        }
    }
}

@Composable
private fun LibraryRow(icon: ImageVector, label: String, onClick: () -> Unit) {
    ListItem(onClick = onClick, leadingContent = { Icon(icon, contentDescription = null) }) { Text(label) }
}

/** The account's favorite albums, artists or podcasts ([kind]). */
@Composable
fun LibraryListScreen(kind: String, onBack: () -> Unit, onNavigate: (String) -> Unit) {
    val title = stringResource(
        when (kind) {
            "album" -> R.string.section_albums
            "artist" -> R.string.section_artists
            else -> R.string.podcasts_title
        },
    )
    val icon = when (kind) {
        "album" -> Icons.Filled.Album
        "artist" -> Icons.Filled.Person
        else -> Icons.Filled.Podcasts
    }
    // One row shape for the three kinds: title, subtitle, cover, route.
    val (load, retry) = rememberLoad("library:$kind") {
        when (kind) {
            "album" -> Engine.libraryAlbums()?.map { listOf(it.name, it.artistLine, it.artworkUrl, Routes.album(it.id, it.name)) }
            "artist" -> Engine.libraryArtists()?.map { listOf(it.name, "", it.artworkUrl, Routes.artist(it.id, it.name)) }
            else -> Engine.libraryPodcasts()?.map { listOf(it.name, "", it.artworkUrl, Routes.podcast(it.id, it.name)) }
        }
    }
    Scaffold(topBar = { PageBar(title, onBack) }) { padding ->
        LoadView(load, retry, icon, stringResource(R.string.tracklist_empty), Modifier.padding(padding), isEmpty = { it.isEmpty() }) { list ->
            LazyColumn(Modifier.fillMaxSize().padding(padding).readableWidth(), contentPadding = listEnd()) {
                items(list, key = { it[3] }) { (name, sub, art, route) ->
                    CoverRow(name, sub, art, onClick = { onNavigate(route) }, round = kind == "artist")
                }
            }
        }
    }
}

/**
 * The music kept offline: the pinned albums and playlists, with how far their fetch got,
 * then the tracks kept one by one. It follows the fetches as they go.
 */
@Composable
fun DownloadedScreen(player: PlayerController, onBack: () -> Unit, onNavigate: (String) -> Unit) {
    val offlineIds by player.offlineIds.collectAsState()
    val load by produceState<Load<Pair<List<OfflineCollection>, List<Track>>>>(Load.Loading, offlineIds) {
        val collections = Engine.offlineCollections()
        val tracks = Engine.offlineTracksOrNull("track", "")
        value = if (collections == null || tracks == null) Load.Failed else Load.Ready(collections.filter { it.kind != "track" } to tracks)
    }
    val context = ListenContext.NONE
    Scaffold(topBar = { PageBar(stringResource(R.string.library_downloaded), onBack) }) { padding ->
        LoadView(load, player::refreshOfflineIds, Icons.Filled.DownloadDone, stringResource(R.string.offline_empty), Modifier.padding(padding), isEmpty = { it.first.isEmpty() && it.second.isEmpty() }) { (collections, tracks) ->
            LazyColumn(Modifier.fillMaxSize().padding(padding).readableWidth(), contentPadding = listEnd()) {
                items(collections, key = { "${it.kind}:${it.id}" }) { c ->
                    val count = pluralStringResource(R.plurals.n_tracks, c.trackCount, c.trackCount)
                    val line = if (c.fetched < c.trackCount) stringResource(R.string.offline_progress, c.fetched, c.trackCount) else count
                    CoverRow(c.title, line, c.cover, onClick = {
                        onNavigate(if (c.kind == "album") Routes.album(c.id, c.title) else Routes.playlist(c.id, c.title))
                    })
                }
                if (tracks.isNotEmpty()) {
                    item(key = "tracks") { SectionHeader(stringResource(R.string.section_tracks)) }
                    itemsIndexed(tracks, key = { _, t -> "t-${t.id}" }) { i, t ->
                        TrackRow(t, player, context, onClick = { player.playQueue(tracks, i, context) })
                    }
                }
            }
        }
    }
}
