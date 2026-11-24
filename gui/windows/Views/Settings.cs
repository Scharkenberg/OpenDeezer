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
    private readonly SettingsCard _accountCard, _qualityCard, _adsCard, _explicitCard;
    private readonly ComboBox _explicit;
    private string[] _explicitLevels = [];
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
        // Built once; the card shows only while signed in.
        _account.Children.Add(Ui.Stack(Space.S, AccountPanel.Actions(() => { }), AccountPanel.ManagePlan()));
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
        var singleDevice = Ui.Switch(PlaybackOptions.SingleDevice, "Settings_SingleDevice", PlaybackOptions.SetSingleDevice);
        QuickSettings.Follow(singleDevice, () => Ui.Quietly(singleDevice, () => singleDevice.IsOn = PlaybackOptions.SingleDevice));
        _ads = Ui.Switch(false, "Settings_DisableAds", off => Engine.Post(() => DeezerCore.SetAdsDisabled(off)));
        _adsCard = Ui.Setting("Settings_DisableAds", "Settings_DisableAdsDisclaimer", Icons.Warning, _ads);
        _explicit = Ui.Combo([], -1, SetExplicit);
        _explicitCard = Ui.Setting("Settings_Explicit", null, Icons.Warning, _explicit);
        var themes = new[] { "", "light", "dark" };
        var theme = Ui.Combo([Loc.S("Settings_ThemeSystem"), Loc.S("Settings_ThemeLight"), Loc.S("Settings_ThemeDark")],
            Math.Max(0, Array.IndexOf(themes, S.Theme)), i =>
            {
                S.Theme = themes[i];
                Session.SaveSettingsSoon();
                Dialogs.ApplyTheme();
            });
        var page = Ui.Stack(Space.XS,
            _accountCard,
            Ui.Group("Settings_GroupPlayback"),
            _qualityCard,
            _explicitCard,
            _device.Root,
            Following("Settings_GaplessPlayback", "Settings_GaplessOn", "Settings_GaplessOff", Icons.Play, QuickSettings.Gapless(), () => PlaybackOptions.Gapless),
            Ui.Setting("Settings_Crossfade", null, Icons.Crossfade, QuickSettings.Crossfade()),
            Following("Settings_VolumeNormalization", "Settings_ReplayGainOn", "Settings_ReplayGainOff", Icons.Volume, QuickSettings.ReplayGain(), () => PlaybackOptions.ReplayGain),
            Ui.Setting("Settings_Equalizer", "Settings_EqualizerDesc", Icons.Equalizer, eq),
            Following("Settings_SingleDevice", "Settings_SingleDeviceOn", "Settings_SingleDeviceOff", Icons.Speakers, singleDevice, () => PlaybackOptions.SingleDevice),
            Ui.Group("Settings_GroupApp"),
            Ui.SettingToggle("Settings_BackgroundPlayback", "Settings_TrayOn", "Settings_TrayOff", Icons.Tray, S.CloseToTray, on =>
            {
                S.CloseToTray = on;
                Session.SaveSettingsSoon();
            }),
            Ui.Setting("Settings_Language", null, Icons.Globe, language),
            Ui.Setting("Settings_Theme", null, Icons.Settings, theme),
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
        Session.AccountChanged += RenderAccount;
        RenderAccount();
        _about.Show(Session.Update);
        var ct = Ct;
        try
        {
            bool signedIn = Session.SignedIn;
            var keptExplicit = Session.Explicit;
            var st = await Task.Run(() => (Devices: DeezerCore.AudioDevices().Devices, Device: DeezerCore.CurrentAudioDevice(),
                Control: DeezerCore.ControlConfig(), Ads: DeezerCore.AdsDisabled(), Explicit: !signedIn ? new ExplicitContent() : keptExplicit ?? DeezerCore.Explicit()));
            if (ct.IsCancellationRequested) return;
            _device.Set(st.Devices, st.Device);
            _remote.Set(st.Control);
            Ui.Quietly(_ads, () => _ads.IsOn = st.Ads);
            if (signedIn && st.Explicit.Error.Length == 0) Session.Explicit = st.Explicit;
            ShowExplicit(st.Explicit);
            await ShowStorage();
        }
        catch (Exception ex) when (ex is DllNotFoundException or EntryPointNotFoundException)
        {
            AppLog.Write("settings read failed: " + ex.Message);
            Toast.Error(Loc.S("Settings_Title"), Loc.S("Error_EngineMissing"));
        }
    }

    public override void Hide() => Session.AccountChanged -= RenderAccount;

    // Who is signed in: on each visit, and when the account finishes loading while the page shows.
    private void RenderAccount()
    {
        bool signedIn = Session.SignedIn;
        _accountCard.Visibility = Ui.Show(signedIn);
        // One row: the name and plan as the header, the actions beside them.
        _accountCard.Header = AccountPanel.Name;
        _accountCard.Description = AccountPanel.Plan;
        _adsCard.Visibility = Ui.Show(Session.IsFree);
    }

    // Deezer's three levels, offered when the account may change them.
    private void ShowExplicit(ExplicitContent e)
    {
        _explicitCard.Visibility = Ui.Show(e.CanEdit && e.Available.Count > 0);
        _explicitLevels = [.. e.Available];
        Ui.Quietly(_explicit, () =>
        {
            _explicit.Items.Clear();
            foreach (var l in _explicitLevels)
                _explicit.Items.Add(Loc.S(l switch { "explicit_hide" => "Explicit_Hide", "explicit_no_recommendation" => "Explicit_NoRecommendation", _ => "Explicit_Display" }));
            _explicit.SelectedIndex = Array.IndexOf(_explicitLevels, e.Level);
        });
    }

    private async void SetExplicit(int i)
    {
        if (i < 0 || i >= _explicitLevels.Length) return;
        string level = _explicitLevels[i];
        if (await Task.Run(() => DeezerCore.DZSetExplicitLevel(level)) == 0)
        {
            // Refused: the level Deezer keeps shows again.
            Ui.Quietly(_explicit, () => _explicit.SelectedIndex = Array.IndexOf(_explicitLevels, Session.Explicit?.Level));
            Toast.Error(Loc.S("Error_Generic"), "");
        }
        else if (Session.Explicit is { } kept) Session.Explicit = kept with { Level = level };
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
