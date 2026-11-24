// One-line labels that don't fit scroll to their end and back instead of only ending in "…".
// Composition animation inside a clip: no layout per frame.

using System;
using System.Collections.Generic;
using System.Numerics;
using Microsoft.UI.Composition;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Hosting;
using Microsoft.UI.Xaml.Media;
using Windows.Foundation;
using WSize = Windows.Foundation.Size;

namespace OpenDeezer;

// Paused while the window is inactive; only labels that are moving are registered.
internal static class Marquee
{
    private static readonly HashSet<MarqueeText> s_moving = new();
    private static readonly Windows.UI.ViewManagement.UISettings s_system = new();

    public static bool Paused { get; private set; }

    public static bool Allowed => !Paused && s_system.AnimationsEnabled;

    public static void SetPaused(bool paused)
    {
        if (Paused == paused) return;
        Paused = paused;
        if (!paused) return;
        foreach (var label in new List<MarqueeText>(s_moving)) label.Stop(resetLayout: true);
    }

    public static void Started(MarqueeText label) => s_moving.Add(label);

    public static void Stopped(MarqueeText label) => s_moving.Remove(label);
}

// Rests ellipsized; scrolls while hovered, on tap, and (AutoPlay) once when its text appears or changes.
internal sealed partial class MarqueeText : Panel
{
    private const float Speed = Timing.MarqueeSpeed;
    private const float ReturnSpeed = Timing.MarqueeReturnSpeed;
    private static readonly TimeSpan Ramp = TimeSpan.FromMilliseconds(Timing.MarqueeRampMs);
    private static readonly TimeSpan Hold = TimeSpan.FromMilliseconds(Timing.MarqueeHoldMs);
    private static readonly TimeSpan MaxReturn = TimeSpan.FromMilliseconds(Timing.MarqueeMaxReturnMs);
    private static readonly TimeSpan Fade = TimeSpan.FromMilliseconds(Timing.MarqueeFadeMs);

    private bool _scrolling, _hovered, _autoPlayed;
    private double _overflow;
    private CompositionScopedBatch? _batch;
    private Visual? _visual;
    private TextBlock? _text;
    private WSize _clipSize;

    public bool AutoPlay { get; init; } = true;
    public bool PlayOnTap { get; init; } = true;

    public MarqueeText()
    {
        Style = Ui.Styles.Get("MarqueePanel"); // hover anywhere on the label
        PointerEntered += (_, _) =>
        {
            _hovered = true;
            Start();
        };
        PointerExited += (_, _) => _hovered = false;
        Tapped += (_, _) =>
        {
            if (PlayOnTap) Start();
        };
        Unloaded += (_, _) => Stop(resetLayout: false);
    }

    // The one child, added before the first layout and never replaced. When first seen, its text
    // and trimming are followed.
    private TextBlock? Text
    {
        get
        {
            if (_text != null || Children.Count == 0 || Children[0] is not TextBlock text) return _text;
            _text = text;
            text.IsTextTrimmedChanged += OnTrimmedChanged;
            text.RegisterPropertyChangedCallback(TextBlock.TextProperty, (_, _) =>
            {
                Stop(resetLayout: true);
                _autoPlayed = false;
                QueueAutoPlay();
            });
            return text;
        }
    }

    protected override WSize MeasureOverride(WSize available)
    {
        if (Text is not { } text) return default;
        if (!_scrolling)
        {
            text.Measure(available);
            return text.DesiredSize;
        }
        text.Measure(new WSize(double.PositiveInfinity, available.Height));
        return new WSize(Math.Min(text.DesiredSize.Width, available.Width), text.DesiredSize.Height);
    }

    protected override WSize ArrangeOverride(WSize final)
    {
        if (Text is { } text)
        {
            double width = _scrolling ? Math.Max(final.Width, text.DesiredSize.Width) : final.Width;
            text.Arrange(new Rect(0, 0, width, final.Height));
            _overflow = _scrolling ? Math.Max(0, text.DesiredSize.Width - final.Width) : 0;
        }
        if (_clipSize != final)
        {
            _clipSize = final;
            // Only the sides clip: ascenders and descenders may draw outside the text box.
            var rect = new Rect(0, -Size.TextOverhang, final.Width, final.Height + 2 * Size.TextOverhang);
            if (Clip is RectangleGeometry clip) clip.Rect = rect;
            else Clip = new RectangleGeometry { Rect = rect };
        }
        return final;
    }

    private void OnTrimmedChanged(TextBlock sender, IsTextTrimmedChangedEventArgs args)
    {
        if (sender.IsTextTrimmed && !_scrolling) QueueAutoPlay();
    }

