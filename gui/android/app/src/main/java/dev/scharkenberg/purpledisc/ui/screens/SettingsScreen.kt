package dev.scharkenberg.purpledisc.ui.screens

import dev.scharkenberg.purpledisc.ui.components.listEnd
import dev.scharkenberg.purpledisc.ui.components.FramedButton
import dev.scharkenberg.purpledisc.ui.components.SectionHeader
import android.content.Intent
import android.graphics.BitmapFactory
import android.net.Uri
import android.text.format.Formatter
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Remove
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import dev.scharkenberg.purpledisc.ui.components.AppIconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderState
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import dev.scharkenberg.purpledisc.BuildConfig
import dev.scharkenberg.purpledisc.R
import dev.scharkenberg.purpledisc.ui.readableWidth
import dev.scharkenberg.purpledisc.data.Prefs
import dev.scharkenberg.purpledisc.engine.Account
import dev.scharkenberg.purpledisc.engine.CacheStats
import dev.scharkenberg.purpledisc.engine.ConnectHostInfo
import dev.scharkenberg.purpledisc.engine.Engine
import dev.scharkenberg.purpledisc.engine.Explicit
import dev.scharkenberg.purpledisc.engine.UpdateInfo
import dev.scharkenberg.purpledisc.engine.WebRemoteInfo
import dev.scharkenberg.purpledisc.ui.theme.LocalMaterialYou
import dev.scharkenberg.purpledisc.ui.theme.Size
import dev.scharkenberg.purpledisc.ui.theme.Space
import dev.scharkenberg.purpledisc.ui.theme.materialYouSupported
import kotlin.math.roundToInt
import kotlinx.coroutines.launch

// Stream-cache stepper (MB): 128 MB steps up to 4 GB.
private const val MEDIA_CACHE_STEP = 128
private const val MEDIA_CACHE_MAX = 4096
internal const val SOURCE_URL = "https://github.com/Scharkenberg/PurpleDisc"

