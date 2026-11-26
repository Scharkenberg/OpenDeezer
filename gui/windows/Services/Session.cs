// Sign-in, the account, sign-out and the settings file.

using System;
using System.IO;
using System.Linq;
using System.Text.Json;
using System.Threading.Tasks;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml.Controls;
using Windows.Networking.Connectivity;

namespace PurpleDisc;

internal enum SignInResult { Ok, Expired, Offline, Failed, EngineMissing, NoAudio }

internal sealed class Session
{
    private readonly Settings _settings;
    private readonly Library _library;
    private readonly Player _player;
    private readonly Offline _offline;
    private readonly DispatcherQueueTimer _saveTimer;
    private Task<SignInResult>? _startup;
    private Func<Task<SignInResult>>? _lastAttempt;
    private int _gen;

    public Session(DispatcherQueue dq, Settings settings, Library library, Player player, Offline offline)
    {
        _settings = settings;
        _library = library;
        _player = player;
        _offline = offline;
        _saveTimer = dq.CreateTimer();
        _saveTimer.Interval = TimeSpan.FromMilliseconds(Timing.SettingsSaveDebounceMs);
        _saveTimer.IsRepeating = false;
        _saveTimer.Tick += (_, _) =>
        {
            var s = _settings;
            Task.Run(() => Config.SaveSettings(s));
        };
        _library.Stored += OnStored;
    }

    public Settings Settings => _settings;
    public bool SignedIn { get; internal set; }
    // Signed in to the music kept offline only: the sign-in found no network and pins exist.
    public bool IsOffline { get; internal set; }

    // An offline session because Deezer refused the saved login: it ends with a new sign-in.
    public bool Expired { get; private set; }
    public bool Busy { get; private set; }
    public bool StartupPending => _startup != null;
    public Account Account { get; private set; } = new();
    public bool AccountKnown => Account.LoggedIn;
    // Unknown plan counts as neither Free nor gated: the engine refuses Free downloads itself.
    public bool IsFree => AccountKnown && !Account.Premium;
    public bool CanDownload => !IsFree;
    public UpdateInfo? Update { get; private set; }
    public static Task WebProfileReady { get; private set; } = Task.CompletedTask;

    public event Action? SignedInChanged;
    public event Action? AccountChanged;
    public event Action? UpdateFound;

    // Keeps the WebView2 profile out of the install folder; after a sign-out it is deleted before use.
    public static void PrepareWebProfile(Settings s)
    {
        string dir = Path.Combine(LocalDir(), "WebView2");
        Environment.SetEnvironmentVariable("WEBVIEW2_USER_DATA_FOLDER", dir);
        if (!s.WipeWebProfile) return;
        WebProfileReady = Task.Run(async () =>
        {
            for (int i = 0; i < Timing.WipeTries && Directory.Exists(dir); i++)
            {
                try { Directory.Delete(dir, recursive: true); }
                catch (Exception ex)
                {
                    if (i == Timing.WipeTries - 1) AppLog.Write("WebView2 wipe: " + ex.Message);
                    await Task.Delay(Timing.WipeRetryMs);
                }
            }
            if (Directory.Exists(dir)) return;
            s.WipeWebProfile = false;
            Config.SaveSettings(s);
        });
    }

    public void Start()
    {
        // After an in-app sign-in or sign-out, %DEEZER_ARL% must not bring the old account back.
        bool envIgnored = _settings.IgnoredEnvArlHash.Length > 0 && Config.EnvArlHash() == _settings.IgnoredEnvArlHash;
        bool fromEnv = false;
        string arl = envIgnored ? Config.LoadArlFile() : Config.LoadArl(out fromEnv);
        if (arl.Length == 0) return;
        _startup = Task.Run(async () =>
        {
            var r = await TryLogin(arl);
            // A stale %DEEZER_ARL% must not shadow a good saved login.
            if (r == SignInResult.Expired && fromEnv && Config.LoadArlFile() is { Length: > 0 } file && file != arl) r = await TryLogin(file);
            return r;
        });
    }

