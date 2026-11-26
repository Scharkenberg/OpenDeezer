package dev.scharkenberg.purpledisc.ui.screens

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.util.lerp
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import dev.scharkenberg.purpledisc.ui.components.FULL_TURN
import dev.scharkenberg.purpledisc.ui.components.PlayPauseIcon
import dev.scharkenberg.purpledisc.ui.components.pressScale
import dev.scharkenberg.purpledisc.ui.theme.Move
import kotlinx.coroutines.launch
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.IconToggleButton
import androidx.compose.material3.Surface
import dev.scharkenberg.purpledisc.ui.components.CoverTheme
import dev.scharkenberg.purpledisc.ui.components.MarqueeText
import dev.scharkenberg.purpledisc.ui.theme.Size
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.QueueMusic
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material.icons.filled.Bedtime
import androidx.compose.material.icons.filled.Cast
import androidx.compose.material.icons.filled.CastConnected
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Lyrics
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.RepeatOne
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Timer
import androidx.compose.material.icons.filled.TimerOff
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.ui.unit.DpSize
import androidx.compose.material3.SliderState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.Routes
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.engine.Track
import dev.scharkenberg.purpledisc.player.PlayerController
import dev.scharkenberg.purpledisc.player.PlayerState
import dev.scharkenberg.purpledisc.ui.HingeSplit
import dev.scharkenberg.purpledisc.ui.LocalFoldState
import dev.scharkenberg.purpledisc.ui.LocalWindowLayout
import dev.scharkenberg.purpledisc.ui.TwoPane
import dev.scharkenberg.purpledisc.ui.components.ActionSheet
import dev.scharkenberg.purpledisc.ui.components.Artwork
import dev.scharkenberg.purpledisc.ui.components.LocalTrackSheet
import dev.scharkenberg.purpledisc.ui.components.MoreButton
import dev.scharkenberg.purpledisc.ui.components.SheetItem
import dev.scharkenberg.purpledisc.ui.components.TrackSheetRequest
import dev.scharkenberg.purpledisc.ui.components.formatDuration
import dev.scharkenberg.purpledisc.ui.theme.Space
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.conflate
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.filterNotNull

private val PlaySize = 72.dp
private const val LIKE_MS = 400
private const val MODE_TURN_MS = 350
private const val VOLUME_STEP_MS = 33L
private val SleepMinutes = listOf(5, 10, 20, 30, 45, 60)
private val Speeds = listOf(0.5, 0.75, 1.0, 1.25, 1.5, 2.0)

