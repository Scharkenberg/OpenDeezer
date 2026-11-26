package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.SectionHeader
import dev.scharkenberg.purpledisc.player.PlayerState
import dev.scharkenberg.purpledisc.ui.components.formatTotal
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.scrollBy
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.DeleteSweep
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.DragHandle
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import dev.scharkenberg.purpledisc.ui.components.MarqueeText
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.readableWidth
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.player.QueueEntry
import dev.scharkenberg.purpledisc.ui.components.Artwork
import dev.scharkenberg.purpledisc.ui.components.DraggableItem
import dev.scharkenberg.purpledisc.ui.components.Load
import dev.scharkenberg.purpledisc.ui.components.LoadView
import dev.scharkenberg.purpledisc.ui.components.MoreButton
import dev.scharkenberg.purpledisc.ui.components.dragHandle
import dev.scharkenberg.purpledisc.ui.components.rememberDragDropState
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QueueScreen(player: PlayerController, onBack: () -> Unit, onMore: (QueueEntry) -> Unit) {
    val state by player.state.collectAsState()
    val offlineIds by player.offlineIds.collectAsState()

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.queue_title))
                        QueueLeft(player, state)
                    }
                },
                navigationIcon = {
                    AppIconButton(onClick = onBack) {
                        Icon(Icons.Filled.KeyboardArrowDown, contentDescription = stringResource(R.string.action_back))
                    }
                },
                actions = {
                    if (state.queue.size > 1) {
                        AppIconButton(onClick = { player.clearQueue() }) {
                            Icon(Icons.Filled.DeleteSweep, contentDescription = stringResource(R.string.action_clear_queue))
                        }
                    }
                },
            )
        },
    ) { padding ->
        LoadView(
            Load.Ready(state.queue),
            onRetry = {},
            emptyIcon = Icons.AutoMirrored.Filled.QueueMusic,
            emptyText = stringResource(R.string.queue_empty),
            modifier = Modifier.padding(padding),
            isEmpty = { it.isEmpty() },
        ) {
            val listState = rememberLazyListState()
            // The rows as shown: a drag reorders them live, and the queue is moved once on release.
            var items by remember { mutableStateOf(state.queue) }
            val dragState = rememberDragDropState(
                lazyListState = listState,
                onMoveLive = { from, to -> items = items.toMutableList().apply { add(to, removeAt(from)) } },
                onMoveCommit = { from, to -> player.moveInQueue(from, to) },
            )
            LaunchedEffect(state.queue) {
                if (dragState.draggingItemIndex == null) items = state.queue
            }
            // Scroll requests while a row is dragged past an edge.
            LaunchedEffect(dragState) {
                while (true) listState.scrollBy(dragState.scrollChannel.receive())
            }
            val currentUid = state.queue.getOrNull(state.index)?.uid
            LaunchedEffect(currentUid) {
                val at = items.indexOfFirst { it.uid == currentUid }
                if (dragState.draggingItemIndex == null && at >= 0) listState.animateScrollToItem(at)
            }
            val playingAt = items.indexOfFirst { it.uid == currentUid }
            LazyColumn(
                Modifier.fillMaxSize().padding(padding).readableWidth(),
                state = listState,
                contentPadding = PaddingValues(bottom = Space.l),
            ) {
                itemsIndexed(items, key = { _, e -> e.uid }) { i, entry ->
                    // Reordering without dragging, for screen readers.
                    val moveUp = stringResource(R.string.queue_move_up)
                    val moveDown = stringResource(R.string.queue_move_down)
                    DraggableItem(dragState, i) {
                        Column(
                            Modifier.semantics {
                                customActions = listOfNotNull(
                                    CustomAccessibilityAction(moveUp) { player.moveInQueue(i, i - 1); true }.takeIf { i > 0 },
                                    CustomAccessibilityAction(moveDown) { player.moveInQueue(i, i + 1); true }.takeIf { i < items.lastIndex },
                                )
                            },
                        ) {
                            if (i == playingAt) SectionHeader(stringResource(R.string.cd_now_playing))
                            if (i == playingAt + 1) SectionHeader(stringResource(R.string.queue_next_up))
                            QueueRow(
                                entry = entry,
                                isCurrent = entry.uid == currentUid,
                                offline = offlineIds.contains(entry.track.id),
                                onClick = { player.jumpTo(entry.uid) },
                                onRemove = { player.removeFromQueue(entry.uid) },
                                onMore = { onMore(entry) },
                                dragHandleModifier = Modifier.dragHandle(dragState, entry.uid),
                            )
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun QueueRow(
    entry: QueueEntry,
    isCurrent: Boolean,
    offline: Boolean,
    onClick: () -> Unit,
    onRemove: () -> Unit,
    onMore: () -> Unit,
    dragHandleModifier: Modifier,
) {
    // Swipe removes a row; the playing row stays (remove it from its menu).
    val dismissState = rememberSwipeToDismissBoxState()
    LaunchedEffect(dismissState.currentValue) {
        if (dismissState.currentValue != SwipeToDismissBoxValue.Settled) onRemove()
    }
    val track = entry.track
    SwipeToDismissBox(
        state = dismissState,
        enableDismissFromStartToEnd = !isCurrent,
        enableDismissFromEndToStart = !isCurrent,
        backgroundContent = {
            // Drawn only while a swipe is under way.
            if (dismissState.dismissDirection != SwipeToDismissBoxValue.Settled) {
                Box(
                    Modifier.fillMaxSize().background(MaterialTheme.colorScheme.errorContainer).padding(horizontal = Space.xl),
                    contentAlignment = Alignment.CenterEnd,
                ) {
                    Icon(Icons.Filled.Delete, contentDescription = stringResource(R.string.action_remove), tint = MaterialTheme.colorScheme.onErrorContainer)
                }
            }
        },
    ) {
        Row(
            Modifier
                .fillMaxWidth()
                .heightIn(min = Size.row)
                .background(if (isCurrent) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface)
                .clickable(onClick = onClick)
                .padding(start = Space.l),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Artwork(track.artworkUrl, Modifier.size(Size.rowArt), shown = Size.rowArt)
            Spacer(Modifier.width(Space.m))
            // Only the playing row scrolls a long title; the others end in an ellipsis.
            val title = track.name.ifBlank { stringResource(R.string.unknown_title) }
            val artist = track.artistLine.ifBlank { track.albumName }
            Column(Modifier.weight(1f)) {
                if (isCurrent) {
                    MarqueeText(title, style = MaterialTheme.typography.bodyLarge)
                    MarqueeText(artist, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                } else {
                    Text(title, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(artist, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            if (offline) {
                Icon(
                    Icons.Filled.DownloadDone,
                    contentDescription = stringResource(R.string.cd_downloaded),
                    modifier = Modifier.size(Space.l),
                    tint = MaterialTheme.colorScheme.primary,
                )
            }
            MoreButton(onMore)
            Box(dragHandleModifier.size(Space.xl * 2), contentAlignment = Alignment.Center) {
                Icon(Icons.Filled.DragHandle, contentDescription = stringResource(R.string.cd_reorder), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

// The time from the playing track's position to the queue's end, as the Windows app's queue
// shows it; alone in following the position, so the screen around it stays still.
@Composable
private fun QueueLeft(player: PlayerController, state: PlayerState) {
    val progress by player.progress.collectAsState()
    val rest = remember(state.queue, state.index) { state.queue.drop(state.index + 1).sumOf { it.track.durationMs } }
    val current = (progress.durationMs - progress.positionMs).coerceAtLeast(0L)
    val minutes = (rest + current) / 60_000L
    if (state.index < 0 || minutes <= 0L) return
    Text(
        stringResource(R.string.queue_left, formatTotal((rest + current) / 1000)),
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}
