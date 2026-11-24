// The window's saved position and size: restored at start, captured as it changes.

using System;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Windows.Graphics;

namespace OpenDeezer;

internal sealed class WindowBounds(Window window, IntPtr hwnd, Settings settings)
{
    private AppWindow AppWindow => window.AppWindow;

    // The saved bounds when they are on a monitor that still exists, else centered.
    public void Place()
    {
        window.MinWidth = Size.WindowMinWidth;
        window.MinHeight = Size.WindowMinHeight;
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