@Composable
fun SettingsScreen(
    account: Account?,
    offlineMode: Boolean,
    onOfflineMode: (Boolean) -> Unit,
    onBack: () -> Unit,
    onEqualizer: () -> Unit,
    onLogout: () -> Unit,
) {
    val context = LocalContext.current
    val prefs = remember(context) { Prefs(context) }
    val scope = rememberCoroutineScope()
    var quality by remember { mutableIntStateOf(Engine.quality()) }
    var replayGain by remember { mutableStateOf(Engine.replayGain()) }
    var gapless by remember { mutableStateOf(Engine.gapless()) }
    val crossfade = remember { SliderState(Engine.crossfadeMs() / 1000f, 11, 0f..12f) }
    var mediaCacheMb by remember { mutableIntStateOf(Engine.mediaCacheMB()) }
    var singleDevice by remember { mutableStateOf(Engine.singleDevice()) }
    var cacheRead by remember { mutableIntStateOf(0) }
    val cache by produceState<CacheStats?>(null, cacheRead) { value = Engine.cacheStats() }
    var explicit by remember { mutableStateOf<Explicit?>(null) }
    val downloadDir by produceState("") { value = Engine.downloadDir() }
    var adsDisabled by remember { mutableStateOf(false) }
    var webRemoteEnabled by remember { mutableStateOf(Engine.webRemoteInfo()?.enabled ?: false) }
    var remoteInfo by remember { mutableStateOf<WebRemoteInfo?>(null) }
    var remoteQR by remember { mutableStateOf<ByteArray?>(null) }
    var connectHostEnabled by remember { mutableStateOf(Engine.connectHostInfo()?.enabled ?: false) }
    var connectHostInfo by remember { mutableStateOf<ConnectHostInfo?>(null) }
    var checkingUpdate by remember { mutableStateOf(false) }
    var updateResult by remember { mutableStateOf<UpdateCheckResult?>(null) }

    LaunchedEffect(offlineMode) { if (!offlineMode) explicit = Engine.explicit() }
    LaunchedEffect(account?.premium) { if (account?.premium == false) adsDisabled = Engine.adsDisabled() }
    LaunchedEffect(connectHostEnabled) { connectHostInfo = if (connectHostEnabled) Engine.connectHostInfo() else null }
    LaunchedEffect(webRemoteEnabled) {
        remoteInfo = if (webRemoteEnabled) Engine.webRemoteInfo() else null
        remoteQR = if (webRemoteEnabled) Engine.webRemoteQRPng() else null
    }

    Scaffold(topBar = { PageBar(stringResource(R.string.settings_title), onBack) }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).readableWidth().verticalScroll(rememberScrollState()).padding(listEnd())) {
            Group(R.string.settings_group_playback)
            val qualities = listOf(
                Triple(R.string.quality_normal, R.string.quality_desc_normal, true),
                Triple(R.string.quality_high, R.string.quality_desc_high, account?.canHq ?: true),
                Triple(R.string.quality_hifi, R.string.quality_desc_flac, account?.canHifi ?: true),
            )
            qualities.forEachIndexed { i, (label, desc, allowed) ->
                Setting(
                    stringResource(label),
                    // A quality the plan lacks says so.
                    stringResource(if (allowed) desc else if (i == 2) R.string.tv_hifi_needs_plan else R.string.tv_high_hifi_needs_plan),
                    enabled = allowed,
                    onClick = {
                        quality = i
                        Engine.setQuality(i)
                    },
                ) { RadioButton(selected = quality == i, onClick = null, enabled = allowed) }
            }
            SwitchRow("ReplayGain", stringResource(R.string.setting_replaygain_sub), replayGain) {
                replayGain = it
                Engine.setReplayGain(it)
            }
            SwitchRow(stringResource(R.string.setting_gapless_title), stringResource(R.string.setting_gapless_sub), gapless) {
                gapless = it
                Engine.setGapless(it)
            }
            Setting(
                stringResource(R.string.settings_crossfade),
                if (crossfade.value <= 0f) stringResource(R.string.common_off) else stringResource(R.string.crossfade_seconds, crossfade.value.roundToInt()),
            )
            Slider(
                state = crossfade,
                onValueChange = { crossfade.value = it },
                onValueChangeFinished = {
                    val ms = (crossfade.value * 1000).toInt()
                    Engine.setCrossfadeMs(ms)
                },
                modifier = Modifier.padding(horizontal = Space.l),
            )
            Setting(stringResource(R.string.equalizer_title), stringResource(R.string.settings_eq_sub), onClick = onEqualizer) {
                Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = null)
            }
            SwitchRow(
                stringResource(R.string.settings_single_device),
                stringResource(if (singleDevice) R.string.single_device_on else R.string.single_device_off),
                singleDevice,
            ) {
                singleDevice = it
                Engine.setSingleDevice(it)
            }
            // The account's setting, shown as it is.
            explicit?.let { e -> Setting(stringResource(R.string.settings_explicit), explicitLabel(e.level)) }
            if (account?.premium == false) {
                SwitchRow(stringResource(R.string.setting_disable_ads_title), stringResource(R.string.setting_disable_ads_disclaimer), adsDisabled) {
                    adsDisabled = it
                    scope.launch { Engine.setAdsDisabled(it) }
                }
            }

            Group(R.string.settings_group_storage)
            SwitchRow(stringResource(R.string.offline_mode), stringResource(R.string.offline_mode_hint), offlineMode, onChange = onOfflineMode)
            Setting(
                stringResource(R.string.settings_stream_cache),
                if (mediaCacheMb <= 0) stringResource(R.string.common_off) else stringResource(R.string.stream_cache_mb, mediaCacheMb),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    AppIconButton(enabled = mediaCacheMb > 0, onClick = { setCache(mediaCacheMb - MEDIA_CACHE_STEP, scope) { mediaCacheMb = it } }) {
                        Icon(Icons.Filled.Remove, contentDescription = stringResource(R.string.cd_decrease))
                    }
                    AppIconButton(enabled = mediaCacheMb < MEDIA_CACHE_MAX, onClick = { setCache(mediaCacheMb + MEDIA_CACHE_STEP, scope) { mediaCacheMb = it } }) {
                        Icon(Icons.Filled.Add, contentDescription = stringResource(R.string.cd_increase))
                    }
                }
            }
            cache?.let { c ->
                val used = Formatter.formatShortFileSize(context, c.bytes)
                val line = if (c.pinnedBytes > 0) {
                    stringResource(R.string.cache_used_offline, used, Formatter.formatShortFileSize(context, c.pinnedBytes))
                } else {
                    stringResource(R.string.cache_used, used)
                }
                Setting(stringResource(R.string.settings_clear_cache), line, onClick = { scope.launch { Engine.clearCache(); cacheRead++ } })
            }
            Setting(stringResource(R.string.download_folder_title), downloadDir.ifBlank { stringResource(R.string.download_folder_default) })

            Group(R.string.settings_group_app)
            if (materialYouSupported) {
                val materialYou = LocalMaterialYou.current
                SwitchRow(stringResource(R.string.settings_material_you), stringResource(R.string.settings_material_you_hint), materialYou.enabled) { materialYou.set(it) }
            }
            SwitchRow(stringResource(R.string.connect_reachable_title), stringResource(R.string.connect_reachable_sub), connectHostEnabled) {
                connectHostEnabled = it
                prefs.connectHostEnabled = it
                scope.launch { Engine.setConnectHostEnabled(it) }
            }
            connectHostInfo?.takeIf { it.enabled && it.addr.isNotBlank() }?.let { info ->
                Setting(stringResource(R.string.settings_connect), stringResource(R.string.reachable_at, info.addr) + (info.name.takeIf { it.isNotBlank() }?.let { " · $it" } ?: ""))
            }
            SwitchRow(stringResource(R.string.settings_phone_remote), stringResource(R.string.phone_remote_sub), webRemoteEnabled) {
                webRemoteEnabled = it
                prefs.phoneRemoteEnabled = it
                scope.launch { Engine.setWebRemoteEnabled(it) }
            }
            if (webRemoteEnabled) RemotePairing(remoteInfo, remoteQR)
            if (BuildConfig.ENABLE_UPSTREAM_UPDATES) {
                Setting(
                    stringResource(R.string.update_check_title),
                    stringResource(if (checkingUpdate) R.string.update_checking else R.string.update_check_sub),
                    enabled = !checkingUpdate,
                    onClick = {
                        checkingUpdate = true
                        scope.launch {
                            val info = Engine.checkUpdate()
                            checkingUpdate = false
                            updateResult = when {
                                info == null -> UpdateCheckResult.Failed
                                info.hasUpdate -> UpdateCheckResult.Available(info)
                                else -> UpdateCheckResult.UpToDate(info.current)
                            }
                        }
                    },
                ) { if (checkingUpdate) CircularProgressIndicator(Modifier.size(Space.l + Space.xs), strokeWidth = 2.dp) }
            }

            Group(R.string.settings_account)
            if (account != null) {
                val plan = stringResource(R.string.plan_label, account.offer.ifBlank { "—" }) +
                    (if (account.canHifi) " · HiFi" else if (account.canHq) " · HQ" else "")
                Setting(account.name, if (account.premium) plan else plan + "\n" + stringResource(R.string.account_free_hint))
            }
            FramedButton(onClick = onLogout, modifier = Modifier.fillMaxWidth().padding(horizontal = Space.l)) {
                Text(stringResource(R.string.action_sign_out))
            }

            Group(R.string.settings_about)
            Setting(stringResource(R.string.app_full_name) + " " + BuildConfig.VERSION_NAME, stringResource(R.string.about_credits))
            Setting(SOURCE_URL.removePrefix("https://"), onClick = { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(SOURCE_URL))) })
        }
    }

    if (BuildConfig.ENABLE_UPSTREAM_UPDATES) UpdateDialog(updateResult, onDismiss = { updateResult = null }) { url ->
        runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))) }
    }
}

