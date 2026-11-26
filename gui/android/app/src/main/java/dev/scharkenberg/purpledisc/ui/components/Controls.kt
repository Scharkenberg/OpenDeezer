package dev.scharkenberg.purpledisc.ui.components

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import dev.scharkenberg.purpledisc.ui.theme.Space

// The app's buttons and chips: Material's, with rounded rectangles instead of pills and the
// scheme's colors run into each other, so they follow Material You and the brand scheme alike.
// The gradient fills the control's own shape, not the larger touch area around it.

internal const val DISABLED_ALPHA = 0.38f
private const val TERTIARY_MIX = 0.55f

/** A filled button on a primary-to-tertiary gradient; [tonal]: on the container colors. */
@Composable
fun GradientButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    tonal: Boolean = false,
    content: @Composable RowScope.() -> Unit,
) {
    val c = MaterialTheme.colorScheme
    val brush = remember(c, tonal) {
        if (tonal) Brush.linearGradient(listOf(c.primaryContainer, c.tertiaryContainer))
        else Brush.linearGradient(listOf(c.primary, lerp(c.primary, c.tertiary, TERTIARY_MIX)))
    }
    Surface(
        onClick = onClick,
        modifier = modifier.alpha(if (enabled) 1f else DISABLED_ALPHA).semantics { role = Role.Button },
        enabled = enabled,
        shape = MaterialTheme.shapes.medium,
        color = Color.Transparent,
        contentColor = if (tonal) c.onPrimaryContainer else c.onPrimary,
    ) {
        Row(
            Modifier
                .background(brush)
                .defaultMinSize(minWidth = ButtonDefaults.MinWidth, minHeight = ButtonDefaults.MinHeight)
                .padding(ButtonDefaults.ContentPadding),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ProvideTextStyle(MaterialTheme.typography.labelLarge) { content() }
        }
    }
}

/** An outlined button whose frame runs from primary into tertiary. */
@Composable
fun FramedButton(onClick: () -> Unit, modifier: Modifier = Modifier, content: @Composable RowScope.() -> Unit) {
    val c = MaterialTheme.colorScheme
    OutlinedButton(
        onClick = onClick,
        modifier = modifier,
        shape = MaterialTheme.shapes.medium,
        border = BorderStroke(1.dp, Brush.linearGradient(listOf(c.primary, c.tertiary))),
        content = content,
    )
}

/** A button's leading icon, at Material's icon size. */
@Composable
fun ButtonIcon(icon: ImageVector) {
    Icon(icon, contentDescription = null, modifier = Modifier.padding(end = Space.s).size(ButtonDefaults.IconSize))
}

/** A filter chip: a quiet fill, the gradient when selected, no outline. */
@Composable
fun AppChip(
    selected: Boolean,
    onClick: () -> Unit,
    label: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    leadingIcon: (@Composable () -> Unit)? = null,
) {
    val c = MaterialTheme.colorScheme
    val brush = remember(c) { Brush.linearGradient(listOf(c.primaryContainer, c.tertiaryContainer)) }
    Surface(
        selected = selected,
        onClick = onClick,
        modifier = modifier.alpha(if (enabled) 1f else DISABLED_ALPHA).semantics { role = Role.Checkbox },
        enabled = enabled,
        shape = MaterialTheme.shapes.small,
        color = if (selected) Color.Transparent else c.surfaceContainerHigh,
        contentColor = if (selected) c.onPrimaryContainer else c.onSurfaceVariant,
    ) {
        Row(
            (if (selected) Modifier.background(brush) else Modifier)
                .heightIn(min = FilterChipDefaults.Height)
                .padding(start = if (leadingIcon != null) Space.s else Space.l, end = Space.l),
            horizontalArrangement = Arrangement.spacedBy(Space.s),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            leadingIcon?.invoke()
            ProvideTextStyle(MaterialTheme.typography.labelLarge, label)
        }
    }
}
