// The notification-area icon. Its owner is a hidden top-level window, not a message-only one:
// only top-level windows receive "TaskbarCreated" after Explorer restarts.

using System;
using System.IO;
using System.Runtime.InteropServices;

namespace OpenDeezer;

internal sealed class Tray(Action restore, Action quit)
{
    private static Tray? s_instance;
    private static NativeMethods.WndProcDelegate? s_proc;
    private static uint s_taskbarCreated;
    private NativeMethods.NOTIFYICONDATAW _nid;
    private IntPtr _hwnd;

    public bool Added { get; private set; }

    public static string IconPath() => Path.Combine(AppContext.BaseDirectory, "opendeezer.ico");

    public void Add()
    {
        try
        {
            s_instance = this;
            s_proc ??= WndProc; // the delegate must outlive the window class
            s_taskbarCreated = NativeMethods.RegisterWindowMessageW("TaskbarCreated");
            var wc = new NativeMethods.WNDCLASSEXW
            {
                cbSize = (uint)Marshal.SizeOf<NativeMethods.WNDCLASSEXW>(),
                lpfnWndProc = Marshal.GetFunctionPointerForDelegate(s_proc),
                hInstance = NativeMethods.GetModuleHandleW(null),
                lpszClassName = "OpenDeezerTrayWnd",
            };
            NativeMethods.RegisterClassExW(ref wc);
            if (_hwnd == IntPtr.Zero)
                _hwnd = NativeMethods.CreateWindowExW(0, "OpenDeezerTrayWnd", "OpenDeezerTray", 0, 0, 0, 0, 0, IntPtr.Zero, IntPtr.Zero, wc.hInstance, IntPtr.Zero);
            _nid = new NativeMethods.NOTIFYICONDATAW
            {
                cbSize = (uint)Marshal.SizeOf<NativeMethods.NOTIFYICONDATAW>(),
                hWnd = _hwnd,
                uID = NativeMethods.TRAY_UID,
                uFlags = NativeMethods.NIF_ICON | NativeMethods.NIF_MESSAGE | NativeMethods.NIF_TIP,
                uCallbackMessage = NativeMethods.WM_TRAYCALLBACK,
                hIcon = LoadIcon(),
                szTip = "OpenDeezer",
                szInfo = "",
                szInfoTitle = "",
            };
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
        string tip = text.Length == 0 ? "OpenDeezer" : "OpenDeezer\n" + text;
        if (tip.Length > 127) tip = tip[..126] + "…";
        if (tip == _nid.szTip) return;
        _nid.szTip = tip;
        _nid.uFlags = NativeMethods.NIF_TIP;
        NativeMethods.Shell_NotifyIconW(NativeMethods.NIM_MODIFY, ref _nid);
    }

    public void Balloon(string title, string body)
    {
        if (!Added) return;
        _nid.uFlags = NativeMethods.NIF_INFO;
        _nid.szInfoTitle = title.Length > 63 ? title[..63] : title;
        _nid.szInfo = body.Length > 255 ? body[..255] : body;
        _nid.dwInfoFlags = NativeMethods.NIIF_INFO;
        NativeMethods.Shell_NotifyIconW(NativeMethods.NIM_MODIFY, ref _nid);
        _nid.szInfo = "";
        _nid.szInfoTitle = "";
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
        NativeMethods.GetCursorPos(out var p);
        NativeMethods.SetForegroundWindow(_hwnd); // so the menu closes on focus loss
        NativeMethods.TrackPopupMenu(menu, NativeMethods.TPM_RIGHTBUTTON, p.X, p.Y, 0, _hwnd, IntPtr.Zero);
        NativeMethods.DestroyMenu(menu);
    }

    // Called from native code: an exception escaping here would end the process.
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