    public async Task<SignInResult> ResumeAsync()
    {
        var startup = _startup;
        if (startup == null) return SignInResult.Expired;
        _lastAttempt = () =>
        {
            Start();
            return ResumeAsync();
        };
        int g = ++_gen;
        Busy = true;
        // The account is most likely the last one: its cached Home paints while the login runs.
        string painted = _settings.LastUserId;
        if (painted.Length > 0) await PaintFromCacheAsync(g, painted);
        var r = await startup;
        Busy = false;
        _startup = null;
        if (g != _gen) return r;
        if (r == SignInResult.Ok) await FinishAsync(reset: false, painted);
        else if (r == SignInResult.Offline && (IsOffline || await StartOfflineAsync(g))) return r;
        // Deezer ended the session: the music kept offline plays until the account signs in again.
        else if (r == SignInResult.Expired)
        {
            Expired = true;
            if (await StartOfflineAsync(g)) return r;
            Expired = false;
            _library.Clear();
        }
        else _library.Clear();
        return r;
    }

    // No network at sign-in, but music kept offline: a session of that alone. Retry signs in.
    private async Task<bool> StartOfflineAsync(int g)
    {
        await _offline.LoadAsync();
        if (g != _gen || _offline.Collections.Count == 0) return false;
        _library.Clear();
        await ApplyPlaybackSettingsAsync();
        await _player.StartAsync();
        if (g != _gen) return false;
        IsOffline = true;
        SignedIn = true;
        SignedInChanged?.Invoke();
        return true;
    }

    // The engine keeps quality, gapless, crossfade and normalization itself; the output device
    // and the volume are the app's, and go to it at every start.
    private async Task ApplyPlaybackSettingsAsync()
    {
        var s = _settings;
        Engine.Post(() =>
        {
            if (s.AudioDevice.Length > 0) DeezerCore.DZSetAudioDevice(s.AudioDevice);
            DeezerCore.DZSetVolume(s.Volume);
        });
        await PlaybackOptions.LoadAsync();
    }

    // Retries by itself once Windows reports Internet access again: one watch, kept while a sign-in
    // failed for want of a network or the session is offline.
    private NetworkStatusChangedEventHandler? _online;

    public void WatchNetwork(Action retry)
    {
        StopNetworkWatch();
        var dq = DispatcherQueue.GetForCurrentThread();
        _online = _ => dq.TryEnqueue(() =>
        {
            if (_online != null && NetworkInformation.GetInternetConnectionProfile()?.GetNetworkConnectivityLevel() == NetworkConnectivityLevel.InternetAccess) retry();
        });
        NetworkInformation.NetworkStatusChanged += _online;
    }

    public void StopNetworkWatch()
    {
        if (_online == null) return;
        NetworkInformation.NetworkStatusChanged -= _online;
        _online = null;
    }

    // Repeats the last sign-in attempt. An expired session found while offline signs out; any other
    // failure keeps it.
    public async Task<SignInResult> RetryAsync()
    {
        var r = await (_lastAttempt?.Invoke() ?? Task.FromResult(SignInResult.Failed));
        if (r != SignInResult.Expired || !IsOffline) return r;
        _gen++;
        _player.Reset();
        _player.StopPolling();
        _library.Clear();
        _offline.Reset();
        StopNetworkWatch();
        IsOffline = false;
        Expired = false;
        SignedIn = false;
        SignedInChanged?.Invoke();
        return r;
    }

    public static bool LooksLikeArl(string s) => s.Length >= 64 && s.All(char.IsLetterOrDigit);

    // While signed in this is an account switch, and a failure keeps the current session.
    public async Task<SignInResult> SignInAsync(string arl)
    {
        if (Busy)
        {
            Toast.Info(Loc.S("Account_Title"), Loc.S("Login_Busy"));
            return SignInResult.Failed;
        }
        _lastAttempt = () => SignInAsync(arl);
        bool switching = SignedIn;
        int g = ++_gen;
        Busy = true;
        if (switching) Toast.Info(Loc.S("Account_Title"), Loc.S("Status_LoggingIn"), sticky: true);
        var r = await TryLogin(arl);
        Busy = false;
        if (g != _gen) return r;
        if (switching) Toast.Hide();
        if (r == SignInResult.Ok)
        {
            _ = Task.Run(() => Config.SaveArl(arl));
            _settings.IgnoredEnvArlHash = Config.EnvArlHash();
            SaveSettingsSoon();
            // The same account signing in again (its session had ended) keeps what plays.
            Account? next = switching ? await Task.Run(DeezerCore.Account) : null;
            await FinishAsync(reset: next != null && next.UserId != Account.UserId, known: next);
        }
        else if (switching) Toast.Error(Loc.S("Login_ErrorTitle"), FailureText(r));
        return r;
    }

