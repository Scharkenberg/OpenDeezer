// The app's motion: Composition animations only, so none runs on the UI thread or lays anything
// out. Each method returns at once when the system's animation effects are off (read at start).
// Nothing is created for motion before a press, a like or a load asks for it.

using System;
using System.Collections.Generic;
using System.Numerics;
using Microsoft.UI.Composition;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Hosting;
using Microsoft.UI.Xaml.Input;

namespace PurpleDisc;

internal static class Motion
{
    private static readonly bool s_on = new Windows.UI.ViewManagement.UISettings().AnimationsEnabled;
    private static readonly HashSet<ButtonBase> s_pressed = new();

    // The content's visual, turning and scaling about its center.
    private static Visual Centered(UIElement e)
    {
        var v = ElementCompositionPreview.GetElementVisual(e);
        v.CenterPoint = new Vector3(e.ActualSize / 2, 0);
        return v;
    }

    private static TimeSpan Ms(int ms) => TimeSpan.FromMilliseconds(ms);

    private static CompositionEasingFunction Overshoot(Compositor c) => c.CreateCubicBezierEasingFunction(Moves.OvershootIn, Moves.OvershootOut);

    // A press by pointer dips the button's content and springs it back on release, cancel or a
    // lost capture. Keys and UI Automation invoke without a pointer and do not animate.
    public static void Press(ButtonBase b)
    {
        if (!s_on) return;
        b.AddHandler(UIElement.PointerPressedEvent, new PointerEventHandler((_, _) =>
        {
            s_pressed.Add(b);
            Spring(b, Moves.PressScale);
        }), true);
        var back = new PointerEventHandler((_, _) =>
        {
            if (s_pressed.Remove(b)) Spring(b, 1);
        });
        b.AddHandler(UIElement.PointerReleasedEvent, back, true);
        b.AddHandler(UIElement.PointerCanceledEvent, back, true);
        b.AddHandler(UIElement.PointerCaptureLostEvent, back, true);
    }

    private static void Spring(ButtonBase b, float to)
    {
        if (b.Content is not UIElement content) return;
        var v = Centered(content);
        var spring = v.Compositor.CreateSpringVector3Animation();
        spring.FinalValue = new Vector3(to, to, 1);
        spring.DampingRatio = Moves.PressDamping;
        v.StartAnimation("Scale", spring);
    }

    // A glyph that just changed (play and pause) grows in and fades in, about the center of a
    // `side`-wide icon.
    public static void Swap(UIElement glyph, float side)
    {
        if (!s_on) return;
        var v = ElementCompositionPreview.GetElementVisual(glyph);
        v.CenterPoint = new Vector3(side / 2, side / 2, 0);
        var c = v.Compositor;
        var grow = c.CreateVector3KeyFrameAnimation();
        grow.InsertKeyFrame(0f, new Vector3(Moves.SwapFrom, Moves.SwapFrom, 1));
        grow.InsertKeyFrame(1f, Vector3.One);
        grow.Duration = Ms(Timing.GlyphSwapMs);
        var fade = c.CreateScalarKeyFrameAnimation();
        fade.InsertKeyFrame(0f, 0f);
        fade.InsertKeyFrame(1f, 1f);
        fade.Duration = Ms(Timing.GlyphSwapMs);
        v.StartAnimation("Scale", grow);
        v.StartAnimation("Opacity", fade);
    }

    // A like: the heart pops, and one accent ring spreads from the button and fades; the ring is
    // made for this and removed when it ends.
    public static void Like(Button like)
    {
        if (!s_on || like.Content is not UIElement heart) return;
        // The pop runs to its end: the release that follows a click does not spring over it.
        s_pressed.Remove(like);
        var v = Centered(heart);
        var c = v.Compositor;
        var pop = c.CreateVector3KeyFrameAnimation();
        pop.InsertKeyFrame(0f, new Vector3(Moves.LikeFrom, Moves.LikeFrom, 1));
        pop.InsertKeyFrame(0.5f, new Vector3(Moves.LikePeak, Moves.LikePeak, 1));
        pop.InsertKeyFrame(1f, Vector3.One);
        pop.Duration = Ms(Timing.LikeMs);

        var size = like.ActualSize;
        var circle = c.CreateEllipseGeometry();
        circle.Center = size / 2;
        circle.Radius = new Vector2(Math.Min(size.X, size.Y) / 2 - Moves.RingWidth);
        var stroke = c.CreateSpriteShape(circle);
        stroke.StrokeBrush = c.CreateColorBrush((Windows.UI.Color)Application.Current.Resources["SystemAccentColor"]);
        stroke.StrokeThickness = Moves.RingWidth;
        var ring = c.CreateShapeVisual();
        ring.Size = size;
        ring.CenterPoint = new Vector3(size / 2, 0);
        ring.Shapes.Add(stroke);
        var spread = c.CreateVector3KeyFrameAnimation();
        spread.InsertKeyFrame(0f, new Vector3(Moves.RingFrom, Moves.RingFrom, 1));
        spread.InsertKeyFrame(1f, new Vector3(Moves.RingTo, Moves.RingTo, 1));
        spread.Duration = Ms(Timing.LikeMs);
        var fade = c.CreateScalarKeyFrameAnimation();
        fade.InsertKeyFrame(0f, 1f);
        fade.InsertKeyFrame(1f, 0f);
        fade.Duration = Ms(Timing.LikeMs);

        ElementCompositionPreview.SetElementChildVisual(like, ring);
        var batch = c.CreateScopedBatch(CompositionBatchTypes.Animation);
        v.StartAnimation("Scale", pop);
        ring.StartAnimation("Scale", spread);
        ring.StartAnimation("Opacity", fade);
        batch.End();
        batch.Completed += (_, _) =>
        {
            if (ReferenceEquals(ElementCompositionPreview.GetElementChildVisual(like), ring)) ElementCompositionPreview.SetElementChildVisual(like, null);
            ring.Dispose();
        };
    }

