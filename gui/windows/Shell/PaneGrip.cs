// A thin grip on the navigation pane's edge: drag to resize between Size.PaneMin
// and Size.PaneMax, never past Size.PaneMaxShare of the window; double-click restores the default.
// Only while the pane is docked open (Expanded); the width is remembered in settings.

using System;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed partial class PaneGrip : Grid
{
    private readonly NavigationView _nav;
    private readonly FrameworkElement _window;
    private readonly Border _line = new() { Style = Ui.Styles.Get("GripLine") };
    private readonly Action<double> _saved;
    private double _preferred, _startX, _startWidth;
    private bool _dragging;

    // `preferred` 0 = the default width.
    public PaneGrip(NavigationView nav, FrameworkElement window, double preferred, Action<double> saved)
    {
        _nav = nav;
        _window = window;
        _saved = saved;
        _preferred = preferred > 0 ? preferred : Size.Pane;
        Style = Ui.Styles.Get("PaneGrip");
        Width = Size.Grip;
        HorizontalAlignment = HorizontalAlignment.Left;
        ProtectedCursor = InputSystemCursor.Create(InputSystemCursorShape.SizeWestEast);
        Children.Add(_line);
        _line.Visibility = Visibility.Collapsed;
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetAccessibilityView(this, Microsoft.UI.Xaml.Automation.Peers.AccessibilityView.Raw);

        PointerEntered += (_, _) => _line.Visibility = Visibility.Visible;
        PointerExited += (_, _) => _line.Visibility = Ui.Show(_dragging);
        PointerPressed += (_, e) =>
        {
            if (!CapturePointer(e.Pointer)) return;
            _dragging = true;
            _startX = e.GetCurrentPoint(_window).Position.X;
            _startWidth = _nav.OpenPaneLength;
            e.Handled = true;
        };
        // The window's coordinates mirror under right-to-left, so the delta needs no flip.
        PointerMoved += (_, e) =>
        {
            if (!_dragging) return;
            _preferred = Clamp(_startWidth + e.GetCurrentPoint(_window).Position.X - _startX);
            Apply();
            e.Handled = true;
        };
        PointerReleased += (_, e) =>
        {
            EndDrag();
            ReleasePointerCaptures();
            e.Handled = true;
        };
        PointerCaptureLost += (_, _) => EndDrag();
        DoubleTapped += (_, e) =>
        {
            _preferred = Size.Pane;
            Apply();
            _saved(0);
            e.Handled = true;
        };
        nav.DisplayModeChanged += (_, _) => Update();
        nav.PaneOpened += (_, _) => Update();
        nav.PaneClosing += (_, _) => Visibility = Visibility.Collapsed;
        window.SizeChanged += (_, _) => Apply();
        Apply();
    }

    // Shown only where dragging means something: the pane docked open beside the content.
    public void Update()
    {
        Visibility = Ui.Show(_nav.IsPaneVisible && _nav.IsPaneOpen && _nav.DisplayMode == NavigationViewDisplayMode.Expanded);
        Apply();
    }

    private double Clamp(double w)
    {
        double max = _window.ActualWidth > 0 ? Math.Min(Size.PaneMax, _window.ActualWidth * Size.PaneMaxShare) : Size.PaneMax;
        return Math.Clamp(w, Size.PaneMin, Math.Max(Size.PaneMin, max));
    }

    // A smaller window narrows the pane for now; the preferred width comes back when it grows.
    private void Apply()
    {
        double w = Clamp(_preferred);
        if (_nav.OpenPaneLength != w) _nav.OpenPaneLength = w;
        Margin = new Thickness(w - Size.Grip / 2, 0, 0, 0);
    }

    private void EndDrag()
    {
        if (!_dragging) return;
        _dragging = false;
        _line.Visibility = Visibility.Collapsed;
        _saved(_preferred == Size.Pane ? 0 : Math.Round(_preferred));
    }
}