    private void QueueAutoPlay()
    {
        if (_autoPlayed || !AutoPlay) return;
        DispatcherQueue?.TryEnqueue(DispatcherQueuePriority.Low, () =>
        {
            if (_autoPlayed || Text is not { IsTextTrimmed: true }) return;
            _autoPlayed = true;
            Start();
        });
    }

    private void Start()
    {
        if (_scrolling || !Marquee.Allowed || Text is not { IsTextTrimmed: true }) return;
        _scrolling = true;
        Marquee.Started(this);
        InvalidateMeasure();
        // The unconstrained measure runs in the next layout pass; animate after it.
        DispatcherQueue?.TryEnqueue(DispatcherQueuePriority.Low, Animate);
    }

    private void Animate()
    {
        if (!_scrolling || Text is not { } text || _overflow < 1 || !Marquee.Allowed)
        {
            Stop(resetLayout: true);
            return;
        }
        try
        {
            ElementCompositionPreview.SetIsTranslationEnabled(text, true);
            _visual = ElementCompositionPreview.GetElementVisual(text);
            var compositor = _visual.Compositor;
            float distance = (float)Math.Ceiling(_overflow + Size.MarqueeOvershoot);
            var outward = Travel(distance, Speed);
            var homeAt = Hold + Length(outward) + Hold;
            var total = homeAt + ReturnLength(distance);
            float At(TimeSpan t) => (float)(t / total);

            var move = compositor.CreateScalarKeyFrameAnimation();
            move.InsertKeyFrame(0f, 0f);
            move.InsertKeyFrame(At(Hold), 0f);
            InsertLeg(move, At, Hold, 0, -distance, outward);
            move.InsertKeyFrame(At(homeAt), -distance);
            var fade = InsertReturn(move, At, homeAt, distance);
            move.Duration = total;

            var batch = _batch = compositor.CreateScopedBatch(CompositionBatchTypes.Animation);
            _visual.StartAnimation("Translation.X", move);
            if (fade != null)
            {
                fade.Duration = total;
                _visual.StartAnimation("Opacity", fade);
            }
            batch.End();
            batch.Completed += (_, _) =>
            {
                if (!ReferenceEquals(batch, _batch)) return;
                if (_hovered && Marquee.Allowed) Animate(); // another pass while the pointer stays
                else Stop(resetLayout: true);
            };
        }
        catch (Exception ex)
        {
            AppLog.Write("marquee: " + ex.Message);
            Stop(resetLayout: true);
        }
    }

    internal void Stop(bool resetLayout)
    {
        _batch = null;
        if (_visual is { } visual)
        {
            visual.StopAnimation("Translation.X");
            visual.StopAnimation("Opacity");
            visual.Properties.InsertVector3("Translation", Vector3.Zero);
            visual.Opacity = 1;
        }
        Marquee.Stopped(this);
        if (!_scrolling) return;
        _scrolling = false;
        if (resetLayout) InvalidateMeasure(); // back to the ellipsized rest state
    }

    // A speed-up, a steady stretch at `speed`, a slow-down; shorter distances never reach full speed.
    private static (TimeSpan Ramp, TimeSpan Steady, float RampDistance) Travel(float distance, float speed)
    {
        var ramp = Ramp;
        float rampDistance = (float)(speed * ramp.TotalSeconds / 2);
        if (distance < rampDistance * 2)
        {
            ramp = TimeSpan.FromSeconds(Math.Sqrt(distance / speed * Ramp.TotalSeconds));
            rampDistance = distance / 2;
        }
        return (ramp, TimeSpan.FromSeconds((distance - rampDistance * 2) / speed), rampDistance);
    }

    private static TimeSpan Length((TimeSpan Ramp, TimeSpan Steady, float RampDistance) leg) => leg.Ramp + leg.Steady + leg.Ramp;

    private static void InsertLeg(ScalarKeyFrameAnimation a, Func<TimeSpan, float> at, TimeSpan start, float from, float to,
        (TimeSpan Ramp, TimeSpan Steady, float RampDistance) leg)
    {
        var c = a.Compositor;
        int direction = Math.Sign(to - from);
        var speedUp = c.CreateCubicBezierEasingFunction(new Vector2(1 / 3f, 0), new Vector2(2 / 3f, 1 / 3f));
        var slowDown = c.CreateCubicBezierEasingFunction(new Vector2(1 / 3f, 2 / 3f), new Vector2(2 / 3f, 1));
        a.InsertKeyFrame(at(start + leg.Ramp), from + direction * leg.RampDistance, speedUp);
        if (leg.Steady > TimeSpan.Zero)
            a.InsertKeyFrame(at(start + leg.Ramp + leg.Steady), to - direction * leg.RampDistance, c.CreateLinearEasingFunction());
        a.InsertKeyFrame(at(start + Length(leg)), to, slowDown);
    }

