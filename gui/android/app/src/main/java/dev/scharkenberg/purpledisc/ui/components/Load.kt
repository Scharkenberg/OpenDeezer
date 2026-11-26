package dev.scharkenberg.purpledisc.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ErrorOutline
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.data.Library
import dev.scharkenberg.purpledisc.ui.theme.Space

/** A screen's data: still loading, failed, or there. */
sealed interface Load<out T> {
    data object Loading : Load<Nothing>
    data object Failed : Load<Nothing>
    data class Ready<T>(val value: T) : Load<T>
}

/**
 * Loads [key] with [fetch] (null means failed), through [Library]: a kept reply shows at once
 * and is loaded again behind when stale. The second value retries.
 */
@Suppress("UNCHECKED_CAST")
@Composable
fun <T : Any> rememberLoad(key: String, fetch: suspend () -> T?): Pair<Load<T>, () -> Unit> {
    var attempt by remember(key) { mutableIntStateOf(0) }
    val load by produceState<Load<T>>(Load.Loading, key, attempt, Library.drops) {
        val kept = Library.peek(key) as T?
        value = kept?.let { Load.Ready(it) } ?: Load.Loading
        if (kept != null && attempt == 0 && Library.isFresh(key)) return@produceState
        val fetched = fetch()
        if (fetched != null) {
            Library.store(key, fetched)
            value = Load.Ready(fetched)
        } else if (kept == null) {
            value = Load.Failed
        }
    }
    return load to { attempt++ }
}

/**
 * The three non-content states: a spinner, an empty line under an icon, a failure with
 * Retry; otherwise [content] with the value.
 */
@Composable
fun <T> LoadView(
    load: Load<T>,
    onRetry: () -> Unit,
    emptyIcon: ImageVector,
    emptyText: String,
    modifier: Modifier = Modifier,
    isEmpty: (T) -> Boolean = { false },
    content: @Composable (T) -> Unit,
) {
    when {
        load is Load.Ready && !isEmpty(load.value) -> content(load.value)
        load is Load.Ready -> Message(emptyIcon, emptyText, modifier)
        load is Load.Failed -> Message(Icons.Filled.ErrorOutline, stringResource(R.string.load_failed), modifier) {
            TextButton(onClick = onRetry) { Text(stringResource(R.string.action_retry)) }
        }
        else -> Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
    }
}

@Composable
private fun Message(icon: ImageVector, text: String, modifier: Modifier, action: @Composable () -> Unit = {}) {
    Column(
        modifier.fillMaxSize().padding(Space.l),
        verticalArrangement = Arrangement.spacedBy(Space.s, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(Space.xl * 2))
        Text(text, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        action()
    }
}
