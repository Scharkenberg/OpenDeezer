// The title bar, in the caption buttons' height: Back, the pane toggle, the app's icon and
// name, the search box centred in the free space, and the trailing buttons right beside
// the caption buttons. Everything that is not a control drags the window.

using System;
using System.Collections.Generic;
using System.Linq;
using Microsoft.UI.Input;
using Microsoft.UI.Windowing;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Windows.Foundation;
using Windows.Graphics;

namespace OpenDeezer;

internal sealed class AppTitleBar
{
    private readonly Window _window;
    private readonly FrameworkElement[] _controls;
    private readonly ColumnDefinition _startInset = new() { Width = new GridLength(0) };
    private readonly ColumnDefinition _endInset = new() { Width = new GridLength(0) };
    private readonly ContentPresenter _search = new() { VerticalAlignment = VerticalAlignment.Center };
    private RectInt32[] _passthrough = [];

    public Grid Root { get; }
    public Button Back { get; }
    public Button PaneToggle { get; }

    public AppTitleBar(Window window, Action back, Action togglePane, params FrameworkElement[] trailing)
    {
        _window = window;
        Back = Ui.IconButton(Icons.Back, "Nav_Back", back);
        PaneToggle = Ui.IconButton(Icons.Menu, "Nav_Pane", togglePane);
        Back.Visibility = Visibility.Collapsed;
        var icon = Ui.Logo(Size.TitleBarIcon);
        icon.Margin = Pad.TitleBarIcon;
        var title = Ui.Text("OpenDeezer", TextKind.Caption);
        title.VerticalAlignment = VerticalAlignment.Center;
        var end = Ui.Row(Space.XS, trailing);
        end.Margin = Pad.TitleBarEnd;
        _search.Margin = Pad.TitleBarSearch;

        Root = new Grid { Height = Size.TitleBar };
        Root.ColumnDefinitions.Add(_startInset);
        foreach (var c in new UIElement[] { Back, PaneToggle, icon, title, _search, end })
        {
            Root.ColumnDefinitions.Add(new ColumnDefinition
            {
                Width = c == _search ? new GridLength(1, GridUnitType.Star) : GridLength.Auto,
            });
            Grid.SetColumn((FrameworkElement)c, Root.ColumnDefinitions.Count - 1);
            Root.Children.Add(c);
        }
        Root.ColumnDefinitions.Add(_endInset);
        Back.Margin = PaneToggle.Margin = Pad.TitleBarButton;
        // The name gives way first when the window narrows.
        Ui.OnWidthClass(Root, c => title.Visibility = Ui.Show(c != WidthClass.Narrow));

        _controls = [Back, PaneToggle, .. trailing];
        // Any shift of a control resizes the search column, the one star column.
        foreach (var e in new FrameworkElement[] { Root, Back, PaneToggle, icon, title, _search, end })
            e.SizeChanged += (_, _) => Arrange();
        Root.Loaded += (_, _) => Root.XamlRoot.Changed += (_, _) => Arrange();
        // The caption buttons start at the visible frame's top edge, the client area one border
        // lower: the bar moves up by that border so both centre on one line.
        AlignWithCaption();
        window.AppWindow.Changed += (_, e) =>
        {
            if (e.DidPresenterChange || e.DidSizeChange) AlignWithCaption();
        };
    }

    // The border between the visible frame and the client area, in DIPs (none when maximised:
    // the client area then starts above the screen).
    private void AlignWithCaption()
    {
        var hwnd = WinRT.Interop.WindowNative.GetWindowHandle(_window);
        var origin = new NativeMethods.POINT();
        if (NativeMethods.DwmGetWindowAttribute(hwnd, NativeMethods.DWMWA_EXTENDED_FRAME_BOUNDS, out var frame,
                System.Runtime.InteropServices.Marshal.SizeOf<NativeMethods.RECT>()) != 0
            || !NativeMethods.ClientToScreen(hwnd, ref origin)) return;
        double border = Math.Max(0, origin.Y - frame.Top) / (NativeMethods.GetDpiForWindow(hwnd) / 96.0);
        Root.Margin = new Thickness(0, -border, 0, 0);
        // The controls moved with the bar: their passthrough regions follow, from the new layout.
        Root.UpdateLayout();
        Arrange();
    }

    public UIElement? Search
    {
        set => _search.Content = value;
    }

    // Insets for the caption buttons and the rectangles that take input instead of dragging.
    private void Arrange()
    {
        // A closing window can still raise a size change once its AppWindow is gone.
        if (Root.XamlRoot is not { } xr || _window.AppWindow is not { } appWindow) return;
        double scale = xr.RasterizationScale;
        var bar = appWindow.TitleBar;
        // Caption buttons sit on the left in a right-to-left window, which mirrors the columns too.
        bool rtl = Root.FlowDirection == FlowDirection.RightToLeft;
        SetWidth(_startInset, (rtl ? bar.RightInset : bar.LeftInset) / scale);
        SetWidth(_endInset, (rtl ? bar.LeftInset : bar.RightInset) / scale);

        var rects = new List<RectInt32>(_controls.Length + 1);
        // The search box itself, not its column: the space around it drags.
        foreach (var c in _search.Content is FrameworkElement box ? _controls.Append(box) : _controls)
        {
            if (c.Visibility != Visibility.Visible || c.ActualWidth == 0) continue;
            var r = c.TransformToVisual(null).TransformBounds(new Rect(0, 0, c.ActualWidth, c.ActualHeight));
            rects.Add(new RectInt32((int)Math.Round(r.X * scale), (int)Math.Round(r.Y * scale),
                (int)Math.Round(r.Width * scale), (int)Math.Round(r.Height * scale)));
        }
        if (rects.SequenceEqual(_passthrough)) return;
        _passthrough = rects.ToArray();
        InputNonClientPointerSource.GetForWindowId(appWindow.Id).SetRegionRects(NonClientRegionKind.Passthrough, _passthrough);
    }

    private static void SetWidth(ColumnDefinition c, double width)
    {
        if (Math.Abs(c.Width.Value - width) > 0.5) c.Width = new GridLength(width);
    }
}
