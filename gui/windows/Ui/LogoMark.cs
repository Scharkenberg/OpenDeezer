// The app's mark (assets/icon.svg) at any size: the circle, arcs, dot and disc drawn by
// Composition, the play triangle a XAML Path over them. It draws itself in when it first loads;
// Spin turns the dot once around it.

using System.Numerics;
using Microsoft.UI.Composition;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Hosting;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using Windows.Foundation;

namespace PurpleDisc;

internal sealed partial class LogoMark : Grid
{
    private readonly CompositionContainerShape _dotTurn;

    public LogoMark(double size)
    {
        Width = Height = size;
        float k = (float)size / Mark.Box;
        // The shapes live in a child under the triangle: a child visual draws over its element's content.
        var host = new Border();
        var triangle = new Path { Data = Triangle(k), Fill = (Brush)Application.Current.Resources["MarkBrush"] };
        Children.Add(host);
        Children.Add(triangle);

        var c = CompositionTarget.GetCompositorForCurrentThread();
        var white = (Windows.UI.Color)Application.Current.Resources["MarkLineColor"];
        var faint = (Windows.UI.Color)Application.Current.Resources["MarkFaintColor"];
        var center = new Vector2(Mark.Box / 2);
        var root = c.CreateContainerShape();
        root.Scale = new Vector2(k);
        root.Shapes.Add(Filled(c, center, Mark.Circle, (Windows.UI.Color)Application.Current.Resources["MarkColor"]));
        var outer = Circle(c, center, Mark.OuterRadius);
        outer.TrimEnd = Mark.OuterSweep / 360;
        root.Shapes.Add(Arc(c, outer, center, Mark.OuterStart, Mark.OuterWidth, white));
        var inner = Circle(c, center, Mark.InnerRadius);
        inner.TrimEnd = Mark.InnerSweep / 360;
        root.Shapes.Add(Arc(c, inner, center, Mark.InnerStart, Mark.InnerWidth, faint));
        var dotAt = new Vector2(Mark.DotX, Mark.DotY);
        var dot = Filled(c, dotAt, Mark.DotRadius, faint);
        dot.CenterPoint = dotAt;
        _dotTurn = c.CreateContainerShape();
        _dotTurn.CenterPoint = center;
        _dotTurn.Shapes.Add(dot);
        root.Shapes.Add(_dotTurn);
        var disc = Filled(c, center, Mark.Disc, white);
        disc.CenterPoint = center;
        root.Shapes.Add(disc);

        var shapes = c.CreateShapeVisual();
        shapes.Size = new Vector2((float)size);
        shapes.Shapes.Add(root);
        ElementCompositionPreview.SetElementChildVisual(host, shapes);

        RoutedEventHandler? first = null;
        first = (_, _) =>
        {
            Loaded -= first;
            Motion.DrawIn(outer, inner, disc, dot, triangle);
        };
        Loaded += first;
    }

    public void Spin() => Motion.Spin(_dotTurn);

    private static CompositionEllipseGeometry Circle(Compositor c, Vector2 center, float radius)
    {
        var g = c.CreateEllipseGeometry();
        g.Center = center;
        g.Radius = new Vector2(radius);
        return g;
    }

    private static CompositionSpriteShape Filled(Compositor c, Vector2 center, float radius, Windows.UI.Color color)
    {
        var s = c.CreateSpriteShape(Circle(c, center, radius));
        s.FillBrush = c.CreateColorBrush(color);
        return s;
    }

    // A circle's outline trimmed to an arc, turned so it starts at `start` degrees (clockwise from 3 o'clock).
    private static CompositionSpriteShape Arc(Compositor c, CompositionEllipseGeometry circle, Vector2 center, float start, float width, Windows.UI.Color color)
    {
        var s = c.CreateSpriteShape(circle);
        s.StrokeBrush = c.CreateColorBrush(color);
        s.StrokeThickness = width;
        s.StrokeStartCap = s.StrokeEndCap = CompositionStrokeCap.Round;
        s.CenterPoint = center;
        s.RotationAngleInDegrees = start - Mark.EllipseStart;
        return s;
    }

    // The play triangle with rounded corners, in pixels (`k` per mark unit).
    private static PathGeometry Triangle(float k)
    {
        var t = Mark.Triangle;
        Point P(int i) => new(t[i] * k, t[i + 1] * k);
        var f = new PathFigure { StartPoint = P(0), IsClosed = true, IsFilled = true };
        f.Segments.Add(new LineSegment { Point = P(2) });
        f.Segments.Add(new QuadraticBezierSegment { Point1 = P(4), Point2 = P(6) });
        f.Segments.Add(new LineSegment { Point = P(8) });
        f.Segments.Add(new QuadraticBezierSegment { Point1 = P(10), Point2 = P(12) });
        f.Segments.Add(new LineSegment { Point = P(14) });
        f.Segments.Add(new QuadraticBezierSegment { Point1 = P(16), Point2 = P(18) });
        var g = new PathGeometry();
        g.Figures.Add(f);
        return g;
    }
}