    public static string FailureText(SignInResult r) => r switch
    {
        SignInResult.Expired => Loc.S("Login_Expired"),
        SignInResult.Offline => Loc.S("NoInternet_Body"),
        SignInResult.EngineMissing => Loc.S("Error_EngineMissing"),
        SignInResult.NoAudio => Loc.S("Error_NoAudio"),
        _ => Loc.S("Login_ErrorBody"),
    };

    private static async Task<SignInResult> TryLogin(string arl)
    {
        try
        {
            return await Task.Run(() =>
            {
                // Connect names this instance from the client info set before DZInit.
                DeezerCore.DZSetClientInfo("windows", Config.DeviceName());
                // The GUI owns play order; the engine only records the shuffle flag for remotes.
                DeezerCore.DZSetShuffleHostOwned(1);
                if (DeezerCore.DZInit(arl) != 0) return SignInResult.Ok;
                return DeezerCore.DZLoginErrorKind() switch
                {
                    1 => SignInResult.Expired,
                    2 => SignInResult.Offline,
                    4 => SignInResult.NoAudio,
                    _ => SignInResult.Failed,
                };
            });
        }
        catch (Exception ex) when (ex is DllNotFoundException or EntryPointNotFoundException or BadImageFormatException)
        {
            AppLog.Write("engine load failed: " + ex);
            return SignInResult.EngineMissing;
        }
    }

    // `painted`: the account whose cached Home is already shown, "" for none; `known`: the account
    // when it was read already.
    private async Task FinishAsync(bool reset, string painted = "", Account? known = null)
    {
        int g = _gen;
        if (reset || IsOffline)
        {
            _player.Reset();
            _library.Clear();
            _offline.Reset();
            Episodes.Reset();
        }
        var s = _settings;
        StopNetworkWatch();
        IsOffline = false;
        Expired = false;
        await ApplyPlaybackSettingsAsync();
        _ = _offline.LoadAsync();
        var account = known != null ? Task.FromResult(known) : Task.Run(DeezerCore.Account);
        var liked = _library.RefreshLikedIdsAsync();
        var home = _library.GetAsync<HomeReply>(Library.HomeKey);
        // The menus offer Pin or Unpin from what is pinned.
        _ = _library.GetAsync<PinnedReply>(Library.PinnedKey);
        await _player.StartAsync();
        if (g != _gen)
        {
            _player.StopPolling();
            return;
        }
        Account = new Account();
        SignedIn = true;
        SignedInChanged?.Invoke();

        var acct = await account;
        if (g != _gen) return;
        SetAccount(acct);
        _player.Restore(acct.UserId);
        if (!AccountKnown) Toast.Show(InfoBarSeverity.Warning, Loc.S("Account_Title"), Loc.S("Account_UnknownTier"));
        if (acct.UserId.Length > 0 && acct.UserId != s.LastUserId)
        {
            s.LastUserId = acct.UserId;
            SaveSettingsSoon();
        }
        if (painted.Length > 0 && acct.UserId.Length > 0 && painted != acct.UserId)
        {
            // The cached Home and playlists painted at startup were another account's.
            _library.Unseed(Library.HomeKey);
            _library.Unseed(Library.PlaylistsKey);
        }
        if (AccountKnown && painted != acct.UserId && !home.IsCompleted) await PaintFromCacheAsync(g, acct.UserId);
        await Task.WhenAll(liked, home);
        if (g == _gen) _player.RefreshLikes();
    }

