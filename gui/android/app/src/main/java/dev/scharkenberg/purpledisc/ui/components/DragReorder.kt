package dev.scharkenberg.purpledisc.ui.components

import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.foundation.lazy.LazyListItemInfo
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.zIndex
import kotlinx.coroutines.channels.Channel

/**
 * Lightweight drag-to-reorder state for a [LazyListState], driven from an
 * explicit per-row drag handle (see [Modifier.dragHandle]) so it never fights a
 * row's tap / swipe / long-press gestures.
 *
 * The reorder is split in two: [onMoveLive] shuffles a caller-owned display list
 * as the dragged row crosses its neighbors (smooth visuals, keyed items), while
 * [onMoveCommit] is fired once on release with the net (from, to) so the queue is
 * mutated a single time.
 */
class DragDropState internal constructor(
    private val state: LazyListState,
    private val onMoveLive: (Int, Int) -> Unit,
    private val onMoveCommit: (Int, Int) -> Unit,
) {
    var draggingItemIndex by mutableStateOf<Int?>(null)
        private set

    // Overscroll requests emitted while dragging past the viewport edge; the
    // screen drains this and scrolls the list so long queues stay reorderable.
    internal val scrollChannel = Channel<Float>()

    private var dragStartIndex: Int? = null
    private var draggingItemDraggedDelta by mutableFloatStateOf(0f)
    private var draggingItemInitialOffset by mutableIntStateOf(0)

    internal val draggingItemOffset: Float
        get() = draggingItemLayoutInfo?.let { item ->
            draggingItemInitialOffset + draggingItemDraggedDelta - item.offset
        } ?: 0f

    private val draggingItemLayoutInfo: LazyListItemInfo?
        get() = state.layoutInfo.visibleItemsInfo.firstOrNull { it.index == draggingItemIndex }

    internal fun onDragStart(key: Any) {
        val info = state.layoutInfo.visibleItemsInfo.firstOrNull { it.key == key } ?: return
        draggingItemIndex = info.index
        dragStartIndex = info.index
        draggingItemInitialOffset = info.offset
    }

    internal fun onDragInterrupted() {
        val from = dragStartIndex
        val to = draggingItemIndex
        if (from != null && to != null && from != to) onMoveCommit(from, to)
        draggingItemIndex = null
        dragStartIndex = null
        draggingItemDraggedDelta = 0f
        draggingItemInitialOffset = 0
    }

    internal fun onDrag(deltaY: Float) {
        draggingItemDraggedDelta += deltaY
        val draggingItem = draggingItemLayoutInfo ?: return
        val startOffset = draggingItem.offset + draggingItemOffset
        val endOffset = startOffset + draggingItem.size
        val middleOffset = startOffset + (endOffset - startOffset) / 2f

        val target = state.layoutInfo.visibleItemsInfo.firstOrNull { item ->
            middleOffset.toInt() in item.offset..(item.offset + item.size) &&
                draggingItem.index != item.index
        }
        if (target != null) {
            val from = draggingItem.index
            onMoveLive(from, target.index)
            draggingItemIndex = target.index
        } else {
            val overscroll = when {
                draggingItemDraggedDelta > 0 ->
                    (endOffset - state.layoutInfo.viewportEndOffset).coerceAtLeast(0f)
                draggingItemDraggedDelta < 0 ->
                    (startOffset - state.layoutInfo.viewportStartOffset).coerceAtMost(0f)
                else -> 0f
            }
            if (overscroll != 0f) scrollChannel.trySend(overscroll)
        }
    }
}

@Composable
fun rememberDragDropState(
    lazyListState: LazyListState,
    onMoveLive: (Int, Int) -> Unit,
    onMoveCommit: (Int, Int) -> Unit,
): DragDropState = remember(lazyListState) {
    DragDropState(lazyListState, onMoveLive, onMoveCommit)
}

/**
 * Attaches to a small drag-handle affordance inside the row with list key [key]: grabbing
 * it and dragging vertically reorders the row. Keyed on the item key, not its index, so
 * the gesture survives the row moving. An immediate (no long-press) drag leaves the rest of
 * the row free for tap and swipe.
 */
fun Modifier.dragHandle(dragDropState: DragDropState, key: Any): Modifier =
    pointerInput(dragDropState, key) {
        detectDragGestures(
            onDragStart = { dragDropState.onDragStart(key) },
            onDrag = { change, dragAmount ->
                change.consume()
                dragDropState.onDrag(dragAmount.y)
            },
            onDragEnd = { dragDropState.onDragInterrupted() },
            onDragCancel = { dragDropState.onDragInterrupted() },
        )
    }

/**
 * Wraps a LazyColumn item so the dragged row lifts above its neighbors and
 * tracks the finger, while the others animate into their new slots.
 */
@Composable
fun LazyItemScope.DraggableItem(
    dragDropState: DragDropState,
    index: Int,
    content: @Composable (isDragging: Boolean) -> Unit,
) {
    val dragging = index == dragDropState.draggingItemIndex
    val modifier = if (dragging) {
        Modifier
            .zIndex(1f)
            .graphicsLayer { translationY = dragDropState.draggingItemOffset }
    } else {
        Modifier.animateItem()
    }
    androidx.compose.foundation.layout.Box(modifier) { content(dragging) }
}