    // A mode or the settings gear: the glyph turns once around and rests upright.
    public static void Turn(UIElement glyph)
    {
        if (!s_on) return;
        var v = Centered(glyph);
        var turn = v.Compositor.CreateScalarKeyFrameAnimation();
        turn.InsertKeyFrame(0f, 0f);
        turn.InsertKeyFrame(1f, Moves.FullTurn);
        turn.Duration = Ms(Timing.TurnMs);
        v.StartAnimation("RotationAngleInDegrees", turn);
    }

    // Next and previous move `dx` toward their direction and back (a translation, not layout).
    public static void Nudge(UIElement glyph, float dx)
    {
        if (!s_on) return;
        ElementCompositionPreview.SetIsTranslationEnabled(glyph, true);
        var v = ElementCompositionPreview.GetElementVisual(glyph);
        var move = v.Compositor.CreateScalarKeyFrameAnimation();
        move.InsertKeyFrame(0f, 0f);
        move.InsertKeyFrame(0.5f, dx);
        move.InsertKeyFrame(1f, 0f);
        move.Duration = Ms(Timing.NudgeMs);
        v.StartAnimation("Translation.X", move);
    }

    // The mark draws itself in: the outer arc, the inner arc after a delay, the disc and the
    // triangle with an overshoot, the dot last. Each part rests drawn when this does not run.
    public static void DrawIn(CompositionGeometry outer, CompositionGeometry inner, CompositionShape disc, CompositionShape dot, UIElement triangle)
    {
        if (!s_on) return;
        var c = outer.Compositor;
        var arcs = c.CreateCubicBezierEasingFunction(Moves.FastOutSlowInIn, Moves.FastOutSlowInOut);
        outer.StartAnimation("TrimEnd", From0(c, outer.TrimEnd, Timing.MarkOuterMs, 0, arcs));
        inner.StartAnimation("TrimEnd", From0(c, inner.TrimEnd, Timing.MarkInnerMs, Timing.MarkInnerDelayMs, arcs));
        var grow = c.CreateVector2KeyFrameAnimation();
        grow.InsertKeyFrame(0f, Vector2.Zero);
        grow.InsertKeyFrame(1f, Vector2.One, Overshoot(c));
        grow.Duration = Ms(Timing.MarkDiscMs);
        disc.StartAnimation("Scale", grow);
        var dotGrow = c.CreateVector2KeyFrameAnimation();
        dotGrow.InsertKeyFrame(0f, Vector2.Zero);
        dotGrow.InsertKeyFrame(1f, Vector2.One);
        dotGrow.Duration = Ms(Timing.MarkDotMs);
        dotGrow.DelayTime = Ms(Timing.MarkDotDelayMs);
        dotGrow.DelayBehavior = AnimationDelayBehavior.SetInitialValueBeforeDelay;
        dot.StartAnimation("Scale", dotGrow);
        var t = Centered(triangle);
        var tGrow = c.CreateVector3KeyFrameAnimation();
        tGrow.InsertKeyFrame(0f, new Vector3(0, 0, 1));
        tGrow.InsertKeyFrame(1f, Vector3.One, Overshoot(c));
        tGrow.Duration = Ms(Timing.MarkDiscMs);
        t.StartAnimation("Scale", tGrow);
    }

    private static ScalarKeyFrameAnimation From0(Compositor c, float to, int ms, int delayMs, CompositionEasingFunction easing)
    {
        var a = c.CreateScalarKeyFrameAnimation();
        a.InsertKeyFrame(0f, 0f);
        a.InsertKeyFrame(1f, to, easing);
        a.Duration = Ms(ms);
        if (delayMs > 0)
        {
            a.DelayTime = Ms(delayMs);
            a.DelayBehavior = AnimationDelayBehavior.SetInitialValueBeforeDelay;
        }
        return a;
    }

    // The mark's dot goes once around it.
    public static void Spin(CompositionShape dot)
    {
        if (!s_on) return;
        var spin = dot.Compositor.CreateScalarKeyFrameAnimation();
        spin.InsertKeyFrame(0f, 0f);
        spin.InsertKeyFrame(1f, 360f);
        spin.Duration = Ms(Timing.MarkSpinMs);
        dot.StartAnimation("RotationAngleInDegrees", spin);
    }
}