    // Home at ReturnSpeed, or when that would take longer than MaxReturn: fade out, jump, fade in.
    private static ScalarKeyFrameAnimation? InsertReturn(ScalarKeyFrameAnimation a, Func<TimeSpan, float> at, TimeSpan start, float distance)
    {
        var back = Travel(distance, ReturnSpeed);
        if (Length(back) <= MaxReturn)
        {
            InsertLeg(a, at, start, -distance, 0, back);
            return null;
        }
        var c = a.Compositor;
        a.InsertKeyFrame(at(start + Fade), -distance);
        a.InsertKeyFrame(Math.Min(1f, at(start + Fade) + 0.0001f), 0f);
        var fade = c.CreateScalarKeyFrameAnimation();
        fade.InsertKeyFrame(at(start), 1f);
        fade.InsertKeyFrame(at(start + Fade), 0f, c.CreateCubicBezierEasingFunction(new Vector2(0.7f, 0), new Vector2(1, 0.5f)));
        fade.InsertKeyFrame(at(start + Fade + Fade), 1f, c.CreateCubicBezierEasingFunction(new Vector2(0.1f, 0.9f), new Vector2(0.2f, 1)));
        return fade;
    }

    private static TimeSpan ReturnLength(float distance) =>
        Length(Travel(distance, ReturnSpeed)) is var back && back <= MaxReturn ? back : Fade + Fade;
}

// The house rule for one-line labels: once a label is actually cut off it moves into a MarqueeText at
// the same place (grid cell, z-order, margin, alignment), scrolling on hover or tap, never on its own.
internal static class Labels
{
    private static readonly DependencyProperty s_scrolls =
        DependencyProperty.RegisterAttached("Scrolls", typeof(bool), typeof(Labels), new PropertyMetadata(false));

    public static TextBlock Scrolls(this TextBlock text)
    {
        if (text.ReadLocalValue(s_scrolls) is true) return text;
        text.SetValue(s_scrolls, true);
        text.TextTrimming = TextTrimming.CharacterEllipsis;
        text.TextWrapping = TextWrapping.NoWrap;
        text.IsTextTrimmedChanged += OnTrimmedChanged;
        return text;
    }

    private static void OnTrimmedChanged(TextBlock text, IsTextTrimmedChangedEventArgs e)
    {
        if (!text.IsTextTrimmed) return;
        text.IsTextTrimmedChanged -= OnTrimmedChanged;
        // Never re-parent from inside a layout pass.
        text.DispatcherQueue?.TryEnqueue(DispatcherQueuePriority.Low, () => Wrap(text));
    }

    private static void Wrap(TextBlock text)
    {
        if (text.Parent is MarqueeText || VisualTreeHelper.GetParent(text) is MarqueeText) return;
        var host = new MarqueeText
        {
            AutoPlay = false,
            Margin = text.Margin,
            HorizontalAlignment = text.HorizontalAlignment,
            VerticalAlignment = text.VerticalAlignment,
            MaxWidth = text.MaxWidth,
            MinWidth = text.MinWidth,
        };
        Grid.SetRow(host, Grid.GetRow(text));
        Grid.SetColumn(host, Grid.GetColumn(text));
        Grid.SetRowSpan(host, Grid.GetRowSpan(text));
        Grid.SetColumnSpan(host, Grid.GetColumnSpan(text));
        Canvas.SetZIndex(host, Canvas.GetZIndex(text));
        switch (text.Parent)
        {
            case Panel panel when panel.Children.IndexOf(text) is >= 0 and var index:
                panel.Children.RemoveAt(index);
                Settle(text);
                host.Children.Add(text);
                panel.Children.Insert(index, host);
                break;
            case Border border when ReferenceEquals(border.Child, text):
                border.Child = null;
                Settle(text);
                host.Children.Add(text);
                border.Child = host;
                break;
            case ContentControl control when ReferenceEquals(control.Content, text):
                control.Content = null;
                Settle(text);
                host.Children.Add(text);
                control.Content = host;
                break;
        }
    }

    private static void Settle(TextBlock text)
    {
        text.Margin = default;
        text.ClearValue(FrameworkElement.MaxWidthProperty);
        text.ClearValue(FrameworkElement.MinWidthProperty);
        text.HorizontalAlignment = HorizontalAlignment.Left;
    }
}
