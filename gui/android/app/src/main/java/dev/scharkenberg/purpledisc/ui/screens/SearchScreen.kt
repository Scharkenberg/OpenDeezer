package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import dev.scharkenberg.purpledisc.ui.components.AppChip
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.async
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Icon
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.readableWidth
import dev.scharkenberg.purpledisc.Routes
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.Podcast
import dev.scharkenberg.purpledisc.engine.SearchItem
import dev.scharkenberg.purpledisc.engine.SearchResults
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.MediaCard
import dev.scharkenberg.purpledisc.ui.components.SectionHeader
import dev.scharkenberg.purpledisc.ui.components.TrackRow
import dev.scharkenberg.purpledisc.ui.components.idKeys
import dev.scharkenberg.purpledisc.ui.components.rememberLoad
import dev.scharkenberg.purpledisc.ui.theme.Space
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

private const val DEBOUNCE_MS = 350L
private const val HISTORY_SIZE = 20

private enum class Filter(val label: Int) {
    ALL(R.string.filter_all),
    TRACKS(R.string.section_tracks),
    ALBUMS(R.string.section_albums),
    ARTISTS(R.string.section_artists),
    PLAYLISTS(R.string.section_playlists),
    PODCASTS(R.string.podcasts_title),
}

private class SearchPage(val results: SearchResults, val best: SearchItem?)

// The page a search item opens; a track has none. Deezer names the types "Track", "Album", ...
private fun routeOf(item: SearchItem): String? = when (item.type) {
    "Album" -> Routes.album(item.id, item.title)
    "Artist" -> Routes.artist(item.id, item.title)
    "Playlist" -> Routes.playlist(item.id, item.title)
    "Podcast" -> Routes.podcast(item.id, item.title)
    else -> null
}

/** The Search tab: recent searches while the field is empty, else the results by filter. */
@Composable
fun SearchScreen(player: PlayerController, onNavigate: (String) -> Unit) {
    var query by rememberSaveable { mutableStateOf("") }
    var searched by rememberSaveable { mutableStateOf("") }
    var filter by rememberSaveable { mutableStateOf(Filter.ALL) }
    val focus = remember { FocusRequester() }
    val focusManager = LocalFocusManager.current

    LaunchedEffect(query) {
        if (query.isNotBlank()) delay(DEBOUNCE_MS)
        searched = query.trim()
    }
    // Back to a search keeps its field and results; only an empty field takes the focus.
    LaunchedEffect(Unit) { if (query.isBlank()) focus.requestFocus() }

    // Opening a result records it, as the Windows app's recent searches do.
    fun opened(type: String, id: String, route: String?) {
        Engine.addSearchHistory(type, id)
        route?.let(onNavigate)
    }

    Scaffold { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                placeholder = { Text(stringResource(R.string.search_hint)) },
                leadingIcon = { Icon(Icons.Filled.Search, contentDescription = null) },
                trailingIcon = if (query.isEmpty()) null else {
                    {
                        AppIconButton(onClick = { query = "" }) {
                            Icon(Icons.Filled.Clear, contentDescription = stringResource(R.string.action_clear))
                        }
                    }
                },
                singleLine = true,
                shape = MaterialTheme.shapes.large,
                modifier = Modifier.fillMaxWidth().padding(horizontal = Space.l, vertical = Space.s).focusRequester(focus),
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { focusManager.clearFocus() }),
            )
            if (searched.isEmpty()) {
                RecentSearches(onPick = { item ->
                    val route = routeOf(item)
                    if (route == null) query = item.title else opened(item.type, item.id, route)
                })
            } else {
                LazyRow(contentPadding = PaddingValues(horizontal = Space.l), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    items(Filter.entries) { f ->
                        AppChip(selected = filter == f, onClick = { filter = f }, label = { Text(stringResource(f.label)) })
                    }
                }
                if (filter == Filter.PODCASTS) {
                    PodcastResults(searched) { p -> opened("Podcast", p.id, Routes.podcast(p.id, p.name)) }
                } else {
                    Results(searched, filter, player, ::opened)
                }
            }
        }
    }
}

@Composable
private fun RecentSearches(onPick: (SearchItem) -> Unit) {
    val scope = rememberCoroutineScope()
    var cleared by remember { mutableIntStateOf(0) }
    val history by produceState<List<SearchItem>>(emptyList(), cleared) { value = Engine.searchHistory(HISTORY_SIZE).orEmpty() }
    if (history.isEmpty()) return
    LazyColumn(contentPadding = listEnd()) {
        items(history, key = { "${it.type}:${it.id}" }) { item ->
            ListItem(
                onClick = { onPick(item) },
                supportingContent = item.subtitle.takeIf { it.isNotBlank() }?.let { { Text(it) } },
                leadingContent = { Icon(Icons.Filled.History, contentDescription = null) },
            ) { Text(item.title) }
        }
        item {
            TextButton(onClick = { scope.launch { Engine.clearSearchHistory(); cleared++ } }, modifier = Modifier.padding(horizontal = Space.s)) {
                Text(stringResource(R.string.search_clear_recent))
            }
        }
    }
}

