// The header of a collection or artist: raised art, kind label, title, meta
// line, actions. Below Narrow the art shrinks and the text stacks under it.

using System;
using System.Numerics;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Documents;
using Microsoft.UI.Xaml.Media;

namespace OpenDeezer;

internal sealed class Hero
{
    private readonly Art _art;
    private readonly Border _generated;
    private readonly FontIcon _generatedIcon = new() { Style = Ui.Styles.Get("GeneratedArtIcon") };
    private readonly TextBlock _kind = Ui.Text("", TextKind.SecondaryCaption);
    internal readonly TextBlock _title = Ui.Text("", TextKind.HeroTitle, wrap: true);
    private readonly TextBlock _meta = Ui.Text("", TextKind.SecondaryBody, wrap: true);
    private readonly double _artSize;

    public Grid Root { get; }
    public StackPanel Actions { get; }

    public Hero(ArtShape shape, double artSize, params UIElement[] actions)
    {
        _artSize = artSize;
        _art = new Art(shape, artSize);
        _generated = new Border { Style = Ui.Styles.Get("GeneratedArtBorder"), Width = artSize, Height = artSize, Child = _generatedIcon };
        var artCell = new Grid { HorizontalAlignment = HorizontalAlignment.Left, VerticalAlignment = VerticalAlignment.Bottom };
        artCell.Children.Add(_art.Root);
        artCell.Children.Add(_generated);
        if (shape != ArtShape.Circle)
        {
            artCell.Shadow = new ThemeShadow();
            artCell.Translation = new Vector3(0, 0, Size.HeroElevation);
        }
        Actions = Ui.Row(Space.S, actions);
        Actions.Margin = Pad.HeroActions;
        var info = Ui.Stack(Space.XS, _kind, _title, _meta, Actions);
        info.VerticalAlignment = VerticalAlignment.Bottom;
        Root = Ui.Grid("auto *", Space.L, artCell, info);
        Root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        Root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        Root.Margin = Pad.Header;
        Ui.OnWidthClass(Root, c =>
        {
            bool narrow = c == WidthClass.Narrow;
            double size = narrow ? Size.HeroArtNarrow : _artSize;
            _art.Resize(size);
            _generated.Width = size;
            _generated.Height = size;
            Grid.SetColumn(info, narrow ? 0 : 1);
            Grid.SetRow(info, narrow ? 1 : 0);
            Grid.SetColumnSpan(info, narrow ? 2 : 1);
            Root.RowSpacing = narrow ? Space.L : 0;
        });
    }

    // `generated` is a glyph shown on an accent tile instead of artwork (Liked songs, Flow);
    // `openFirst` makes the meta line's first part (an album's artist) a link.
    public void Set(string kind, string title, string meta, string artUrl, string? generated = null, Action? openFirst = null)
    {
        Ui.SetText(_kind, kind);
        _kind.Visibility = Ui.Show(kind.Length > 0);
        Ui.SetText(_title, title);
        int cut = meta.IndexOf(" · ");
        string first = cut < 0 ? meta : meta[..cut];
        if (openFirst != null && first.Length > 0)
        {
            var link = new Hyperlink { Inlines = { new Run { Text = first } } };
            link.Click += (_, _) => openFirst();
            _meta.Inlines.Clear();
            _meta.Inlines.Add(link);
            _meta.Inlines.Add(new Run { Text = meta[first.Length..] });
        }
        else Ui.SetText(_meta, meta);
        _meta.Visibility = Ui.Show(meta.Length > 0);
        _art.Set(artUrl, title);
        _generated.Visibility = Ui.Show(generated != null);
        if (generated != null) _generatedIcon.Glyph = generated;
        AutomationProperties.SetName(_art.Root, title);
    }
}