// The engine applies it at once and keeps it.
private fun setCache(mb: Int, scope: kotlinx.coroutines.CoroutineScope, set: (Int) -> Unit) {
    val v = mb.coerceIn(0, MEDIA_CACHE_MAX)
    set(v)
    scope.launch { Engine.setMediaCacheMB(v) }
}

@Composable
private fun explicitLabel(level: String) = stringResource(
    when (level) {
        "explicit_hide" -> R.string.explicit_hide
        "explicit_no_recommendation" -> R.string.explicit_no_recommendation
        else -> R.string.explicit_display
    },
)

@Composable
private fun Group(label: Int) {
    SectionHeader(stringResource(label))
}

@Composable
private fun Setting(
    title: String,
    supporting: String? = null,
    enabled: Boolean = true,
    onClick: (() -> Unit)? = null,
    trailing: @Composable (() -> Unit)? = null,
) {
    val sub: (@Composable () -> Unit)? = supporting?.let { { Text(it) } }
    if (onClick != null) {
        ListItem(onClick = onClick, enabled = enabled, supportingContent = sub, trailingContent = trailing) { Text(title) }
    } else {
        ListItem(supportingContent = sub, trailingContent = trailing) { Text(title) }
    }
}

@Composable
internal fun SwitchRow(title: String, supporting: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Setting(title, supporting, onClick = { onChange(!checked) }) { Switch(checked = checked, onCheckedChange = onChange) }
}

