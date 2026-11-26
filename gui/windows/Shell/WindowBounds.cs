// The window's saved position and size: restored at start, captured as it changes.

using System;
using System.Runtime.InteropServices;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Windows.Graphics;

namespace PurpleDisc;

internal sealed class WindowBounds(Window window, IntPtr hwnd, Settings settings)
{
    private AppWindow AppWindow => window.AppWindow;

    // The saved bounds when they are on a monitor that still exists, else centered.
    public void Place()
    {
        LimitSize(hwnd);
        var s = settings;
        if (s.WinW > 0 && s.WinH > 0)
        {
            var rect = new RectInt32(s.WinX, s.WinY, s.WinW, s.WinH);
            if (DisplayArea.GetFromRect(rect, DisplayAreaFallback.None) != null)
            {
                AppWindow.MoveAndResize(rect);
                return;
            }
        }
        double scale = Ui.DpiScale(hwnd);
        var area = DisplayArea.GetFromWindowId(AppWindow.Id, DisplayAreaFallback.Primary).WorkArea;
        int w = Math.Min((int)(Size.WindowWidth * scale), area.Width);
        int h = Math.Min((int)(Size.WindowHeight * scale), area.Height);
        AppWindow.MoveAndResize(new RectInt32(area.X + (area.Width - w) / 2, area.Y + (area.Height - h) / 2, w, h));
    }

    internal static unsafe void LimitSize(IntPtr window) =>
        NativeMethods.SetWindowSubclass(window, (IntPtr)(delegate* unmanaged<IntPtr, uint, IntPtr, IntPtr, nuint, nuint, IntPtr>)&MinSize, 1, 0);

    // The minimum size, answered to Windows itself in the window's current scale: a border drag
    // stops there whatever the Windows App SDK's presenter does.
    [UnmanagedCallersOnly]
    private static unsafe IntPtr MinSize(IntPtr hWnd, uint msg, IntPtr wParam, IntPtr lParam, nuint id, nuint refData)
    {
        IntPtr result = NativeMethods.DefSubclassProc(hWnd, msg, wParam, lParam);
        if (msg == NativeMethods.WM_GETMINMAXINFO)
        {
            double scale = Ui.DpiScale(hWnd);
            var info = (NativeMethods.MINMAXINFO*)lParam;
            info->ptMinTrackSize.X = (int)Math.Ceiling(Size.WindowMinWidth * scale);
            info->ptMinTrackSize.Y = (int)Math.Ceiling(Size.WindowMinHeight * scale);
        }
        return result;
    }

    // The restored bounds, not the maximized ones.
    public void Capture()
    {
        if (AppWindow.Presenter is not OverlappedPresenter op || !AppWindow.IsVisible || op.State == OverlappedPresenterState.Minimized) return;
        settings.WinMax = op.State == OverlappedPresenterState.Maximized;
        if (op.State != OverlappedPresenterState.Restored) return;
        settings.WinX = AppWindow.Position.X;
        settings.WinY = AppWindow.Position.Y;
        settings.WinW = AppWindow.Size.Width;
        settings.WinH = AppWindow.Size.Height;
    }
}
