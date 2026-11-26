package dev.scharkenberg.purpledisc.tv

import android.Manifest
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.scharkenberg.purpledisc.AppViewModel
import dev.scharkenberg.purpledisc.ui.theme.PurpleDiscTheme

/**
 * Android TV entry point. Reuses the shared [AppViewModel] / engine / player and
 * renders a D-pad-driven, 10-foot Compose UI ([TvApp]) instead of the touch app.
 */
class TvActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        // The TV app is always dark.
        enableEdgeToEdge(SystemBarStyle.dark(Color.TRANSPARENT), SystemBarStyle.dark(Color.TRANSPARENT))
        super.onCreate(savedInstanceState)
        // Android 17+: Purple Disc Connect discovery and the Connect host need it.
        if (Build.VERSION.SDK_INT >= 37 &&
            checkSelfPermission(Manifest.permission.ACCESS_LOCAL_NETWORK) != PackageManager.PERMISSION_GRANTED
        ) {
            registerForActivityResult(ActivityResultContracts.RequestPermission()) {}
                .launch(Manifest.permission.ACCESS_LOCAL_NETWORK)
        }
        setContent {
            PurpleDiscTheme(dynamicColor = false, dark = true) {
                val vm: AppViewModel = viewModel()
                TvApp(vm)
            }
        }
    }
}
