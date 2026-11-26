package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.GradientButton
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CloudOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.theme.Space

/**
 * Shown when sign-in fails because the device is offline (engine login kind 2).
 * Unlike the login screen it keeps the saved ARL: [onRetry] re-attempts the same
 * credentials so a recovered connection lands the user straight back in the app.
 */
@Composable
fun NoInternetScreen(busy: Boolean, onRetry: () -> Unit) {
    Column(
        Modifier.fillMaxSize().padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(
            Icons.Filled.CloudOff,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.size(64.dp),
        )
        Spacer(Modifier.height(Space.l))
        Text(
            stringResource(R.string.no_internet_title),
            style = MaterialTheme.typography.headlineSmall,
        )
        Spacer(Modifier.height(Space.s))
        Text(
            stringResource(R.string.no_internet_body),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(Space.xl))
        if (busy) {
            CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
        } else {
            GradientButton(onClick = onRetry) { Text(stringResource(R.string.action_retry)) }
        }
    }
}