@Composable
private fun Results(q: String, filter: Filter, player: PlayerController, opened: (String, String, String?) -> Unit) {
    val (load, retry) = rememberLoad("search:$q") {
        coroutineScope {
            val best = async { Engine.bestResult(q) }
            Engine.searchOrNull(q)?.let { SearchPage(it, best.await()) }
        }
    }
    LoadView(load, retry, Icons.Filled.Search, stringResource(R.string.search_no_results), isEmpty = { it.results.isEmpty }) { page ->
        val r = page.results
        val context = ListenContext("search_track", q)
        val trackKeys = remember(r) { idKeys(r.tracks) { it.id } }
        LazyColumn(Modifier.fillMaxSize().readableWidth(), contentPadding = listEnd()) {
            if (filter == Filter.ALL) {
                page.best?.let { best ->
                    item { SectionHeader(stringResource(R.string.search_top_result)) }
                    item {
                        CoverRow(best.title, best.subtitle, best.picture, round = best.type == "Artist", onClick = {
                            val i = r.tracks.indexOfFirst { it.id == best.id }
                            if (best.type == "Track" && i >= 0) {
                                opened("Track", best.id, null)
                                player.playQueue(r.tracks, i, context)
                            } else {
                                opened(best.type, best.id, routeOf(best))
                            }
                        })
                    }
                }
            }
            if (filter == Filter.ALL || filter == Filter.TRACKS) {
                if (filter == Filter.ALL && r.tracks.isNotEmpty()) item { SectionHeader(stringResource(R.string.section_tracks)) }
                itemsIndexed(r.tracks, key = { i, _ -> trackKeys[i] }) { i, t ->
                    TrackRow(t, player, context, onClick = {
                        opened("Track", t.id, null)
                        player.playQueue(r.tracks, i, context)
                    })
                }
            }
            when (filter) {
                Filter.ALL -> carousels(r, opened)
                Filter.ALBUMS -> items(r.albums, key = { it.id }) { a ->
                    CoverRow(a.name, a.artistLine, a.artworkUrl, onClick = { opened("Album", a.id, Routes.album(a.id, a.name)) })
                }
                Filter.ARTISTS -> items(r.artists, key = { it.id }) { a ->
                    val fans = if (a.nbFans > 0) pluralStringResource(R.plurals.n_fans, a.nbFans, a.nbFans) else ""
                    CoverRow(a.name, fans, a.artworkUrl, round = true, onClick = { opened("Artist", a.id, Routes.artist(a.id, a.name)) })
                }
                Filter.PLAYLISTS -> items(r.playlists, key = { it.id }) { p ->
                    CoverRow(p.name, p.owner, p.artworkUrl, onClick = { opened("Playlist", p.id, Routes.playlist(p.id, p.name)) })
                }
                else -> Unit
            }
        }
    }
}

/** Albums, artists and playlists as carousels; Charts shows its results the same way. */
fun LazyListScope.carousels(r: SearchResults, opened: (String, String, String?) -> Unit) {
    if (r.albums.isNotEmpty()) {
        item { SectionHeader(stringResource(R.string.section_albums)) }
        item {
            CardRow(r.albums, { it.id }) { a -> MediaCard(a.name, a.artistLine, a.artworkUrl, { opened("Album", a.id, Routes.album(a.id, a.name)) }) }
        }
    }
    if (r.artists.isNotEmpty()) {
        item { SectionHeader(stringResource(R.string.section_artists)) }
        item {
            CardRow(r.artists, { it.id }) { a ->
                val fans = if (a.nbFans > 0) pluralStringResource(R.plurals.n_fans, a.nbFans, a.nbFans) else ""
                MediaCard(a.name, fans, a.artworkUrl, { opened("Artist", a.id, Routes.artist(a.id, a.name)) }, round = true)
            }
        }
    }
    if (r.playlists.isNotEmpty()) {
        item { SectionHeader(stringResource(R.string.section_playlists)) }
        item {
            CardRow(r.playlists, { it.id }) { p -> MediaCard(p.name, p.owner, p.artworkUrl, { opened("Playlist", p.id, Routes.playlist(p.id, p.name)) }) }
        }
    }
}

/** A carousel of cards. */
@Composable
fun <T> CardRow(list: List<T>, id: (T) -> String, card: @Composable (T) -> Unit) {
    LazyRow(contentPadding = PaddingValues(horizontal = Space.l), horizontalArrangement = Arrangement.spacedBy(Space.m)) {
        items(list, key = id) { card(it) }
    }
}

@Composable
private fun PodcastResults(q: String, onOpen: (Podcast) -> Unit) {
    val (load, retry) = rememberLoad("podcasts:$q") { Engine.searchPodcastsOrNull(q) }
    LoadView(load, retry, Icons.Filled.Search, stringResource(R.string.podcasts_no_results), isEmpty = { it.isEmpty() }) { list ->
        LazyColumn(Modifier.fillMaxSize().readableWidth(), contentPadding = listEnd()) {
            items(list, key = { it.id }) { p ->
                val sub = if (p.episodeCount > 0) pluralStringResource(R.plurals.n_episodes, p.episodeCount, p.episodeCount) else ""
                CoverRow(p.name, sub, p.artworkUrl, onClick = { onOpen(p) })
            }
        }
    }
}
