package dev.scharkenberg.purpledisc.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import dev.scharkenberg.purpledisc.R

// Deezer brand purple: the TV app's accent and the seed of the brand schemes below.
val DeezerPurple = Color(0xFFA238FF)

// Behind Now Playing until the cover's own color is read.
val CoverFallback = Color(0xFF2A2433)

private val BrandLight = lightColorScheme(
    primary = Color(0xFF7F12D6),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFF1DBFF),
    onPrimaryContainer = Color(0xFF2E004E),
    secondaryContainer = Color(0xFFEDDDF6),
    onSecondaryContainer = Color(0xFF241A2C),
    tertiary = Color(0xFFA0307E),
    onTertiary = Color(0xFFFFFFFF),
    tertiaryContainer = Color(0xFFFFD7F0),
    onTertiaryContainer = Color(0xFF3D0030),
    background = Color(0xFFFFF7FE),
    onBackground = Color(0xFF1E1A21),
    surface = Color(0xFFFFF7FE),
    onSurface = Color(0xFF1E1A21),
    surfaceVariant = Color(0xFFEBDFEC),
    onSurfaceVariant = Color(0xFF4C4450),
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFFAF1F9),
    surfaceContainer = Color(0xFFF4EBF4),
    surfaceContainerHigh = Color(0xFFEEE5EE),
    surfaceContainerHighest = Color(0xFFE8E0E8),
    outlineVariant = Color(0xFFCEC3D0),
    error = Color(0xFFBA1A1A),
    onError = Color(0xFFFFFFFF),
    errorContainer = Color(0xFFFFDAD6),
    onErrorContainer = Color(0xFF410002),
)

private val BrandDark = darkColorScheme(
    primary = Color(0xFFDFB7FF),
    onPrimary = Color(0xFF4B0080),
    primaryContainer = DeezerPurple,
    onPrimaryContainer = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFF4D3F57),
    onSecondaryContainer = Color(0xFFEDDDF6),
    tertiary = Color(0xFFFFAEE3),
    onTertiary = Color(0xFF5E0F4D),
    tertiaryContainer = Color(0xFFB8298F),
    onTertiaryContainer = Color(0xFFFFD7F0),
    background = Color(0xFF151218),
    onBackground = Color(0xFFE8E0E8),
    surface = Color(0xFF151218),
    onSurface = Color(0xFFE8E0E8),
    surfaceVariant = Color(0xFF4C4450),
    onSurfaceVariant = Color(0xFFCEC3D0),
    surfaceContainerLowest = Color(0xFF100D13),
    surfaceContainerLow = Color(0xFF1E1A21),
    surfaceContainer = Color(0xFF221E25),
    surfaceContainerHigh = Color(0xFF2C2830),
    surfaceContainerHighest = Color(0xFF37333B),
    outlineVariant = Color(0xFF4C4450),
    error = Color(0xFFFFB4AB),
    onError = Color(0xFF690005),
    errorContainer = Color(0xFF93000A),
    onErrorContainer = Color(0xFFFFDAD6),
)

// Inter, the variable font the build copies to res/font from assets/fonts (OFL,
// assets/fonts/Inter-OFL.txt): from Android 8 each weight a named point on its weight axis.
// Android 7's font loader cannot open the variable file (Minikin: addFont failed): it takes the
// static Regular and SemiBold, so Medium draws as Regular and Bold or Black as SemiBold.
private val Inter = if (Build.VERSION.SDK_INT >= 26) {
    FontFamily(
        listOf(400, 500, 600, 700, 800, 900).map { w ->
            Font(R.font.inter, FontWeight(w), FontStyle.Normal, variationSettings = FontVariation.Settings(FontVariation.weight(w)))
        },
    )
} else {
    FontFamily(Font(R.font.inter_regular, FontWeight.Normal), Font(R.font.inter_semibold, FontWeight.SemiBold))
}

// The Material type scale in Inter: headings semibold and a little tighter, titles semibold. The
// rest drop Material's tracking, which is set for Roboto and reads loose in Inter.
private val AppTypography = Typography().run {
    fun TextStyle.inter(weight: FontWeight = fontWeight ?: FontWeight.Normal, tighter: Boolean = false) =
        copy(fontFamily = Inter, fontWeight = weight, letterSpacing = if (tighter) (-0.02).em else 0.sp)
    Typography(
        displayLarge = displayLarge.inter(FontWeight.SemiBold, tighter = true),
        displayMedium = displayMedium.inter(FontWeight.SemiBold, tighter = true),
        displaySmall = displaySmall.inter(FontWeight.SemiBold, tighter = true),
        headlineLarge = headlineLarge.inter(FontWeight.SemiBold, tighter = true),
        headlineMedium = headlineMedium.inter(FontWeight.SemiBold, tighter = true),
        headlineSmall = headlineSmall.inter(FontWeight.SemiBold, tighter = true),
        titleLarge = titleLarge.inter(FontWeight.SemiBold),
        titleMedium = titleMedium.inter(FontWeight.SemiBold),
        titleSmall = titleSmall.inter(FontWeight.SemiBold),
        bodyLarge = bodyLarge.inter(),
        bodyMedium = bodyMedium.inter(),
        bodySmall = bodySmall.inter(),
        labelLarge = labelLarge.inter(FontWeight.SemiBold),
        labelMedium = labelMedium.inter(FontWeight.Medium),
        labelSmall = labelSmall.inter(FontWeight.Medium),
    )
}

private val AppShapes = Shapes(
    small = RoundedCornerShape(8.dp),
    medium = RoundedCornerShape(12.dp),
    large = RoundedCornerShape(16.dp),
)

/** Whether dynamic color (Material You) is possible on this device (API 31+). */
val materialYouSupported: Boolean get() = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S

/** The dynamic-color setting and its switch, provided from the Activity. */
data class MaterialYouState(
    val enabled: Boolean = true,
    val set: (Boolean) -> Unit = {},
)

val LocalMaterialYou = staticCompositionLocalOf { MaterialYouState() }

/**
 * The app theme: light or dark as the system is (or always dark, for TV), with the system's
 * dynamic colors when [dynamicColor] is on and the device has them, else the brand schemes.
 */
@Composable
fun PurpleDiscTheme(
    dynamicColor: Boolean = true,
    dark: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    val context = LocalContext.current
    // Built once per choice: a new scheme object would redraw every screen (MaterialTheme
    // provides it as a static local). A wallpaper change recreates the activity, and with it this.
    val colorScheme = remember(dynamicColor, dark) {
        when {
            dynamicColor && materialYouSupported -> if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
            dark -> BrandDark
            else -> BrandLight
        }
    }
    MaterialTheme(colorScheme = colorScheme, typography = AppTypography, shapes = AppShapes, content = content)
}
