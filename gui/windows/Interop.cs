// Win32 calls the Windows App SDK projection lacks: the tray icon and its hidden window,
// DPI, and the single-instance wait.

using System;
using System.Runtime.InteropServices;

// Every native call is source-generated ([LibraryImport]) or blittable: no runtime marshalling.
[assembly: System.Runtime.CompilerServices.DisableRuntimeMarshalling]

namespace PurpleDisc;

internal static partial class NativeMethods
{
    internal const uint WM_TRAYCALLBACK = 0x8000 + 1; // WM_APP + 1: the tray icon's callback message
    internal const uint WM_COMMAND = 0x0111;
    internal const int WM_LBUTTONUP = 0x0202;
    internal const int WM_LBUTTONDBLCLK = 0x0203;
    internal const int WM_RBUTTONUP = 0x0205;
    internal const int WM_CONTEXTMENU = 0x007B;

    internal const uint NIM_ADD = 0x00000000;
    internal const uint NIM_MODIFY = 0x00000001;
    internal const uint NIM_DELETE = 0x00000002;
    internal const uint NIF_INFO = 0x00000010;
    internal const uint NIIF_INFO = 0x00000001;
    internal const uint NIF_MESSAGE = 0x00000001;
    internal const uint NIF_ICON = 0x00000002;
    internal const uint NIF_TIP = 0x00000004;

    internal const uint MF_STRING = 0x00000000;
    internal const uint MF_SEPARATOR = 0x00000800;
    internal const uint MF_GRAYED = 0x00000001;
    internal const uint TPM_RIGHTBUTTON = 0x0002;

    internal const int MENU_RESTORE = 1001;
    internal const int MENU_QUIT = 1002;
    internal const int MENU_PLAYPAUSE = 1003;
    internal const int MENU_NEXT = 1004;
    internal const int MENU_PREV = 1005;

    internal const uint IMAGE_ICON = 1;
    internal const uint LR_LOADFROMFILE = 0x00000010;
    internal const int SM_CXSMICON = 49;
    internal const int SM_CYSMICON = 50;
    internal const int SM_CXVIRTUALSCREEN = 78;
    internal const int SM_CYVIRTUALSCREEN = 79;
    internal const uint TRAY_UID = 1;

    internal static readonly IntPtr IDI_APPLICATION = new(32512);

    // lpszMenuName and lpszClassName point at UTF-16 strings.
    [StructLayout(LayoutKind.Sequential)]
    internal struct WNDCLASSEXW
    {
        public uint cbSize;
        public uint style;
        public IntPtr lpfnWndProc;
        public int cbClsExtra;
        public int cbWndExtra;
        public IntPtr hInstance;
        public IntPtr hIcon;
        public IntPtr hCursor;
        public IntPtr hbrBackground;
        public IntPtr lpszMenuName;
        public IntPtr lpszClassName;
        public IntPtr hIconSm;
    }

    // The text fields are NUL-terminated UTF-16 in place (Tray.SetText).
    [StructLayout(LayoutKind.Sequential)]
    internal unsafe struct NOTIFYICONDATAW
    {
        public uint cbSize;
        public IntPtr hWnd;
        public uint uID;
        public uint uFlags;
        public uint uCallbackMessage;
        public IntPtr hIcon;
        public fixed char szTip[128];
        public uint dwState;
        public uint dwStateMask;
        public fixed char szInfo[256];
        public uint uVersionOrTimeout;
        public fixed char szInfoTitle[64];
        public uint dwInfoFlags;
        public Guid guidItem;
        public IntPtr hBalloonIcon;
    }

    [StructLayout(LayoutKind.Sequential)]
    internal struct POINT { public int X; public int Y; }

    [StructLayout(LayoutKind.Sequential)]
    internal struct RECT { public int Left, Top, Right, Bottom; }

    internal const uint WM_GETMINMAXINFO = 0x0024;

