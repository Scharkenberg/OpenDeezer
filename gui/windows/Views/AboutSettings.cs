// The About group: version, license and source, and the update check.

using System;
using CommunityToolkit.WinUI.Controls;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Windows.System;

namespace PurpleDisc;

internal sealed class AboutSettings
{
    // Shown without the scheme, opened with it.
    private const string Https = "https://";
    private const string Source = "github.com/Scharkenberg/PurpleDisc";
    private const string WindowsAuthor = "github.com/Scharkenberg";

    private readonly SettingsCard _update;
    private readonly Button _check, _download, _install;
    private readonly ProgressBar _progress = new()
    {
        Width = Size.UpdateProgress,
        Maximum = 100,
        VerticalAlignment = VerticalAlignment.Center,
        Visibility = Visibility.Collapsed,
    };
    private UpdateInfo? _info;

    public UIElement[] Cards { get; }

    public AboutSettings()
    {
        _check = Ui.Button(Loc.S("Settings_CheckForUpdates"), Check);
        _download = Ui.Button(Loc.S("Btn_Download"), () => Launch(_info!.Url));
        _install = Ui.Button(Loc.S("Settings_UpdateNow"), Install, accent: true);
        _update = Ui.Setting("Settings_CheckForUpdates", "Settings_UpdateCheckDesc", Icons.Update,
            Ui.Row(Space.S, _progress, _check, _download, _install));
        Cards =
        [
            Ui.Setting("About_Title", null, Icons.Info, Ui.Text("Purple Disc for Deezer " + Config.AppVersion(), TextKind.Body)),
            Ui.Setting("About_Tagline", "About_Credits", Icons.Code, Ui.Stack(0,
                Ui.Link(Source, () => Launch(Https + Source)),
                Ui.Link(WindowsAuthor, () => Launch(Https + WindowsAuthor)))),
            _update,
        ];
    }

    // The installed copy updates itself; the zip and a dev build open the release page.
    public void Show(UpdateInfo? info)
    {
        _info = info;
        bool has = info is { HasUpdate: true }, install = has && Installer.IsInstalled && info!.Installer.Length > 0;
        _download.Visibility = Ui.Show(has && !install);
        _install.Visibility = Ui.Show(install);
        if (has) _update.Description = Loc.Format("Settings_UpdateAvailFormat", info!.Latest, info.Current);
    }

    private async void Install()
    {
        var info = _info!;
        _check.IsEnabled = _install.IsEnabled = false;
        _progress.Value = 0;
        _progress.Visibility = Visibility.Visible;
        _update.Description = Loc.Format("Settings_UpdateDownloadingFormat", info.Latest, 0);
        bool started = await Session.InstallUpdateAsync(info, percent =>
        {
            _progress.Value = percent;
            _update.Description = Loc.Format("Settings_UpdateDownloadingFormat", info.Latest, percent);
        });
        if (started)
        {
            _progress.IsIndeterminate = true;
            _update.Description = Loc.Format("Settings_UpdateInstallingFormat", info.Latest);
            return;
        }
        _progress.Visibility = Visibility.Collapsed;
        _check.IsEnabled = _install.IsEnabled = true;
        _install.Visibility = Visibility.Collapsed;
        _download.Visibility = Visibility.Visible;
        _update.Description = Loc.S("Settings_UpdateInstallFail");
    }

    private async void Check()
    {
        _update.Description = Loc.S("Settings_Checking");
        var info = await Session.CheckForUpdateAsync();
        if (info.HasUpdate) Show(info);
        else _update.Description = info.Latest.Length == 0 ? Loc.S("Settings_UpdateFail") : Loc.Format("Settings_UpToDateFormat", info.Current);
    }

    private static async void Launch(string url) => await Launcher.LaunchUriAsync(new Uri(url));
}
