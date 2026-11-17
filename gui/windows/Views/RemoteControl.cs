// The Remote control settings: the control API (network access, token) and the
// phone remote with its pairing QR code, code and address.

using System;
using System.Security.Cryptography;
using System.Threading.Tasks;
using CommunityToolkit.WinUI.Controls;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media.Imaging;
using Windows.Storage.Streams;

namespace OpenDeezer;

internal sealed class RemoteControl
{
    private const string LanAddress = ":7654";

    private readonly ToggleSwitch _on, _lan, _phone;
    private readonly PasswordBox _token = new() { PlaceholderText = Loc.S("Settings_AccessToken"), PasswordRevealMode = PasswordRevealMode.Peek, MinWidth = Size.FieldMin };
    private readonly Button _generate;
    private readonly Image _qr = new() { Width = Size.QrCode, Height = Size.QrCode };
    private readonly TextBlock _code = new() { Style = Ui.Styles.Get("PairingCodeText"), HorizontalAlignment = HorizontalAlignment.Center };
    private readonly TextBlock _url = Ui.Text("", TextKind.SecondaryCaption, wrap: true);
    private readonly SettingsCard _pairing;
    private (bool On, string Addr, string Token) _applied;
    private readonly LatestValuePump<(bool On, string Addr, string Token)> _send =
        new(c => Task.Run(() => DeezerCore.DZSetControlConfig(c.On ? 1 : 0, c.Addr, c.Token)));

    public SettingsExpander Root { get; }

    public RemoteControl()
    {
        _on = Ui.Switch(false, "Settings_RemoteControl", _ => Apply());
        _lan = Ui.Switch(false, "Settings_RemoteLanOn", _ => Apply());
        _phone = Ui.Switch(false, "Settings_PhoneRemote", SetPhone);
        _token.LostFocus += (_, _) => Apply();
        _token.KeyDown += (_, e) =>
        {
            if (e.Key == Windows.System.VirtualKey.Enter) Apply();
        };
        _generate = Ui.Button(Loc.S("Btn_Generate"), () =>
        {
            _token.Password = Convert.ToHexString(RandomNumberGenerator.GetBytes(16)).ToLowerInvariant();
            _token.PasswordRevealMode = PasswordRevealMode.Visible; // so it can be typed on the phone
            Apply();
        });
        _url.TextAlignment = TextAlignment.Center;
        var pairing = Ui.Stack(Space.S, _qr, _code, _url);
        pairing.HorizontalAlignment = HorizontalAlignment.Center;
        _pairing = Ui.Compact(new SettingsCard { Content = pairing, ContentAlignment = ContentAlignment.Vertical, Visibility = Visibility.Collapsed });
        Root = Ui.Compact(new SettingsExpander
        {
            Header = Loc.S("Settings_RemoteControl"),
            Description = Loc.S("Settings_RemoteDesc"),
            HeaderIcon = new FontIcon { Glyph = Icons.Phone },
            Content = _on,
        });
        Root.Items.Add(Ui.Setting("Settings_RemoteLanOn", "Settings_RemoteLanCaption", null, _lan));
        Root.Items.Add(Ui.Setting("Settings_AccessToken", null, null, Ui.Grid("* auto", Space.S, _token, _generate)));
        Root.Items.Add(Ui.Setting("Settings_PhoneRemote", "PhoneRemote_Instructions", null, _phone));
        Root.Items.Add(_pairing);
    }

    public async void Set(ControlConfig c)
    {
        // A config read while a change is still being sent predates it: the switches keep the change.
        if (!_send.Busy)
        {
            _applied = (c.Enabled, c.Lan ? LanAddress : "", c.Token);
            Ui.Quietly(_on, () => _on.IsOn = c.Enabled);
            Ui.Quietly(_lan, () => _lan.IsOn = c.Lan);
            _token.Password = c.Token;
            Root.IsExpanded = c.Enabled;
            EnableParts();
        }
        var info = await Task.Run(DeezerCore.WebRemoteInfo);
        Ui.Quietly(_phone, () => _phone.IsOn = info.Enabled);
        if (info.Enabled) await ShowPairing();
        else _pairing.Visibility = Visibility.Collapsed;
    }

    private void EnableParts()
    {
        _lan.IsEnabled = _on.IsOn;
        _token.IsEnabled = _on.IsOn;
        _generate.IsEnabled = _on.IsOn;
    }

    // DZSetControlConfig restarts the control server and drops paired phones: no-op changes are
    // skipped, and one call runs at a time (back-to-back rebuilds can fail to bind).
    private void Apply()
    {
        EnableParts();
        var want = (_on.IsOn, _lan.IsOn ? LanAddress : "", _token.Password);
        if (want == _applied) return;
        _applied = want;
        _send.Post(want);
    }

    private async void SetPhone(bool on)
    {
        await Task.Run(() => DeezerCore.DZWebRemoteSetEnabled(on ? 1 : 0));
        if (on) await ShowPairing();
        else _pairing.Visibility = Visibility.Collapsed;
    }

    private async Task ShowPairing()
    {
        var (info, png) = await Task.Run(() => (DeezerCore.WebRemoteInfo(), DeezerCore.WebRemoteQRPng()));
        _code.Text = info.Code;
        _url.Text = info.Url;
        _pairing.Visibility = Visibility.Visible;
        if (png.Length == 0) return;
        using var stream = new InMemoryRandomAccessStream();
        using (var writer = new DataWriter(stream))
        {
            writer.WriteBytes(png);
            await writer.StoreAsync();
            writer.DetachStream();
        }
        stream.Seek(0);
        var bmp = new BitmapImage();
        _qr.Source = bmp;
        await bmp.SetSourceAsync(stream);
    }
}
