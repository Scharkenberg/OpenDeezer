// Signed out: continue with Deezer (web sign-in) or paste an ARL token inline.

using System;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media.Imaging;

namespace PurpleDisc;

internal sealed class SignInView : View
{
    private readonly StateView _state;
    private readonly Button _web, _submit;
    private readonly HyperlinkButton _useArl;
    private readonly PasswordBox _arl = new() { PlaceholderText = Loc.S("Dialog_PasteArlPlaceholder"), PasswordRevealMode = PasswordRevealMode.Peek };
    private readonly StackPanel _arlEntry;
    private readonly InfoBar _error = new() { IsOpen = false, IsClosable = true, Severity = InfoBarSeverity.Error };
    private readonly ProgressBar _progress = new() { IsIndeterminate = true, Visibility = Visibility.Collapsed };

    public override FrameworkElement Root => _state.Root;

    public SignInView()
    {
        var icon = Ui.Logo(Size.AppIcon);
        var title = Ui.Text(Loc.S("Dialog_SignInTitle"), TextKind.PageTitle, wrap: true);
        title.TextAlignment = TextAlignment.Center;
        _web = Ui.Button(Loc.S("Btn_LogInWithDeezer"), ContinueWithDeezer, accent: true);
        _web.HorizontalAlignment = HorizontalAlignment.Stretch;
        _submit = Ui.Button(Loc.S("Btn_SignIn"), SubmitArl);
        _arl.KeyDown += (_, e) =>
        {
            if (e.Key == Windows.System.VirtualKey.Enter) SubmitArl();
        };
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(_arl, Loc.S("Dialog_PasteArlPlaceholder"));
        _arlEntry = Ui.Stack(Space.S, _arl, _submit);
        _arlEntry.Visibility = Visibility.Collapsed;
        _useArl = Ui.Link(Loc.S("Btn_EnterArlManually"), () =>
        {
            _arlEntry.Visibility = Visibility.Visible;
            _arl.Focus(FocusState.Programmatic);
        });
        _useArl.HorizontalAlignment = HorizontalAlignment.Center;
        var column = Ui.Stack(Space.L, icon, title, _web, _useArl, _arlEntry, _error, _progress);
        column.Width = Size.SignInColumn;
        column.HorizontalAlignment = HorizontalAlignment.Center;
        column.VerticalAlignment = VerticalAlignment.Center;
        _state = new StateView(column, SkeletonShape.Rows);
    }

    public override void Show(object? arg, bool isBack)
    {
        if (!isBack && Session.StartupPending) _ = Run(Session.ResumeAsync);
    }

    public override void Hide() => Session.StopNetworkWatch();

    private void ContinueWithDeezer() =>
        Nav.Push<WebSignInView>(new Func<string, Task>(arl => Run(() => Session.SignInAsync(arl))));

    private void SubmitArl()
    {
        string arl = _arl.Password.Trim();
        if (!Session.LooksLikeArl(arl))
        {
            ShowError(Loc.S("Login_ArlInvalid"));
            return;
        }
        _ = Run(() => Session.SignInAsync(arl));
    }

    private async Task Run(Func<Task<SignInResult>> attempt)
    {
        Session.StopNetworkWatch();
        _error.IsOpen = false;
        _state.ShowContent();
        SetBusy(true);
        var r = await attempt();
        if (Session.SignedIn) return;
        SetBusy(false);
        switch (r)
        {
            case SignInResult.Ok:
                break;
            case SignInResult.Offline:
                _state.ShowError(Loc.S("NoInternet_Title") + "\n" + Loc.S("NoInternet_Body"), Retry);
                Session.WatchNetwork(Retry);
                break;
            case SignInResult.Failed:
            case SignInResult.NoAudio:
                _state.ShowError(Loc.S("Login_ErrorTitle") + "\n" + Session.FailureText(r), Retry);
                break;
            default:
                ShowError(Session.FailureText(r));
                break;
        }
    }

    private void Retry() => _ = Run(Session.RetryAsync);

    private void SetBusy(bool busy)
    {
        _progress.Visibility = Ui.Show(busy);
        _web.IsEnabled = !busy;
        _useArl.IsEnabled = !busy;
        _submit.IsEnabled = !busy;
        _arl.IsEnabled = !busy;
    }

    private void ShowError(string text)
    {
        _error.Message = text;
        _error.IsOpen = true;
    }
}
