// One notice at a time, floating bottom-centre over the views. It closes by itself after a few
// seconds unless it is sticky or carries an action. UI thread only.

using System;
using System.Numerics;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;

namespace OpenDeezer;

internal static class Toast
{
    private static readonly Vector3 s_below = new(0, Size.ToastRise, 0);
    private static readonly InfoBar s_bar = new() { IsOpen = false, IsClosable = true, Style = Ui.Styles.Get("ToastInfoBar") };
    private static readonly DispatcherQueueTimer s_timer = Timer(Timing.ToastMs, Hide);
    private static readonly DispatcherQueueTimer s_fadeOut = Timer(Timing.ToastOutMs, FadedOut);

    // The overlay the shell places over its view host.
    public static Border Root { get; } = new()
    {
        Style = Ui.Styles.Get("ToastBorder"),
        Child = s_bar,
        MaxWidth = Size.Toast,
        Margin = Pad.Toast,
        HorizontalAlignment = HorizontalAlignment.Center,
        VerticalAlignment = VerticalAlignment.Bottom,
        Opacity = 0,
        Translation = s_below,
    };

    static Toast()
    {
        // The close button fades out too; IsOpen is already false while Closing runs.
        s_bar.Closing += (_, e) =>
        {
            if (e.Reason != InfoBarCloseReason.CloseButton) return;
            e.Cancel = true;
            Hide();
        };
    }

    public static void Show(InfoBarSeverity severity, string title, string text, ButtonBase? action = null, bool sticky = false)
    {
        s_bar.Severity = severity;
        s_bar.Title = title;
        s_bar.Message = text;
        s_bar.ActionButton = action;
        s_timer.Stop();
        s_fadeOut.Stop();
        if (Root.Opacity < 1)
        {
            s_bar.IsOpen = true;
            Animate(Timing.ToastInMs);
            Root.Opacity = 1;
            Root.Translation = Vector3.Zero;
        }
        if (!sticky && action == null) s_timer.Start();
    }

    public static void Error(string title, string text) => Show(InfoBarSeverity.Error, title, text);

    public static void Info(string title, string text, bool sticky = false) => Show(InfoBarSeverity.Informational, title, text, sticky: sticky);

    public static void Success(string title, string text) => Show(InfoBarSeverity.Success, title, text);

    public static void Hide()
    {
        if (Root.Opacity == 0) return;
        s_timer.Stop();
        Animate(Timing.ToastOutMs);
        Root.Opacity = 0;
        s_fadeOut.Start();
    }

    private static void FadedOut()
    {
        Root.OpacityTransition = null;
        Root.TranslationTransition = null;
        Root.Translation = s_below;
        s_bar.IsOpen = false;
    }

    private static void Animate(int ms)
    {
        Ui.Fades(Root, ms);
        Root.TranslationTransition = new Vector3Transition { Duration = TimeSpan.FromMilliseconds(ms) };
    }

    private static DispatcherQueueTimer Timer(int ms, Action tick)
    {
        var t = DispatcherQueue.GetForCurrentThread().CreateTimer();
        t.Interval = TimeSpan.FromMilliseconds(ms);
        t.IsRepeating = false;
        t.Tick += (_, _) => tick();
        return t;
    }
}
