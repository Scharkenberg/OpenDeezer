// Settings, built once. Saved values show at once; values the engine owns fill in on each
// visit. Every change applies and is saved immediately.

using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using CommunityToolkit.WinUI.Controls;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class SettingsView : View
{
    private readonly StackPanel _account = Ui.Stack(0);
    private readonly SettingsCard _accountCard, _qualityCard, _adsCard;
    private readonly ComboBox _quality;
    private readonly ToggleSwitch _ads;
    private readonly DeviceSetting _device = new();
    private readonly FolderSetting _folder, _cacheFolder;
    private readonly CacheSetting _cache;
    private readonly RemoteControl _remote = new();
    private readonly AboutSettings _about = new();
    private readonly ScrollViewer _scroller;

    public override FrameworkElement Root => _scroller;
    public override ScrollViewer? Scroller => _scroller;

    private static Settings S => Session.Settings;

    public SettingsView()
    {
        _accountCard = Ui.Setting("Account_Title", null, Icons.Person, _account);
        var storage = new StorageGroup();
        _cache = new CacheSetting(storage);
        _cacheFolder = new FolderSetting("Settings_CacheFolder", Icons.Folder, storage, DeezerCore.SetMediaCacheDir, "Settings_CacheFolderConfirm");
        _folder = new FolderSetting("Settings_DownloadFolder", Icons.Folder, storage,
            dir => DeezerCore.SetDownloadDir(dir) ? "" : Loc.S("Error_FolderRefused"));
        _quality = QuickSettings.Quality();
        _qualityCard = Ui.Setting("Settings_AudioQuality", null, Icons.Quality, _quality);
        // The warning follows the quality wherever it is changed.
        QuickSettings.Follow(_qualityCard, ShowQualityWarning);
        // A button, not a clickable card: the toolkit card throws on UIA Invoke.
        var eq = Ui.IconButton(Icons.ChevronRight, "Settings_Equalizer", () => Nav.Push<EqualizerView>());
        var languages = new List<string>();
        foreach (var l in Loc.Languages) languages.Add(l.Tag.Length == 0 ? Loc.S("Settings_LanguageSystem") : l.Native);
        var language = Ui.Combo([.. languages], Math.Max(0, Array.FindIndex(Loc.Languages, l => l.Tag == S.Language)), SetLanguage);
        _ads = Ui.Switch(false, "Settings_DisableAds", off => Engine.Post(() => DeezerCore.SetAdsDisabled(off)));
        _adsCard = Ui.Setting("Settings_DisableAds", "Settings_DisableAdsDisclaimer", Icons.Warning, _ads);

        var page = Ui.Stack(Space.XS,
            _accountCard,
            Ui.Group("Settings_GroupPlayback"),
            _qualityCard,
            _device.Root,
            Following("Settings_GaplessPlayback", "Settings_GaplessOn", "Settings_GaplessOff", Icons.Play, QuickSettings.Gapless(), () => PlaybackOptions.Gapless),
            Ui.Setting("Settings_Crossfade", null, Icons.Crossfade, QuickSettings.Crossfade()),
            Following("Settings_VolumeNormalization", "Settings_ReplayGainOn", "Settings_ReplayGainOff", Icons.Volume, QuickSettings.ReplayGain(), () => PlaybackOptions.ReplayGain),
            Ui.Setting("Settings_Equalizer", "Settings_EqualizerDesc", Icons.Equalizer, eq),
            Ui.Group("Settings_GroupApp"),
            Ui.SettingToggle("Settings_BackgroundPlayback", "Settings_TrayOn", "Settings_TrayOff", Icons.Tray, S.CloseToTray, on =>
            {
                S.CloseToTray = on;
                Session.SaveSettingsSoon();
            }),
            Ui.Setting("Settings_Language", null, Icons.Globe, language),
            _adsCard,
            Ui.Group("Settings_GroupStorage"),
            _cache.SizeCard,
            _cache.AgeCard,
            _cacheFolder.Root,
            _cache.ClearCard,
            _folder.Root,
            Ui.Group("Settings_RemoteControl"),
            _remote.Root,
            Ui.Group("Settings_GroupAbout"));
        foreach (var card in _about.Cards) page.Children.Add(card);
        _scroller = Ui.Page(Size.ContentMax, Ui.PageTitle(Loc.S("Settings_Title")), page);
        _cacheFolder.Applied += _cache.Refresh;
        // The engine goes on with the fetches the move stopped: their progress shows.
        _cacheFolder.Applied += () => _ = App.Offline.LoadAsync();
        storage.Failed += async () => await ShowStorage();
    }

    public override async void Show(object? arg, bool isBack)
    {
        bool signedIn = Session.SignedIn;
        _accountCard.Visibility = Ui.Show(signedIn);
        // One row: the name and plan as the header, the actions beside them.
        _accountCard.Header = AccountPanel.Name;
        _accountCard.Description = AccountPanel.Plan;
        _account.Children.Clear();
        if (signedIn) _account.Children.Add(AccountPanel.Actions(() => { }));
        _adsCard.Visibility = Ui.Show(Session.IsFree);
        _about.Show(Session.Update);
        var ct = Ct;
        try
        {
            var st = await Task.Run(() => (Devices: DeezerCore.AudioDevices().Devices, Device: DeezerCore.CurrentAudioDevice(),
                Control: DeezerCore.ControlConfig(), Ads: DeezerCore.AdsDisabled()));
            if (ct.IsCancellationRequested) return;
            _device.Set(st.Devices, st.Device);
            _remote.Set(st.Control);
            Ui.Quietly(_ads, () => _ads.IsOn = st.Ads);
            await ShowStorage();
        }
        catch (Exception ex) when (ex is DllNotFoundException or EntryPointNotFoundException)
        {
            AppLog.Write("settings read failed: " + ex.Message);
            Toast.Error(Loc.S("Settings_Title"), Loc.S("Error_EngineMissing"));
        }
    }

    // The Storage group as the engine has it: on each visit, and after a change failed.
    private async Task ShowStorage()
    {
        var st = await Task.Run(() => (Dir: DeezerCore.DownloadDir(), CacheMb: DeezerCore.MediaCacheMB(),
            CacheAge: DeezerCore.DZMediaCacheMaxAgeDays(), Cache: DeezerCore.MediaCacheStats()));
        _folder.Set(st.Dir);
        _cacheFolder.Set(st.Cache.Dir);
        _cache.Set(st.CacheMb, st.CacheAge, st.Cache);
    }

    private void SetLanguage(int i)
    {
        if (Loc.Languages[i].Tag == S.Language) return;
        S.Language = Loc.Languages[i].Tag;
        Session.SaveSettingsSoon();
        var restart = new Button { Content = Loc.S("Btn_RestartNow") };
        restart.Click += (_, _) => App.Restart();
        Toast.Show(InfoBarSeverity.Informational, Loc.S("Settings_Language"), Loc.S("Settings_LanguageRestart"), restart);
    }

    private void ShowQualityWarning() =>
        _qualityCard.Description = PlaybackOptions.Allowed(PlaybackOptions.Quality) ? null! : Loc.Format("Settings_QualityWarnFormat", Session.Account.Offer);

    // A card whose description follows its switch, wherever the option is changed.
    private static SettingsCard Following(string headerKey, string onKey, string offKey, string glyph, ToggleSwitch sw, Func<bool> on)
    {
        var card = Ui.Setting(headerKey, null, glyph, sw);
        return QuickSettings.Follow(card, () => card.Description = Loc.S(on() ? onKey : offKey));
    }
}