    // The engine has no logout export: sign-out forgets the login, wipes the web profile and restarts.
    public void SignOut()
    {
        _gen++;
        string cache = CacheDir();
        _player.Reset();
        _library.Clear();
        _offline.Reset();
        Episodes.Reset();
        SignedIn = false;
        StopNetworkWatch();
        IsOffline = false;
        Expired = false;
        _player.StopPolling();
        Config.DeleteArl();
        if (cache.Length > 0)
        {
            try { if (Directory.Exists(cache)) Directory.Delete(cache, recursive: true); }
            catch (Exception ex) { AppLog.Write("cache delete: " + ex.Message); }
        }
        Account = new Account();
        _settings.IgnoredEnvArlHash = Config.EnvArlHash();
        _settings.WipeWebProfile = true;
        if (App.Restart()) return;
        SignedInChanged?.Invoke();
    }

    // The account's explicit-content setting as last read or set this session; null until read.
    internal ExplicitContent? Explicit { get; set; }

    // The account, and everything that shows it told; its kept settings go with the old one.
    internal void SetAccount(Account account)
    {
        Account = account;
        Explicit = null;
        AccountChanged?.Invoke();
    }

    public void SaveSettingsSoon()
    {
        _saveTimer.Stop();
        _saveTimer.Start();
    }

    public void SaveSettingsNow()
    {
        _saveTimer.Stop();
        Config.SaveSettings(_settings);
    }

    public async void CheckForUpdate()
    {
        var info = await CheckForUpdateAsync();
        if (!info.HasUpdate) return;
        Update = info;
        UpdateFound?.Invoke();
    }

    public static async Task<UpdateInfo> CheckForUpdateAsync()
    {
        try { return await Task.Run(DeezerCore.CheckUpdate); }
        catch (Exception ex) when (ex is DllNotFoundException or EntryPointNotFoundException) { return new UpdateInfo(); }
    }

    private static string LocalDir()
    {
        string local = Environment.GetEnvironmentVariable("LOCALAPPDATA") is { Length: > 0 } l
            ? l : Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData);
        return Path.Combine(local, "purpledisc");
    }

    // An offline session has no account loaded: its cache is the last signed-in account's.
    private string CacheDir() => CacheDir(IsOffline ? _settings.LastUserId : Account.UserId);

    private static string CacheDir(string userId) => userId.Length == 0 ? "" : Path.Combine(LocalDir(), "cache", userId);

    // The account's last Home and playlists, read off the UI thread and painted before the network answers.
    private async Task PaintFromCacheAsync(int g, string userId)
    {
        string dir = CacheDir(userId);
        var (home, playlists) = await Task.Run(() => (
            Read(Path.Combine(dir, Library.HomeKey + ".json"), j => JsonSerializer.Deserialize(j, WireJson.Default.HomeReply)),
            Read(Path.Combine(dir, Library.PlaylistsKey + ".json"), j => JsonSerializer.Deserialize(j, WireJson.Default.PlaylistsReply))));
        if (g != _gen) return;
        Seed(Library.HomeKey, home);
        Seed(Library.PlaylistsKey, playlists);
    }

    private static Reply? Read(string path, Func<string, Reply?> parse)
    {
        try
        {
            return File.Exists(path) ? parse(File.ReadAllText(path)) : null;
        }
        catch (Exception ex) when (ex is IOException or JsonException or UnauthorizedAccessException)
        {
            AppLog.Write("cache read: " + ex.Message);
            return null;
        }
    }

    // The network's answer, when it came first, wins.
    private void Seed(string key, Reply? cached)
    {
        if (cached != null && _library.Peek<Reply>(key) == null) _library.Seed(key, cached);
    }

    private void OnStored(string key, Reply reply)
    {
        string dir = CacheDir();
        if (dir.Length == 0 || !(reply is HomeReply && key == Library.HomeKey || reply is PlaylistsReply && key == Library.PlaylistsKey)) return;
        Task.Run(() =>
        {
            try
            {
                string json = reply is HomeReply h
                    ? JsonSerializer.Serialize(h, WireJson.Default.HomeReply)
                    : JsonSerializer.Serialize((PlaylistsReply)reply, WireJson.Default.PlaylistsReply);
                Directory.CreateDirectory(dir);
                File.WriteAllText(Path.Combine(dir, key + ".json"), json);
            }
            catch (Exception ex) { AppLog.Write("cache write: " + ex.Message); }
        });
    }
}
