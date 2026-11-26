package dev.scharkenberg.purpledisc

import dev.scharkenberg.purpledisc.ui.components.LocalFloatingBar
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space
import dev.scharkenberg.purpledisc.ui.components.LocalOfflineIds
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowCompat
import android.app.Activity
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import dev.scharkenberg.purpledisc.ui.LocalWindowLayout
import dev.scharkenberg.purpledisc.ui.WindowLayout
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.ui.unit.dp
import dev.scharkenberg.purpledisc.ui.components.LogoMark
import dev.scharkenberg.purpledisc.ui.components.DRAW_IN_MS
import dev.scharkenberg.purpledisc.ui.components.launchMarkShown
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.foundation.background
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.input.pointer.pointerInput
import kotlinx.coroutines.delay
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.LibraryMusic
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.navigation.NavBackStackEntry
import androidx.compose.animation.AnimatedContentScope
import androidx.compose.animation.EnterExitState
import androidx.compose.runtime.SideEffect
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import dev.scharkenberg.purpledisc.engine.ListenContext
import dev.scharkenberg.purpledisc.player.eventMessage
import dev.scharkenberg.purpledisc.ui.LocalFoldState
import dev.scharkenberg.purpledisc.ui.components.LocalOfflineMode
import dev.scharkenberg.purpledisc.ui.components.LocalPlayingId
import dev.scharkenberg.purpledisc.ui.components.LocalTrackSheet
import dev.scharkenberg.purpledisc.ui.components.PlayerBar
import dev.scharkenberg.purpledisc.ui.components.TrackSheet
import dev.scharkenberg.purpledisc.ui.components.TrackSheetRequest
import dev.scharkenberg.purpledisc.ui.components.UpdateBanner
import dev.scharkenberg.purpledisc.ui.screens.ArtistProfileScreen
import dev.scharkenberg.purpledisc.ui.screens.ChartsScreen
import dev.scharkenberg.purpledisc.ui.screens.CollectionScreen
import dev.scharkenberg.purpledisc.ui.screens.ConnectDialog
import dev.scharkenberg.purpledisc.ui.screens.DownloadedScreen
import dev.scharkenberg.purpledisc.ui.screens.EpisodesScreen
import dev.scharkenberg.purpledisc.ui.screens.EqualizerScreen
import dev.scharkenberg.purpledisc.ui.screens.HistoryScreen
import dev.scharkenberg.purpledisc.ui.screens.HomeScreen
import dev.scharkenberg.purpledisc.ui.screens.LibraryListScreen
import dev.scharkenberg.purpledisc.ui.screens.LibraryScreen
import dev.scharkenberg.purpledisc.ui.screens.LoginScreen
import dev.scharkenberg.purpledisc.ui.screens.LyricsScreen
import dev.scharkenberg.purpledisc.ui.screens.NoInternetScreen
import dev.scharkenberg.purpledisc.ui.screens.NowPlayingScreen
import dev.scharkenberg.purpledisc.ui.screens.PlaylistsBookScreen
import dev.scharkenberg.purpledisc.ui.screens.PlaylistsScreen
import dev.scharkenberg.purpledisc.ui.screens.QueueScreen
import dev.scharkenberg.purpledisc.ui.screens.SearchScreen
import dev.scharkenberg.purpledisc.ui.screens.SettingsScreen
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

private const val TRANSITION_MS = 260

@Composable
fun PurpleDiscApp(vm: AppViewModel) {
    Column(Modifier.fillMaxSize()) {
        vm.updateInfo?.let { info ->
            UpdateBanner(info = info, onDismiss = vm::dismissUpdate, modifier = Modifier.fillMaxWidth())
        }
        BoxWithConstraints(Modifier.weight(1f)) {
          val layout = remember(maxWidth, maxHeight) { WindowLayout.of(maxWidth, maxHeight) }
          CompositionLocalProvider(LocalWindowLayout provides layout) {
            when (vm.stage) {
                AuthStage.LOADING -> {}

                AuthStage.NEEDS_LOGIN -> LoginScreen(
                    busy = vm.busy,
                    error = vm.loginError,
                    onArl = { arl, auto -> vm.login(arl, auto = auto) },
                )

                AuthStage.NO_INTERNET -> NoInternetScreen(
                    busy = vm.busy,
                    onRetry = vm::retry,
                )

                AuthStage.READY -> MainScaffold(vm)
            }
            LaunchOverlay(loading = vm.stage == AuthStage.LOADING)
          }
        }
    }
}