    [StructLayout(LayoutKind.Sequential)]
    internal struct MINMAXINFO { public POINT ptReserved, ptMaxSize, ptMaxPosition, ptMinTrackSize, ptMaxTrackSize; }

    [LibraryImport("comctl32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool SetWindowSubclass(IntPtr hWnd, IntPtr proc, nuint id, nuint refData);
    [LibraryImport("comctl32.dll")] internal static partial IntPtr DefSubclassProc(IntPtr hWnd, uint msg, IntPtr wParam, IntPtr lParam);

    internal const int DWMWA_EXTENDED_FRAME_BOUNDS = 9;
    [LibraryImport("dwmapi.dll")] internal static partial int DwmGetWindowAttribute(IntPtr hWnd, int attribute, out RECT value, int size);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool ClientToScreen(IntPtr hWnd, ref POINT pt);

    [LibraryImport("kernel32.dll", StringMarshalling = StringMarshalling.Utf16)] internal static partial IntPtr GetModuleHandleW(string? lpModuleName);

    [LibraryImport("user32.dll")] internal static partial ushort RegisterClassExW(ref WNDCLASSEXW wc);
    [LibraryImport("user32.dll", StringMarshalling = StringMarshalling.Utf16)]
    internal static partial IntPtr CreateWindowExW(uint exStyle, string className, string windowName, uint style,
        int x, int y, int w, int h, IntPtr parent, IntPtr menu, IntPtr hInstance, IntPtr param);
    [LibraryImport("user32.dll")] internal static partial IntPtr DefWindowProcW(IntPtr hWnd, uint msg, IntPtr wParam, IntPtr lParam);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool SetForegroundWindow(IntPtr hWnd);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool GetCursorPos(out POINT pt);
    [LibraryImport("user32.dll")] internal static partial IntPtr CreatePopupMenu();
    [LibraryImport("user32.dll", StringMarshalling = StringMarshalling.Utf16)] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool AppendMenuW(IntPtr hMenu, uint flags, IntPtr id, string? item);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool TrackPopupMenu(IntPtr hMenu, uint flags, int x, int y, int reserved, IntPtr hWnd, IntPtr rect);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool DestroyMenu(IntPtr hMenu);
    [LibraryImport("user32.dll")] internal static partial IntPtr LoadIconW(IntPtr hInstance, IntPtr lpIconName);

    [LibraryImport("user32.dll", StringMarshalling = StringMarshalling.Utf16)] internal static partial uint RegisterWindowMessageW(string name);
    [LibraryImport("user32.dll", StringMarshalling = StringMarshalling.Utf16)] internal static partial IntPtr LoadImageW(IntPtr hInst, string name, uint type, int cx, int cy, uint flags);
    [LibraryImport("user32.dll")] internal static partial int GetSystemMetrics(int index);
    [LibraryImport("user32.dll")] internal static partial uint GetDpiForWindow(IntPtr hWnd);
    [LibraryImport("user32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool AllowSetForegroundWindow(uint processId);
    [LibraryImport("kernel32.dll", StringMarshalling = StringMarshalling.Utf16)] internal static partial IntPtr CreateEventW(IntPtr attrs, [MarshalAs(UnmanagedType.Bool)] bool manualReset, [MarshalAs(UnmanagedType.Bool)] bool initialState, string? name);
    [LibraryImport("kernel32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool SetEvent(IntPtr h);
    [LibraryImport("ole32.dll")] internal static partial int CoWaitForMultipleObjects(uint flags, uint timeout, uint count, IntPtr[] handles, out uint index);

    [LibraryImport("shell32.dll")] [return: MarshalAs(UnmanagedType.Bool)] internal static partial bool Shell_NotifyIconW(uint msg, ref NOTIFYICONDATAW data);
    [LibraryImport("shell32.dll", StringMarshalling = StringMarshalling.Utf16)] internal static partial IntPtr ExtractIconW(IntPtr hInst, string exeFileName, uint iconIndex);
}
