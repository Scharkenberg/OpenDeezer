package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.AppChip
import dev.scharkenberg.purpledisc.ui.components.SectionHeader
import dev.scharkenberg.purpledisc.ui.components.listEnd
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderState
import androidx.compose.material3.VerticalSlider
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.ui.theme.Space
import kotlin.math.roundToInt
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.conflate
import kotlinx.coroutines.flow.filterNotNull

/**
 * 10-band graphic equalizer backed by the shared engine DSP. The engine owns
 * all state and persistence (a manual band edit flips the preset to "custom"
 * engine-side, saves are debounced there); this screen only renders controls
 * and forwards changes. State is re-read on every entry so edits from other
 * clients (remote, desktop) show up.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EqualizerScreen(onBack: () -> Unit) {
    var eq by remember { mutableStateOf(Engine.eqState()) }
    // Local slider positions: updated live while dragging, re-seeded from the
    // engine on preset changes.
    val gains = remember { mutableStateListOf<Float>().apply { eq?.gainsDb?.forEach { add(it.toFloat()) } } }
    var preamp by remember { mutableFloatStateOf(eq?.preampDb?.toFloat() ?: 0f) }

    // Drags fire on every frame; conflate to ~30 engine calls/s. The engine
    // debounces persistence itself, so there is no save logic here.
    val pendingBand = remember { MutableStateFlow<Pair<Int, Float>?>(null) }
    LaunchedEffect(Unit) {
        pendingBand.filterNotNull().conflate().collect { (band, db) ->
            Engine.setEqBand(band, db.toDouble())
            delay(33)
        }
    }

    fun applyPreset(name: String) {
        Engine.setEqPreset(name)
        Engine.eqState()?.let { st ->
            eq = st
            gains.clear()
            st.gainsDb.forEach { gains.add(it.toFloat()) }
            preamp = st.preampDb.toFloat()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.equalizer_title)) },
                navigationIcon = {
                    AppIconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = stringResource(R.string.action_back))
                    }
                },
            )
        },
    ) { padding ->
        val st = eq
        if (st == null) {
            Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                Text(stringResource(R.string.eq_unavailable), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            return@Scaffold
        }
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState()).padding(listEnd())) {
            SwitchRow(stringResource(R.string.common_enable), stringResource(R.string.eq_enable_sub), st.enabled) {
                eq = st.copy(enabled = it)
                Engine.setEqEnabled(it)
            }

            HorizontalDivider()

            SectionHeader(stringResource(R.string.eq_preset))
            Row(
                Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = Space.l),
                horizontalArrangement = Arrangement.spacedBy(Space.s),
            ) {
                if (st.preset == "custom") {
                    AppChip(selected = true, onClick = {}, label = { Text(stringResource(R.string.eq_custom)) })
                }
                st.presets.forEach { name ->
                    AppChip(
                        selected = st.preset == name,
                        onClick = { applyPreset(name) },
                        label = { Text(presetLabel(name)) },
                    )
                }
            }

            Row(Modifier.fillMaxWidth().padding(Space.l).height(280.dp)) {
                st.bands.forEachIndexed { i, hz ->
                    Column(
                        Modifier.weight(1f).fillMaxHeight(),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        val gain = gains.getOrElse(i) { 0f }
                        Text(
                            gainLabel(gain),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        BandSlider(
                            value = gain,
                            onValueChange = { v ->
                                // 0.5 dB snapping doubles as a soft 0-detent.
                                val snapped = (v * 2).roundToInt() / 2f
                                if (i < gains.size && gains[i] != snapped) {
                                    gains[i] = snapped
                                    pendingBand.value = i to snapped
                                    // The engine flips to "custom" on band
                                    // edits; mirror that immediately.
                                    if (st.preset != "custom") eq = st.copy(preset = "custom")
                                }
                            },
                            modifier = Modifier.weight(1f),
                        )
                        Text(
                            bandLabel(hz),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }

            SectionHeader(stringResource(R.string.eq_preamp))
            Column(Modifier.padding(horizontal = Space.l)) {
                Text(
                    "%+.1f dB".format(preamp),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                val preampState = remember { SliderState(preamp, 0, -12f..12f) }
                Slider(
                    state = preampState,
                    onValueChange = {
                        preamp = (it * 2).roundToInt() / 2f
                        preampState.value = preamp
                    },
                    onValueChangeFinished = { Engine.setEqPreamp(preamp.toDouble()) },
                )
            }

            HorizontalDivider()

            SwitchRow(stringResource(R.string.eq_mono_title), stringResource(R.string.eq_mono_sub), st.mono) {
                eq = st.copy(mono = it)
                Engine.setEqMono(it)
            }
        }
    }
}

// A band's gain, -12 to +12 dB, on Material's vertical slider (+12 at the top). The engine's
// value moves the thumb unless it is held.
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
@Composable
private fun BandSlider(value: Float, onValueChange: (Float) -> Unit, modifier: Modifier = Modifier) {
    val state = remember { SliderState(value, 0, -12f..12f) }
    if (!state.isDragging) state.value = value
    // enabled, no finish callback, and not reversed: the low end at the bottom.
    VerticalSlider(state, { state.value = it; onValueChange(it) }, modifier, true, null, false)
}
// "bass-boost" -> "Bass Boost".
private fun presetLabel(name: String): String =
    name.split('-').joinToString(" ") { part -> part.replaceFirstChar { it.uppercase() } }

private fun bandLabel(hz: Double): String =
    if (hz >= 1000) "${(hz / 1000).toInt()}k" else "${hz.toInt()}"

private fun gainLabel(db: Float): String {
    val v = db.roundToInt()
    return if (v > 0) "+$v" else "$v"
}
