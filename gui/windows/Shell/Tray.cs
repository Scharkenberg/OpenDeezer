// The notification-area icon. Its owner is a hidden top-level window, not a message-only one:
// only top-level windows receive "TaskbarCreated" after Explorer restarts.

using System;
using System.IO;
using System.Runtime.InteropServices;

namespace PurpleDisc;

internal sealed unsafe class Tray(Action restore, Action quit)
{
    private static Tray? s_instance;
    private static uint s_taskbarCreated;
    private NativeMethods.NOTIFYICONDATAW _nid;
    private string _tip = "";
    private IntPtr _hwnd;

    public bool Added { get; private set; }

    public static string IconPath() => Path.Combine(AppContext.BaseDirectory, "purpledisc.ico");

    public void Add()
    {
        try
        {
            s_instance = this;
            s_taskbarCreated = NativeMethods.RegisterWindowMessageW("TaskbarCreated");
            IntPtr hInstance = NativeMethods.GetModuleHandleW(null);
            fixed (char* className = "PurpleDiscTrayWnd")
            {
                var wc = new NativeMethods.WNDCLASSEXW
                {
                    cbSize = (uint)sizeof(NativeMethods.WNDCLASSEXW),
                    lpfnWndProc = (IntPtr)(delegate* unmanaged<IntPtr, uint, IntPtr, IntPtr, IntPtr>)&WndProc,
                    hInstance = hInstance,
                    lpszClassName = (IntPtr)className,
                };
                NativeMethods.RegisterClassExW(ref wc);
            }
            if (_hwnd == IntPtr.Zero)
                _hwnd = NativeMethods.CreateWindowExW(0, "PurpleDiscTrayWnd", "PurpleDiscTray", 0, 0, 0, 0, 0, IntPtr.Zero, IntPtr.Zero, hInstance, IntPtr.Zero);
            _nid = new NativeMethods.NOTIFYICONDATAW
            {
                cbSize = (uint)sizeof(NativeMethods.NOTIFYICONDATAW),
                hWnd = _hwnd,
                uID = NativeMethods.TRAY_UID,
                uFlags = NativeMethods.NIF_ICON | NativeMethods.NIF_MESSAGE | NativeMethods.NIF_TIP,
                uCallbackMessage = NativeMethods.WM_TRAYCALLBACK,
                hIcon = LoadIcon(),
            };
            _tip = "Purple Disc";
            fixed (char* tip = _nid.szTip) SetText(tip, 128, _tip);
            Added = NativeMethods.Shell_NotifyIconW(NativeMethods.NIM_ADD, ref _nid);
        }
        catch (Exception ex) { AppLog.Write("tray: " + ex.Message); }
    }

    public void Remove()
    {
        if (!Added) return;
        NativeMethods.Shell_NotifyIconW(NativeMethods.NIM_DELETE, ref _nid);
        Added = false;
    }

    public void SetTip(string text)
    {
        if (!Added) return;
        string tip = text.Length == 0 ? "Purple Disc" : "Purple Disc\n" + text;
        if (tip.Length > 127) tip = tip[..126] + "…";
        if (tip == _tip) return;
        _tip = tip;
        fixed (char* p = _nid.szTip) SetText(p, 128, tip);
        _nid.uFlags = NativeMethods.NIF_TIP;
        NativeMethods.Shell_NotifyIconW(NativeMethods.NIM_MODIFY, ref _nid);
    }

    public void Balloon(string title, string body)
    {
        if (!Added) return;
        _nid.uFlags = NativeMethods.NIF_INFO;
        fixed (char* t = _nid.szInfoTitle) SetText(t, 64, title);
        fixed (char* b = _nid.szInfo) SetText(b, 256, body);
        _nid.dwInfoFlags = NativeMethods.NIIF_INFO;
        NativeMethods.Shell_NotifyIconW(NativeMethods.NIM_MODIFY, ref _nid);
        fixed (char* t = _nid.szInfoTitle) SetText(t, 64, "");
        fixed (char* b = _nid.szInfo) SetText(b, 256, "");
    }

    // Writes text into a fixed field of `size` chars, cut to fit, NUL-terminated.
    private static void SetText(char* field, int size, string text)
    {
        var dst = new Span<char>(field, size);
        dst.Clear();
        text.AsSpan(0, Math.Min(text.Length, size - 1)).CopyTo(dst);
    }

    private static IntPtr LoadIcon()
    {
        int cx = NativeMethods.GetSystemMetrics(NativeMethods.SM_CXSMICON);
        int cy = NativeMethods.GetSystemMetrics(NativeMethods.SM_CYSMICON);
        if (File.Exists(IconPath()))
        {
            IntPtr h = NativeMethods.LoadImageW(IntPtr.Zero, IconPath(), NativeMethods.IMAGE_ICON, cx, cy, NativeMethods.LR_LOADFROMFILE);
            if (h != IntPtr.Zero) return h;
        }
        if (Environment.ProcessPath is { Length: > 0 } exe)
        {
            IntPtr h = NativeMethods.ExtractIconW(NativeMethods.GetModuleHandleW(null), exe, 0);
            if (h != IntPtr.Zero && h.ToInt64() != 1) return h; // 1: the file has no icons
        }
        return NativeMethods.LoadIconW(IntPtr.Zero, NativeMethods.IDI_APPLICATION);
    }

