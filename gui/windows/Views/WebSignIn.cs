// Deezer's own sign-in page in WebView2; the title-bar Back cancels it. The
// "arl" cookie is HttpOnly, so it is read through the cookie manager.

using System;
using System.Threading.Tasks;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.Web.WebView2.Core;

namespace OpenDeezer;

internal sealed class WebSignInView : View
{
    private readonly Grid _host = new();
    private readonly StateView _state;
    private readonly DispatcherQueueTimer _timer;
    private WebView2? _web;
    private Func<string, Task>? _onArl;
    private string _staleArl = "";
    private bool _done;

    public override FrameworkElement Root => _state.Root;

    public WebSignInView()
    {
        _state = new StateView(_host, SkeletonShape.Page);
        // Cookies are checked on every navigation; the timer catches a cookie set by script.
        _timer = DispatcherQueue.GetForCurrentThread().CreateTimer();
        _timer.Interval = TimeSpan.FromMilliseconds(Timing.CookieCheckMs);
        _timer.Tick += (_, _) => CheckCookie();
    }

    public override async void Show(object? arg, bool isBack)
    {
        _onArl = arg as Func<string, Task>;
        _done = false;
        var ct = Ct;
        _state.ShowLoading();
        _staleArl = await Task.Run(Config.LoadArlFile); // a login equal to the saved one is never handed back
        await Session.WebProfileReady;
        if (ct.IsCancellationRequested) return;
        try { CoreWebView2Environment.GetAvailableBrowserVersionString(); }
        catch (Exception ex)
        {
            AppLog.Write("WebView2 missing: " + ex.Message);
            _state.ShowEmpty(Icons.Error, Loc.S("Login_WebView2Missing"));
            return;
        }
        _web = new WebView2();
        _web.CoreWebView2Initialized += async (s, e) =>
        {
            if (e.Exception != null)
            {
                AppLog.Write("WebView2 init failed: " + e.Exception);
                _state.ShowEmpty(Icons.Error, Loc.S("Login_WebView2Missing"));
                return;
            }
            // A stale or previous account's session must never be captured: its cookies go. The
            // device's stay, so Deezer sees the device it knows sign in again.
            var cookies = s.CoreWebView2.CookieManager;
            foreach (var c in await cookies.GetCookiesAsync("https://www.deezer.com"))
                if (c.Name is "arl" or "sid" or "refresh-token" or "account_id") cookies.DeleteCookie(c);
            s.CoreWebView2.NavigationCompleted += (_, _) => CheckCookie();
            s.CoreWebView2.SourceChanged += (_, _) => CheckCookie();
            s.CoreWebView2.Navigate("https://www.deezer.com/login");
            _state.ShowContent();
            _timer.Start();
        };
        _host.Children.Add(_web);
        _ = _web.EnsureCoreWebView2Async();
    }

    // Signs the account in again after Deezer ended its session (the notice and the offline page
    // offer it); the queue stays when it is the same account.
    public static void SignInAgain() =>
        App.Navigator.Push<WebSignInView>(new Func<string, Task>(arl => App.Session.SignInAsync(arl)));

    public override void Hide()
    {
        _timer.Stop();
        if (_web == null) return;
        _host.Children.Remove(_web);
        _web.Close(); // lets msedgewebview2.exe exit now
        _web = null;
    }

    private async void CheckCookie()
    {
        if (_done || _web?.CoreWebView2 is not { } core) return;
        var cookies = await core.CookieManager.GetCookiesAsync("https://www.deezer.com");
        // Back while the read was in flight: Hide closed this WebView.
        if (!ReferenceEquals(_web?.CoreWebView2, core)) return;
        foreach (var c in cookies)
        {
            if (c.Name != "arl" || c.Value.Length == 0 || c.Value == _staleArl || _done) continue;
            _done = true;
            var onArl = _onArl;
            Nav.Back();
            if (onArl != null) await onArl(c.Value);
            return;
        }
    }
}