/**
 * The launch mark over the stage's content: the mark drawing itself in on the circle the window
 * starts with, at its size (192 dp box) and place (the window's center), each time the stage enters
 * LOADING. It stays while the sign-in runs or its draw-in does, then fades out; a spinner shows
 * below it when the sign-in takes longer than [SLOW_SIGN_IN_MS].
 */
@Composable
private fun LaunchOverlay(loading: Boolean) {
    val isLoading by rememberUpdatedState(loading)
    val drawIn = remember { Animatable(if (loading) 0f else 1f) }
    LaunchedEffect(Unit) {
        snapshotFlow { isLoading }.collect {
            if (it) {
                drawIn.snapTo(0f)
                drawIn.animateTo(1f, tween(DRAW_IN_MS, easing = LinearEasing))
            }
        }
    }
    var slow by remember { mutableStateOf(false) }
    LaunchedEffect(loading) {
        slow = false
        if (!loading) return@LaunchedEffect
        delay(SLOW_SIGN_IN_MS)
        slow = true
    }
    val shown by remember { derivedStateOf { isLoading || drawIn.value < 1f } }
    AnimatedVisibility(shown, enter = EnterTransition.None, exit = fadeOut(tween(TRANSITION_MS / 2))) {
        DisposableEffect(Unit) {
            launchMarkShown = true
            onDispose { launchMarkShown = false }
        }
        Box(
            Modifier
                .fillMaxSize()
                .background(MaterialTheme.colorScheme.background)
                .pointerInput(Unit) { awaitPointerEventScope { while (true) awaitPointerEvent().changes.forEach { it.consume() } } },
            contentAlignment = Alignment.Center,
        ) {
            LogoMark(playingId = null, modifier = Modifier.size(Size.launchMark), drawIn = drawIn)
            if (slow) CircularProgressIndicator(Modifier.offset(y = Size.launchSpinner))
        }
    }
}

private const val SLOW_SIGN_IN_MS = 2_000L

private fun NavBackStackEntry.arg(name: String) = arguments?.getString(name).orEmpty()

