package dev.scharkenberg.purpledisc

import android.Manifest
import android.content.pm.PackageManager
import android.graphics.drawable.ColorDrawable
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.scharkenberg.purpledisc.data.Prefs
import dev.scharkenberg.purpledisc.ui.LocalFoldState
import dev.scharkenberg.purpledisc.ui.rememberFoldState
import dev.scharkenberg.purpledisc.ui.theme.LocalMaterialYou
import dev.scharkenberg.purpledisc.ui.theme.MaterialYouState
import dev.scharkenberg.purpledisc.ui.theme.PurpleDiscTheme
import dev.scharkenberg.purpledisc.engine.Engine
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.first
import androidx.lifecycle.lifecycleScope

class MainActivity : ComponentActivity() {
    private val permission = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    /**
     * Asks for [name] when it first matters: the media notification's at the first play
     * (Android 13+), the local network's when Connect or the Connect host is used (Android 17+).
     */
    fun ask(name: String) {
        val since = if (name == Manifest.permission.POST_NOTIFICATIONS) 33 else 37
        if (Build.VERSION.SDK_INT >= since && checkSelfPermission(name) != PackageManager.PERMISSION_GRANTED) permission.launch(name)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        // The system bars follow the theme, which follows the system.
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        // The theme's circle is for the starting window only, never behind a page.
        window.setBackgroundDrawable(ColorDrawable(getColor(R.color.window_background)))
        val player = (application as PurpleDiscApplication).player
        lifecycleScope.launch {
            player.state.first { it.isPlaying }
            ask(Manifest.permission.POST_NOTIFICATIONS)
        }
        lifecycleScope.launch {
            Engine.connectActive.first { it }
            ask(Manifest.permission.ACCESS_LOCAL_NETWORK)
        }
        setContent {
            val prefs = remember { Prefs(this@MainActivity) }
            // Opt-in Material You: read the saved choice, and let the Settings
            // switch flip it live (updates the pref + this state → theme recomposes).
            var dynamicColor by remember { mutableStateOf(prefs.materialYou) }
            PurpleDiscTheme(dynamicColor = dynamicColor) {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background,
                ) {
                    val vm: AppViewModel = viewModel()
                    // Kept across recompositions: the window-layout flow emits again each time the
                    // app returns to the foreground, and a new value of a static local would redraw
                    // the whole app.
                    val materialYou = remember(dynamicColor) {
                        MaterialYouState(enabled = dynamicColor) { v ->
                            prefs.materialYou = v
                            dynamicColor = v
                        }
                    }
                    // Foldable posture (tabletop/book) drives the optional split
                    // layouts; flat devices keep the classic single-pane UI.
                    CompositionLocalProvider(
                        LocalFoldState provides rememberFoldState(this),
                        LocalMaterialYou provides materialYou,
                    ) {
                        PurpleDiscApp(vm)
                    }
                }
            }
        }
    }
}
