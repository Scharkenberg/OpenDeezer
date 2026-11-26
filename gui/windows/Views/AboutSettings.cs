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
    private readonly Button _download;
    private string _url = "";

    public UIElement[] Cards { get; }

    public AboutSettings()
    {
        _download = Ui.Button(Loc.S("Btn_Download"), () => Launch(_url));
        _update = Ui.Setting("Settings_CheckForUpdates", "Settings_UpdateCheckDesc", Icons.Update,
            Ui.Row(Space.S, Ui.Button(Loc.S("Settings_CheckForUpdates"), Check), _download));
        Cards =
        [
            Ui.Setting("About_Title", null, Icons.Info, Ui.Text("Purple Disc for Deezer " + Config.AppVersion(), TextKind.Body)),
            Ui.Setting("About_Tagline", "About_Credits", Icons.Code, Ui.Stack(0,
                Ui.Link(Source, () => Launch(Https + Source)),
                Ui.Link(WindowsAuthor, () => Launch(Https + WindowsAuthor)))),
            _update,
        ];
    }

    public void Show(UpdateInfo? info)
    {
        _download.Visibility = Ui.Show(info is { HasUpdate: true });
        _url = info?.Url ?? "";
        if (info is { HasUpdate: true }) _update.Description = Loc.Format("Settings_UpdateAvailFormat", info.Latest, info.Current);
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