@Composable
private fun MainScaffold(vm: AppViewModel) {
    val navController = rememberNavController()
    val player = vm.player
    // What the shell shows, apart from the rest of the player state.
    val showBar by remember(player) { player.state.map { it.current != null || it.showElsewhere }.distinctUntilChanged() }
        .collectAsState(false)
    val connected by remember(player) { player.state.map { it.connectedDevice.isNotBlank() }.distinctUntilChanged() }
        .collectAsState(false)
    val playingId by remember(player) { player.state.map { it.current?.id.orEmpty() }.distinctUntilChanged() }
        .collectAsState("")
    var showConnect by remember { mutableStateOf(false) }
    var sheet by remember { mutableStateOf<TrackSheetRequest?>(null) }
    val snackbarHostState = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current

    fun nav(route: String) = navController.navigate(route) { launchSingleTop = true }
    fun tab(route: String) = navController.navigate(route) {
        popUpTo(navController.graph.findStartDestination().id) { saveState = true }
        launchSingleTop = true
        restoreState = true
    }
    // A double tap never pops the first screen.
    val back: () -> Unit = { if (navController.previousBackStackEntry != null) navController.popBackStack() }
    fun message(text: String) {
        snackbarHostState.currentSnackbarData?.dismiss()
        scope.launch { snackbarHostState.showSnackbar(text) }
    }

    val destination = navController.currentBackStackEntryAsState().value?.destination
    // A full page still current while its transition heads out is dragged back (predictive back):
    // the page under it shows with its bars.
    var leaving by remember { mutableStateOf<String?>(null) }
    val reportLeaving: @Composable AnimatedContentScope.(String) -> Unit = { route ->
        val out = transition.targetState == EnterExitState.PostExit
        SideEffect { if (out) leaving = route else if (leaving == route) leaving = null }
        DisposableEffect(Unit) { onDispose { if (leaving == route) leaving = null } }
    }
    val full = destination?.route in Routes.FULL && leaving != destination?.route
    // Meanwhile the bars lie over the page under it: padding it would shrink it mid-gesture.
    val overlay = leaving != null
    // The mini player's height as drawn: taller with large text.
    var barHeight by remember { mutableStateOf(Size.miniBar + Space.xs * 2) }
    val density = LocalDensity.current
    // Now Playing and the lyrics draw on a dark cover gradient in either theme: light status-bar
    // icons there, the theme's own elsewhere; set again after a rotation, which resets them.
    val onCover = destination?.route == Routes.NOW_PLAYING || destination?.route == Routes.LYRICS
    val lightTheme = MaterialTheme.colorScheme.background.luminance() > 0.5f
    val view = LocalView.current
    val orientation = LocalConfiguration.current.orientation
    LaunchedEffect(onCover, lightTheme, orientation) {
        val window = (view.context as? Activity)?.window ?: return@LaunchedEffect
        WindowCompat.getInsetsController(window, view).isAppearanceLightStatusBars = lightTheme && !onCover
    }
    // The tab a sub-page was opened from stays selected.
    var currentTab by remember { mutableStateOf(Routes.HOME) }
    destination?.route?.takeIf { it in Routes.TABS }?.let { currentTab = it }

    // Player outcomes app-wide (downloads, failed plays, radio, Connect).
    LaunchedEffect(player) { player.events.collect { message(eventMessage(context, it)) } }
    // A failed optimistic Like toggle was reverted.
    LaunchedEffect(player) { player.favoriteFailures.collect { message(context.getString(R.string.favorite_update_failed)) } }

    // A wide or short window takes a navigation rail at the side instead of the bottom bar.
    val rail = LocalWindowLayout.current.twoPane
    val tabs = listOf(
        Triple(Routes.HOME, Icons.Filled.Home, R.string.nav_home),
        Triple(Routes.SEARCH, Icons.Filled.Search, R.string.search_title),
        Triple(Routes.LIBRARY, Icons.Filled.LibraryMusic, R.string.nav_library),
    )
    val offlineIds by player.offlineIds.collectAsState()
    CompositionLocalProvider(
        LocalPlayingId provides playingId,
        LocalOfflineIds provides offlineIds,
        LocalTrackSheet provides { sheet = it },
        LocalOfflineMode provides vm.offlineMode,
    ) {
        // Each page's own Scaffold takes the system bars, so a page can draw under them;
        // the bars' height reaches it as consumed padding.
        Scaffold(
            contentWindowInsets = WindowInsets(0),
            snackbarHost = { SnackbarHost(snackbarHostState, Modifier.padding(bottom = if (showBar && !full) barHeight else 0.dp)) },
            bottomBar = {
                if (!full && !rail) {
                    NavigationBar {
                        tabs.forEach { (route, icon, label) ->
                            NavigationBarItem(
                                selected = currentTab == route,
                                onClick = { currentTab = route; tab(route) },
                                icon = { Icon(icon, contentDescription = null) },
                                label = { Text(stringResource(label)) },
                            )
                        }
                    }
                }
            },
        ) { padding ->
          Row(Modifier.fillMaxSize().padding(if (overlay) PaddingValues() else padding).consumeWindowInsets(padding)) {
            if (rail && !full) {
                NavigationRail {
                    tabs.forEach { (route, icon, label) ->
                        NavigationRailItem(
                            selected = currentTab == route,
                            onClick = { currentTab = route; tab(route) },
                            icon = { Icon(icon, contentDescription = null) },
                            label = { Text(stringResource(label)) },
                        )
                    }
                }
            }
            // Beside the rail, the pages and the mini player leave the start side's system bar to it.
            Box(
                Modifier.weight(1f).fillMaxHeight()
                    .then(if (rail && !full) Modifier.consumeWindowInsets(WindowInsets.safeDrawing.only(WindowInsetsSides.Start)) else Modifier),
            ) {
            CompositionLocalProvider(LocalFloatingBar provides if (showBar) barHeight else 0.dp) {
            NavHost(
                navController = navController,
                startDestination = Routes.HOME,
                modifier = Modifier.fillMaxSize(),
                // Pages fade in with a short slide; the full-screen player pages rise from the
                // mini player and sink back into it.
                enterTransition = {
                    if (targetState.destination.route in Routes.FULL) slideInVertically(tween(TRANSITION_MS)) { it }
                    else fadeIn(tween(TRANSITION_MS)) + slideInHorizontally(tween(TRANSITION_MS)) { it / 12 }
                },
                exitTransition = { fadeOut(tween(TRANSITION_MS / 2)) },
                popEnterTransition = { fadeIn(tween(TRANSITION_MS)) },
                popExitTransition = {
                    if (initialState.destination.route in Routes.FULL) slideOutVertically(tween(TRANSITION_MS)) { it }
                    else fadeOut(tween(TRANSITION_MS / 2)) + slideOutHorizontally(tween(TRANSITION_MS)) { it / 12 }
                },
            ) {
                composable(Routes.HOME) {
                    HomeScreen(player, connected, ::nav, onCast = { showConnect = true })
                }
                composable(Routes.SEARCH) { SearchScreen(player, ::nav) }
                composable(Routes.LIBRARY) { LibraryScreen(vm.offlineMode, ::nav) }
                composable(Routes.LIKED) {
                    // Book posture: the library on the left page, the tracks on the right one.
                    val fold = LocalFoldState.current
                    val hinge = fold.hingeBounds
                    if (fold.isBook && hinge != null) {
                        PlaylistsBookScreen(player = player, hingeBounds = hinge, onBack = back)
                    } else {
                        CollectionScreen("liked", "", stringResource(R.string.home_liked), player, back)
                    }
                }
                composable(Routes.PLAYLISTS) {
                    val fold = LocalFoldState.current
                    val hinge = fold.hingeBounds
                    if (fold.isBook && hinge != null) {
                        PlaylistsBookScreen(player = player, hingeBounds = hinge, onBack = back)
                    } else {
                        PlaylistsScreen(onBack = back, onNavigate = ::nav, onMessage = ::message, scope = scope)
                    }
                }
                composable(Routes.ALBUMS) { LibraryListScreen("album", back, ::nav) }
                composable(Routes.ARTISTS) { LibraryListScreen("artist", back, ::nav) }
                composable(Routes.PODCASTS) { LibraryListScreen("podcast", back, ::nav) }
                composable(Routes.DOWNLOADED) { DownloadedScreen(player, back, ::nav) }
                composable(Routes.HISTORY) { HistoryScreen(player = player, onBack = back) }
                composable(Routes.PLAYLIST) { e -> CollectionScreen("playlist", e.arg("id"), e.arg("name"), player, back) }
                composable(Routes.ALBUM) { e -> CollectionScreen("album", e.arg("id"), e.arg("name"), player, back) }
                composable(Routes.ARTIST) { e -> ArtistProfileScreen(e.arg("id"), e.arg("name"), player, back, ::nav) }
                composable(Routes.PODCAST) { e -> EpisodesScreen(e.arg("id"), e.arg("name"), player, back) }
                composable(Routes.CHARTS) { ChartsScreen(player, back, ::nav) }
                composable(Routes.NOW_PLAYING) {
                    reportLeaving(Routes.NOW_PLAYING)
                    NowPlayingScreen(
                        player = player,
                        onBack = back,
                        onNavigate = ::nav,
                        onCast = { showConnect = true },
                    )
                }
                composable(Routes.LYRICS) {
                    reportLeaving(Routes.LYRICS)
                    LyricsScreen(player = player, onBack = back)
                }
                composable(Routes.QUEUE) {
                    reportLeaving(Routes.QUEUE)
                    QueueScreen(player = player, onBack = back, onMore = { e ->
                        sheet = TrackSheetRequest(e.track, ListenContext.NONE, queueUid = e.uid)
                    })
                }
                composable(Routes.SETTINGS) {
                    SettingsScreen(
                        account = vm.account,
                        offlineMode = vm.offlineMode,
                        onOfflineMode = vm::switchOfflineMode,
                        onBack = back,
                        onEqualizer = { nav(Routes.EQUALIZER) },
                        onLogout = vm::logout,
                    )
                }
                composable(Routes.EQUALIZER) { EqualizerScreen(onBack = back) }
            }
            }
            // The mini player floats over the pages' bottom edge: they scroll on under it.
            androidx.compose.animation.AnimatedVisibility(
                showBar && !full,
                Modifier
                    .align(Alignment.BottomCenter)
                    .padding(bottom = if (overlay) padding.calculateBottomPadding() else 0.dp)
                    .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom + WindowInsetsSides.Horizontal)),
                enter = slideInVertically { it } + fadeIn(),
                exit = slideOutVertically { it } + fadeOut(),
            ) {
                val playerState by player.state.collectAsState()
                Box(Modifier.onSizeChanged { barHeight = with(density) { it.height.toDp() } }) {
                    PlayerBar(
                        state = playerState,
                        progress = player.progress.collectAsState(),
                        onOpen = { nav(Routes.NOW_PLAYING) },
                        onToggle = player::togglePause,
                        onNext = player::next,
                        // The other player's whole queue when it is an Purple Disc on this network.
                        onPlayHere = { t -> scope.launch { if (!player.takeOver(t)) player.playSingle(t, ListenContext.NONE) } },
                    )
                }
            }
            }
          }
        }

        sheet?.let { request ->
            TrackSheet(
                request = request,
                player = player,
                offlineMode = vm.offlineMode,
                onDismiss = { sheet = null },
                onArtist = { id, name -> nav(Routes.artist(id, name)) },
                onAlbum = { id, name -> nav(Routes.album(id, name)) },
                onMessage = ::message,
                scope = scope,
            )
        }
    }

    if (showConnect) {
        ConnectDialog(player = player, onDismiss = { showConnect = false })
    }
}

