// The four states of a data view over one content element: loading (skeleton
// blocks shaped like the content), content, empty and error.

using System;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal enum SkeletonShape { Rows, Tiles, Page }

internal sealed class StateView
{
    private readonly FrameworkElement _content;
    private readonly SkeletonShape _shape;
    private readonly StackPanel _message;
    private readonly FontIcon _icon = Ui.Icon(Icons.Info, IconSize.Large);
    private readonly TextBlock _text = Ui.Text("", TextKind.Body, wrap: true);
    private readonly Button _action = new();
    private StackPanel? _skeleton;
    private Thickness _skeletonPadding;
    private Action? _onAction;

    public Grid Root { get; } = new();

    // `inset`: false when the state sits inside a page under its title (no page padding of its own).
    public StateView(FrameworkElement content, SkeletonShape shape, bool inset = true)
    {
        _content = content;
        _shape = shape;
        if (inset)
            Ui.PagePadding(Root, p =>
            {
                _skeletonPadding = p;
                if (_skeleton != null) _skeleton.Padding = p;
            });
        _text.TextAlignment = TextAlignment.Center;
        _action.HorizontalAlignment = HorizontalAlignment.Center;
        _action.Click += (_, _) => _onAction?.Invoke();
        _icon.Style = Ui.Styles.Get("SecondaryIcon20");
        _message = Ui.Stack(Space.M, _icon, _text, _action);
        _message.HorizontalAlignment = HorizontalAlignment.Center;
        _message.VerticalAlignment = VerticalAlignment.Center;
        _message.MaxWidth = Size.MessageMax;
        if (!inset) _message.Margin = Pad.SectionTop;
        Root.Children.Add(content);
        Root.Children.Add(_message);
        ShowContent();
    }

    public void ShowLoading()
    {
        if (_skeleton == null)
        {
            _skeleton = BuildSkeleton(_shape);
            _skeleton.Padding = _skeletonPadding;
            Root.Children.Add(_skeleton);
        }
        Set(loading: true, message: false);
    }

    public void ShowContent() => Set(loading: false, message: false);

    public void ShowEmpty(string glyph, string text, string? actionText = null, Action? action = null) =>
        ShowMessage(glyph, text, actionText, action);

    public void ShowError(string text, Action retry) => ShowMessage(Icons.Error, text, Loc.S("Action_Retry"), retry);

    // A page whose data failed to load.
    public void ShowFailed(string error, Action retry) => ShowError(Loc.S("Page_Error") + "\n" + error, retry);

    public void ShowResult(bool empty, string error, Action retry, string emptyGlyph, string emptyText)
    {
        if (error.Length > 0 && empty) ShowFailed(error, retry);
        else if (empty) ShowEmpty(emptyGlyph, emptyText);
        else ShowContent();
    }

    private void ShowMessage(string glyph, string text, string? actionText, Action? action)
    {
        _icon.Glyph = glyph;
        _text.Text = text;
        _onAction = action;
        _action.Content = actionText;
        _action.Visibility = Ui.Show(action != null);
        Set(loading: false, message: true);
    }

    private void Set(bool loading, bool message)
    {
        _content.Visibility = Ui.Show(!loading && !message);
        if (_skeleton != null) _skeleton.Visibility = Ui.Show(loading);
        _message.Visibility = Ui.Show(message);
    }

    // Tiles a tile gap apart and rows at the row pitch, as TileGrid and TrackList lay them out.
    private static StackPanel BuildSkeleton(SkeletonShape shape)
    {
        var panel = Section.Stack();
        if (shape == SkeletonShape.Page) panel.Children.Add(Ui.Skeleton(Size.SkeletonLine, Space.XXL));
        if (shape is SkeletonShape.Page or SkeletonShape.Tiles)
        {
            var tiles = Ui.Row(Space.S);
            for (int i = 0; i < Count.SkeletonTiles; i++) tiles.Children.Add(Ui.Skeleton(Size.Tile, Size.Tile));
            panel.Children.Add(tiles);
        }
        if (shape is SkeletonShape.Page or SkeletonShape.Rows)
        {
            var rows = Ui.Stack(0);
            for (int i = 0; i < Count.SkeletonRows; i++)
            {
                var row = Ui.Row(Space.M, Ui.Skeleton(Size.RowArt, Size.RowArt), Ui.Skeleton(Size.SkeletonLine, Space.L));
                row.Height = Size.Row;
                foreach (var block in row.Children) ((FrameworkElement)block).VerticalAlignment = VerticalAlignment.Center;
                rows.Children.Add(row);
            }
            panel.Children.Add(rows);
        }
        return panel;
    }
}