// The pairing code, its QR and the link for the phone remote.
@Composable
private fun RemotePairing(info: WebRemoteInfo?, qr: ByteArray?) {
    Column(Modifier.fillMaxWidth().padding(Space.l), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(Space.m)) {
        if (info == null) {
            CircularProgressIndicator()
            return@Column
        }
        Text(stringResource(R.string.phone_remote_scan), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        val image = remember(qr) { qr?.let { BitmapFactory.decodeByteArray(it, 0, it.size)?.asImageBitmap() } }
        if (image != null) Image(bitmap = image, contentDescription = stringResource(R.string.cd_qr_code), modifier = Modifier.size(Size.hero))
        Text(info.code, style = MaterialTheme.typography.displaySmall.copy(fontFeatureSettings = "tnum"), textAlign = TextAlign.Center)
        Text(info.url, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
    }
}

private sealed interface UpdateCheckResult {
    data class UpToDate(val current: String) : UpdateCheckResult
    data class Available(val info: UpdateInfo) : UpdateCheckResult
    data object Failed : UpdateCheckResult
}

@Composable
private fun UpdateDialog(result: UpdateCheckResult?, onDismiss: () -> Unit, onOpen: (String) -> Unit) {
    when (result) {
        is UpdateCheckResult.UpToDate -> AlertDialog(
            onDismissRequest = onDismiss,
            confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.action_ok)) } },
            title = { Text(stringResource(R.string.update_uptodate_title)) },
            text = {
                Text(if (result.current.isBlank()) stringResource(R.string.update_latest_generic) else stringResource(R.string.update_latest_named, result.current))
            },
        )
        is UpdateCheckResult.Available -> AlertDialog(
            onDismissRequest = onDismiss,
            confirmButton = {
                TextButton(onClick = { onDismiss(); onOpen(result.info.url) }) { Text(stringResource(R.string.action_download)) }
            },
            dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.action_close)) } },
            title = { Text(stringResource(R.string.update_available_title, result.info.latest)) },
            text = { Text(result.info.notes.ifBlank { stringResource(R.string.update_available_generic) }, modifier = Modifier.verticalScroll(rememberScrollState())) },
        )
        UpdateCheckResult.Failed -> AlertDialog(
            onDismissRequest = onDismiss,
            confirmButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.action_ok)) } },
            title = { Text(stringResource(R.string.update_failed_title)) },
            text = { Text(stringResource(R.string.update_failed_body)) },
        )
        null -> Unit
    }
}