    private void ShowMenu()
    {
        var player = App.Player;
        IntPtr menu = NativeMethods.CreatePopupMenu();
        uint transport = App.Session.SignedIn ? NativeMethods.MF_STRING : NativeMethods.MF_STRING | NativeMethods.MF_GRAYED;
        NativeMethods.AppendMenuW(menu, NativeMethods.MF_STRING, (IntPtr)NativeMethods.MENU_RESTORE, Loc.S("Tray_Open"));
        NativeMethods.AppendMenuW(menu, NativeMethods.MF_SEPARATOR, IntPtr.Zero, null);
        NativeMethods.AppendMenuW(menu, transport, (IntPtr)NativeMethods.MENU_PLAYPAUSE, Loc.S(player.State == PlayState.Playing ? "Tooltip_Pause" : "Tooltip_Play"));
        NativeMethods.AppendMenuW(menu, transport, (IntPtr)NativeMethods.MENU_NEXT, Loc.S("Tooltip_Next"));
        NativeMethods.AppendMenuW(menu, transport, (IntPtr)NativeMethods.MENU_PREV, Loc.S("Tooltip_Previous"));
        NativeMethods.AppendMenuW(menu, NativeMethods.MF_SEPARATOR, IntPtr.Zero, null);
        NativeMethods.AppendMenuW(menu, NativeMethods.MF_STRING, (IntPtr)NativeMethods.MENU_QUIT, Loc.S("Tray_Quit"));
        ThemeMenu();
        NativeMethods.GetCursorPos(out var p);
        NativeMethods.SetForegroundWindow(_hwnd); // so the menu closes on focus loss
        NativeMethods.TrackPopupMenu(menu, NativeMethods.TPM_RIGHTBUTTON, p.X, p.Y, 0, _hwnd, IntPtr.Zero);
        NativeMethods.DestroyMenu(menu);
    }

    // A Win32 menu takes the process's preferred app mode (uxtheme, by ordinal only). It is set from
    // the app's theme setting and, for "system", Windows' current value, read at every open: a switch
    // made by hand, at a scheduled time or while the PC slept is right without any notice arriving.
    // High contrast keeps the system's own menu colors.
    private static void ThemeMenu()
    {
        bool dark = App.Settings.Theme switch
        {
            "dark" => true,
            "light" => false,
            _ => Microsoft.Win32.Registry.GetValue(@"HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize", "AppsUseLightTheme", 1) is 0,
        };
        var hc = new NativeMethods.HIGHCONTRASTW { cbSize = (uint)sizeof(NativeMethods.HIGHCONTRASTW) };
        bool highContrast = NativeMethods.SystemParametersInfoW(NativeMethods.SPI_GETHIGHCONTRAST, hc.cbSize, ref hc, 0) && (hc.dwFlags & NativeMethods.HCF_HIGHCONTRASTON) != 0;
        NativeMethods.SetPreferredAppMode(highContrast ? 0 : dark ? 2 : 3); // Default, ForceDark, ForceLight
        NativeMethods.FlushMenuThemes();
    }

    // Called from native code: an exception escaping here would end the process.
    [UnmanagedCallersOnly]
    private static IntPtr WndProc(IntPtr hWnd, uint msg, IntPtr wParam, IntPtr lParam)
    {
        try
        {
            if (s_instance is { } self && self.Handle(msg, wParam, lParam)) return IntPtr.Zero;
        }
        catch (Exception ex) { AppLog.Write("tray: " + ex); }
        return NativeMethods.DefWindowProcW(hWnd, msg, wParam, lParam);
    }

    private bool Handle(uint msg, IntPtr wParam, IntPtr lParam)
    {
        if (msg == NativeMethods.WM_TRAYCALLBACK)
        {
            switch ((int)(lParam.ToInt64() & 0xFFFF))
            {
                case NativeMethods.WM_LBUTTONUP:
                case NativeMethods.WM_LBUTTONDBLCLK: restore(); break;
                case NativeMethods.WM_RBUTTONUP:
                case NativeMethods.WM_CONTEXTMENU: ShowMenu(); break;
            }
            return true;
        }
        if (msg == NativeMethods.WM_COMMAND)
        {
            switch ((int)(wParam.ToInt64() & 0xFFFF))
            {
                case NativeMethods.MENU_RESTORE: restore(); break;
                case NativeMethods.MENU_QUIT: quit(); break;
                case NativeMethods.MENU_PLAYPAUSE: App.Player.TogglePlayPause(); break;
                case NativeMethods.MENU_NEXT: App.Player.Next(); break;
                case NativeMethods.MENU_PREV: App.Player.Prev(); break;
            }
            return true;
        }
        // Explorer restarted: the notification area forgot every icon.
        if (msg == s_taskbarCreated && s_taskbarCreated != 0)
        {
            _nid.uFlags = NativeMethods.NIF_ICON | NativeMethods.NIF_MESSAGE | NativeMethods.NIF_TIP;
            Added = NativeMethods.Shell_NotifyIconW(NativeMethods.NIM_ADD, ref _nid);
            return true;
        }
        return false;
    }
}
