package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.GradientButton
import dev.scharkenberg.purpledisc.ui.components.FramedButton
import android.annotation.SuppressLint
import android.webkit.CookieManager
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.viewinterop.AndroidView
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.theme.Space

private const val LOGIN_URL = "https://www.deezer.com/login"

private fun readArl(): String? {
    val cm = CookieManager.getInstance()
    val raw = cm.getCookie("https://www.deezer.com") ?: cm.getCookie(".deezer.com") ?: return null
    // Cookie string is "k=v; k2=v2; ...". The arl cookie is httpOnly but CookieManager exposes it.
    return raw.split(";")
        .map { it.trim() }
        .firstOrNull { it.startsWith("arl=") }
        ?.substringAfter("arl=")
        ?.takeIf { it.length > 20 }
}

@SuppressLint("SetJavaScriptEnabled")
@Composable
fun LoginScreen(
    busy: Boolean,
    error: String?,
    // auto = true when the ARL was captured from the WebView cookie jar rather
    // than typed by the user, so a token that already failed isn't retried.
    onArl: (arl: String, auto: Boolean) -> Unit,
) {
    var manual by rememberSaveable { mutableStateOf(false) }
    // Never put a bearer credential into Compose's saved-state Bundle.
    var arlField by remember { mutableStateOf("") }

    Column(Modifier.fillMaxSize().systemBarsPadding()) {
        Column(
            Modifier.fillMaxWidth().padding(Space.l),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                stringResource(R.string.app_name),
                style = MaterialTheme.typography.headlineSmall,
                color = MaterialTheme.colorScheme.primary,
            )
            Text(
                if (manual) stringResource(R.string.login_manual_prompt) else stringResource(R.string.login_web_prompt),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (error != null) {
                Spacer(Modifier.height(Space.s))
                Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            }
            if (busy) {
                Spacer(Modifier.height(Space.m))
                CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
            }
        }

        HorizontalDivider()

        if (manual) {
            Column(
                Modifier.fillMaxSize().padding(Space.l).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(Space.m),
            ) {
                OutlinedTextField(
                    value = arlField,
                    onValueChange = { arlField = it },
                    label = { Text("ARL") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                    // A token: no suggestions, nothing learned by the keyboard.
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false, imeAction = ImeAction.Done),
                )
                GradientButton(
                    onClick = { onArl(arlField.trim(), false) },
                    enabled = !busy && arlField.isNotBlank(),
                    modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.action_login)) }
                FramedButton(
                    onClick = { manual = false },
                    modifier = Modifier.fillMaxWidth(),
                ) { Text(stringResource(R.string.login_use_web)) }
                Text(
                    stringResource(R.string.login_manual_help),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        } else {
            AndroidView(
                modifier = Modifier.fillMaxWidth().weight(1f),
                factory = { ctx ->
                    CookieManager.getInstance().apply {
                        setAcceptCookie(true)
                    }
                    WebView(ctx).apply {
                        CookieManager.getInstance().setAcceptThirdPartyCookies(this, true)
                        settings.javaScriptEnabled = true
                        settings.domStorageEnabled = true
                        webViewClient = object : WebViewClient() {
                            // Redirect chains fire onPageFinished repeatedly;
                            // submit each captured token at most once.
                            private var sent: String? = null
                            override fun onPageFinished(view: WebView?, url: String?) {
                                super.onPageFinished(view, url)
                                CookieManager.getInstance().flush()
                                readArl()?.let {
                                    if (it != sent) {
                                        sent = it
                                        onArl(it, true)
                                    }
                                }
                            }
                        }
                        loadUrl(LOGIN_URL)
                    }
                },
                onRelease = {
                    it.stopLoading()
                    it.loadUrl("about:blank")
                    (it.parent as? android.view.ViewGroup)?.removeView(it)
                    it.removeAllViews()
                    it.destroy()
                },
            )
            FramedButton(
                onClick = { manual = true },
                modifier = Modifier.fillMaxWidth().padding(Space.l),
            ) { Text(stringResource(R.string.login_paste_manual)) }
        }
    }
}