/** A top bar with no fill, for the pages on the backdrop. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BackdropBar(title: @Composable () -> Unit, onBack: () -> Unit, actions: @Composable () -> Unit = {}) {
    TopAppBar(
        title = title,
        navigationIcon = {
            AppIconButton(onClick = onBack) {
                Icon(Icons.Filled.KeyboardArrowDown, contentDescription = stringResource(R.string.action_back))
            }
        },
        actions = { actions() },
        colors = TopAppBarDefaults.topAppBarColors(containerColor = Color.Transparent),
    )
}

private const val SLEEP_CHECK_MS = 1000L

// The cover's largest size on this page.
private val CoverMax = 520.dp

@Composable
fun NowPlayingScreen(player: PlayerController, onBack: () -> Unit, onNavigate: (String) -> Unit, onCast: () -> Unit) {
    val state by player.state.collectAsState()
    val track = state.current
    val openSheet = LocalTrackSheet.current
    CoverTheme(track?.artworkUrl) {
        Scaffold(
            containerColor = Color.Transparent,
            contentColor = MaterialTheme.colorScheme.onSurface,
            topBar = {
                BackdropBar({}, onBack) {
                    if (track != null) MoreButton { openSheet(TrackSheetRequest(track, ListenContext.NONE)) }
                }
            },
        ) { padding ->
            if (track == null) {
                Box(Modifier.fillMaxSize().padding(padding), contentAlignment = Alignment.Center) {
                    Text(stringResource(R.string.np_nothing_playing), color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                return@Scaffold
            }
            val fold = LocalFoldState.current
            val hinge = fold.hingeBounds
            if (fold.isTableTop && hinge != null) {
                // Tabletop posture: the upright half shows the cover and the lyrics, the
                // flat half keeps the controls under the fingers.
                HingeSplit(
                    hingeBounds = hinge,
                    horizontalHinge = true,
                    modifier = Modifier.fillMaxSize().padding(padding),
                    first = {
                        Row(Modifier.fillMaxSize().padding(Space.l), horizontalArrangement = Arrangement.spacedBy(Space.l)) {
                            Box(Modifier.weight(1f).fillMaxHeight(), contentAlignment = Alignment.Center) {
                                Artwork(track.artworkUrl, Modifier.aspectRatio(1f), shape = MaterialTheme.shapes.large, shown = CoverMax)
                            }
                            LyricsContent(player, Modifier.weight(1f).fillMaxHeight())
                        }
                    },
                    second = {
                        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = Space.xl)) {
                            Controls(state, track, player, onNavigate, onCast)
                        }
                    },
                )
            } else if (LocalWindowLayout.current.twoPane) {
                // Landscape, a tablet or an open foldable: the cover beside the controls.
                TwoPane(Modifier.fillMaxSize().padding(padding), firstShare = 0.45f, first = {
                    Box(Modifier.fillMaxSize().padding(Space.l), contentAlignment = Alignment.Center) {
                        Artwork(track.artworkUrl, Modifier.widthIn(max = CoverMax).aspectRatio(1f), shape = MaterialTheme.shapes.large, shown = CoverMax)
                    }
                }, second = {
                    Column(
                        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = Space.xl),
                        verticalArrangement = Arrangement.Center,
                    ) {
                        Controls(state, track, player, onNavigate, onCast)
                    }
                })
            } else {
                Column(
                    Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState()).padding(horizontal = Space.xl),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Artwork(track.artworkUrl, Modifier.widthIn(max = CoverMax).fillMaxWidth().aspectRatio(1f), shape = MaterialTheme.shapes.large, shown = CoverMax)
                    Spacer(Modifier.height(Space.xl))
                    Controls(state, track, player, onNavigate, onCast)
                }
            }
        }
    }
}

// Title and heart, the seek bar, the transport row, the row of pages and timers, and the
// volume of a Connect device.
@Composable
private fun Controls(state: PlayerState, track: Track, player: PlayerController, onNavigate: (String) -> Unit, onCast: () -> Unit) {
    val liked by player.likedIds.collectAsState()
    var sleepSheet by remember { mutableStateOf(false) }
    var speedSheet by remember { mutableStateOf(false) }
    var sleeping by remember { mutableStateOf(Engine.sleepActive()) }
    // The timer ends by itself: the button follows the engine's remaining time.
    LaunchedEffect(sleeping) {
        while (sleeping) {
            delay(SLEEP_CHECK_MS)
            sleeping = Engine.sleepActive()
        }
    }
    val connected = state.connectedDevice.isNotBlank()

    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            MarqueeText(track.name.ifBlank { stringResource(R.string.unknown_title) }, style = MaterialTheme.typography.titleLarge)
            val artist = track.artists.firstOrNull()
            MarqueeText(
                track.artistLine.ifBlank { track.albumName },
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = if (artist != null) Modifier.clickable { onNavigate(Routes.artist(artist.id, artist.name)) } else Modifier,
            )
            val format = if (connected) stringResource(R.string.np_playing_on, state.connectedDevice) else state.format
            if (format.isNotBlank()) {
                Text(format, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (!track.isEpisode) {
            val on = track.id in liked
            // A like (not an unlike): the heart pops and one ring spreads from it.
            val burst = remember { Animatable(1f) }
            val scope = rememberCoroutineScope()
            val ring = MaterialTheme.colorScheme.primary
            val interaction = remember { MutableInteractionSource() }
            IconToggleButton(
                checked = on,
                onCheckedChange = {
                    if (it) scope.launch { burst.snapTo(0f); burst.animateTo(1f, tween(LIKE_MS, easing = LinearEasing)) }
                    player.toggleFavorite(track)
                },
                modifier = Modifier.pressScale(interaction).drawBehind {
                    val t = burst.value
                    if (t < 1f) drawCircle(ring, size.minDimension / 2 * lerp(0.5f, 1.5f, t), alpha = 1f - t, style = Stroke(Move.ring.toPx()))
                },
                interactionSource = interaction,
            ) {
                Icon(
                    if (on) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder,
                    contentDescription = stringResource(R.string.cd_like),
                    modifier = Modifier.graphicsLayer {
                        val t = burst.value
                        val s = if (t < 0.5f) lerp(0.6f, 1.25f, t / 0.5f) else lerp(1.25f, 1f, (t - 0.5f) / 0.5f)
                        scaleX = s
                        scaleY = s
                    },
                )
            }
        }
    }
    // A 30 s preview (a track the account cannot stream) says so.
    if (state.preview) {
        Surface(shape = MaterialTheme.shapes.small, color = MaterialTheme.colorScheme.secondaryContainer) {
            Text(stringResource(R.string.preview_chip), style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(horizontal = Space.m, vertical = Space.xs))
        }
    }

    SeekBar(player)

    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
        ModeButton(state.shuffle, { player.setShuffle(it) }, Icons.Filled.Shuffle, stringResource(R.string.cd_shuffle), turn = true)
        AppIconButton(onClick = player::prev, enabled = state.hasPrev, nudge = -Move.nudge) {
            Icon(Icons.Filled.SkipPrevious, contentDescription = stringResource(R.string.action_previous), modifier = Modifier.size(Space.xl + Space.s))
        }
        val playPress = remember { MutableInteractionSource() }
        FilledIconButton(onClick = player::togglePause, modifier = Modifier.size(PlaySize).pressScale(playPress), interactionSource = playPress) {
            PlayPauseIcon(state.state == Engine.PLAYING, stringResource(R.string.cd_play_pause), Modifier.size(Space.xl + Space.m))
        }
        AppIconButton(onClick = player::next, enabled = state.hasNext, nudge = Move.nudge) {
            Icon(Icons.Filled.SkipNext, contentDescription = stringResource(R.string.action_next), modifier = Modifier.size(Space.xl + Space.s))
        }
        ModeButton(
            state.repeatMode != 0,
            { player.setRepeat((state.repeatMode + 1) % 3) },
            if (state.repeatMode == 2) Icons.Filled.RepeatOne else Icons.Filled.Repeat,
            stringResource(R.string.cd_repeat),
            turn = true,
        )
    }

    Row(Modifier.fillMaxWidth().padding(vertical = Space.s), horizontalArrangement = Arrangement.SpaceBetween) {
        if (track.isEpisode) {
            ModeButton(Engine.playbackSpeed() != 1.0, { speedSheet = true }, Icons.Filled.Speed, stringResource(R.string.cd_speed), MaterialTheme.colorScheme.onSurface)
        } else {
            AppIconButton(onClick = { onNavigate(Routes.LYRICS) }) {
                Icon(Icons.Filled.Lyrics, contentDescription = stringResource(R.string.lyrics_title))
            }
        }
        ModeButton(sleeping, { sleeping = Engine.sleepActive(); sleepSheet = true }, Icons.Filled.Bedtime, stringResource(R.string.settings_sleep_timer), MaterialTheme.colorScheme.onSurface)
        ModeButton(connected, { onCast() }, if (connected) Icons.Filled.CastConnected else Icons.Filled.Cast, stringResource(R.string.cd_connect), MaterialTheme.colorScheme.onSurface)
        AppIconButton(onClick = { onNavigate(Routes.QUEUE) }) {
            Icon(Icons.AutoMirrored.Filled.QueueMusic, contentDescription = stringResource(R.string.queue_title))
        }
    }

    // A phone has its own volume keys; a Connect device is set from here.
    if (connected) VolumeBar(state, player)
    state.queue.getOrNull(state.index + 1)?.let { next -> UpNext(next.track) { onNavigate(Routes.QUEUE) } }
    Spacer(Modifier.height(Space.l))

    if (sleepSheet) SleepSheet(onDismiss = { sleepSheet = false; sleeping = Engine.sleepActive() })
    if (speedSheet) SpeedSheet(onDismiss = { speedSheet = false })
}

// A slider with a thin track and a round thumb, quieter than the default over a cover.
@Composable
private fun ThinSlider(state: SliderState, onValueChange: (Float) -> Unit, modifier: Modifier = Modifier, onValueChangeFinished: (() -> Unit)? = null) {
    val interaction = remember { MutableInteractionSource() }
    Slider(
        state = state,
        onValueChange = onValueChange,
        onValueChangeFinished = onValueChangeFinished,
        modifier = modifier,
        interactionSource = interaction,
        thumb = { SliderDefaults.Thumb(interaction, thumbSize = DpSize(Space.l, Space.l)) },
        track = { SliderDefaults.Track(sliderState = it, modifier = Modifier.height(Space.xs), drawStopIndicator = null, thumbTrackGapSize = 0.dp) },
    )
}

// A mode that is on or off (shuffle, repeat, timer, speed, Connect): in the accent with a dot
// under it when on; off, dimmed (shuffle, repeat) or as the buttons beside it ([offColor]).
@Composable
private fun ModeButton(
    on: Boolean,
    onToggle: (Boolean) -> Unit,
    icon: ImageVector,
    description: String,
    offColor: Color = MaterialTheme.colorScheme.onSurfaceVariant,
    turn: Boolean = false,
) {
    // A toggle by the user turns the glyph once around; a change read from the engine does not.
    val rotation = remember { Animatable(0f) }
    val scope = rememberCoroutineScope()
    val interaction = remember { MutableInteractionSource() }
    IconToggleButton(
        checked = on,
        onCheckedChange = {
            if (turn) scope.launch { rotation.animateTo(rotation.targetValue + FULL_TURN, tween(MODE_TURN_MS)) }
            onToggle(it)
        },
        modifier = Modifier.pressScale(interaction).graphicsLayer { rotationZ = rotation.value },
        interactionSource = interaction,
        colors = IconButtonDefaults.iconToggleButtonColors(
            contentColor = offColor,
            checkedContentColor = MaterialTheme.colorScheme.primary,
        ),
    ) {
        Box(contentAlignment = Alignment.Center) {
            Icon(icon, contentDescription = description)
            if (on) Box(Modifier.offset(y = Space.m + Space.xs).size(Space.xs).background(MaterialTheme.colorScheme.primary, CircleShape))
        }
    }
}

// The next row, as a card that opens the queue.
@Composable
private fun UpNext(track: Track, onOpen: () -> Unit) {
    Surface(onClick = onOpen, shape = MaterialTheme.shapes.medium, color = MaterialTheme.colorScheme.surfaceContainerHigh, modifier = Modifier.fillMaxWidth().padding(top = Space.m)) {
        Row(Modifier.padding(Space.m), verticalAlignment = Alignment.CenterVertically) {
            Artwork(track.artworkUrl, Modifier.size(Size.rowArt), shown = Size.rowArt)
            Spacer(Modifier.width(Space.m))
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.queue_next_up), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                MarqueeText(track.name, style = MaterialTheme.typography.titleSmall)
                MarqueeText(track.artistLine, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Icon(Icons.AutoMirrored.Filled.QueueMusic, contentDescription = stringResource(R.string.queue_title), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

// The position moves the thumb unless it is held; the times follow the thumb.
@Composable
private fun SeekBar(player: PlayerController) {
    val slider = remember { SliderState() }
    val durationMs by remember(player) { player.progress.map { it.durationMs }.distinctUntilChanged() }.collectAsState(0L)
    LaunchedEffect(player) {
        player.progress.collect { p ->
            if (!slider.isDragging && p.durationMs > 0) slider.value = (p.positionMs.toFloat() / p.durationMs).coerceIn(0f, 1f)
        }
    }
    ThinSlider(slider, { slider.value = it }, onValueChangeFinished = { player.seek((slider.value * durationMs).toLong()) })
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(formatDuration((slider.value * durationMs).toLong()), style = MaterialTheme.typography.labelSmall)
        Text(formatDuration(durationMs), style = MaterialTheme.typography.labelSmall)
    }
}

// Drags fire on every frame; they reach the device at most ~30 times a second.
@Composable
private fun VolumeBar(state: PlayerState, player: PlayerController) {
    val slider = remember { SliderState(state.volume.toFloat().coerceIn(0f, 1f)) }
    if (!slider.isDragging) slider.value = state.volume.toFloat().coerceIn(0f, 1f)
    val pending = remember { MutableStateFlow<Float?>(null) }
    LaunchedEffect(Unit) {
        pending.filterNotNull().conflate().collect {
            player.setVolume(it.toDouble())
            delay(VOLUME_STEP_MS)
        }
    }
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.AutoMirrored.Filled.VolumeUp, contentDescription = stringResource(R.string.cd_volume), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.size(Space.s))
        ThinSlider(slider, { slider.value = it; pending.value = it }, Modifier.weight(1f))
    }
}

// The sleep timer's choices; the state is read each time the sheet opens.
@Composable
private fun SleepSheet(onDismiss: () -> Unit) {
    val active = remember { Engine.sleepActive() }
    val status = when {
        Engine.sleepEndOfTrack() -> stringResource(R.string.sleep_desc_end)
        active -> {
            val min = ((Engine.sleepRemainingMs() + 59_999L) / 60_000L).toInt()
            pluralStringResource(R.plurals.pauses_in_minutes, min, min)
        }
        else -> stringResource(R.string.sleep_desc_off)
    }
    ActionSheet(onDismiss, header = {
        ListItem(
            supportingContent = { Text(status) },
            colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        ) { Text(stringResource(R.string.settings_sleep_timer), style = MaterialTheme.typography.titleMedium) }
    }) { close ->
        SleepMinutes.forEach { m ->
            SheetItem(Icons.Filled.Timer, pluralStringResource(R.plurals.pauses_in_minutes, m, m)) { close { Engine.setSleepTimer(m, endOfTrack = false) } }
        }
        SheetItem(Icons.Filled.Timer, stringResource(R.string.sleep_desc_end)) { close { Engine.setSleepTimer(0, endOfTrack = true) } }
        if (active) SheetItem(Icons.Filled.TimerOff, stringResource(R.string.sleep_turn_off)) { close { Engine.cancelSleepTimer() } }
    }
}

@Composable
private fun SpeedSheet(onDismiss: () -> Unit) {
    val current = remember { Engine.playbackSpeed() }
    ActionSheet(onDismiss, header = {
        ListItem(colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow)) {
            Text(stringResource(R.string.cd_speed), style = MaterialTheme.typography.titleMedium)
        }
    }) { close ->
        Speeds.forEach { s ->
            val label = "${s.toString().removeSuffix(".0")}×"
            ListItem(
                selected = s == current,
                onClick = { close { Engine.setPlaybackSpeed(s) } },
                colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
            ) { Text(label) }
        }
    }
}
