// A titled block of a view: section title, optional "Show all", then content. Sections bring
// no outer margin: a page stacks them Space.L apart (Section.Stack).

using System;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class Section
{
    private readonly HyperlinkButton? _showAll;

    public StackPanel Root { get; }

    public Section(string titleKey, UIElement content, Action? showAll = null)
    {
        var title = Ui.Text(Loc.S(titleKey), TextKind.SectionTitle);
        _showAll = showAll == null ? null : Ui.Link(Loc.S("Action_ShowAll"), showAll);
        var header = Ui.Grid("* auto", Space.M, title, _showAll);
        header.Margin = Pad.Header;
        Root = Ui.Stack(0, header, content);
    }

    // Sections and other blocks of a page, one section gap apart.
    public static StackPanel Stack(params UIElement?[] blocks) => Ui.Stack(Space.L, blocks);

    // A tile grid limited to `rows`. "Show all" runs `showAll`, or else lifts the limit.
    public static Section Tiles<T>(string titleKey, TileGrid<T> grid, int rows, Action? showAll = null) where T : class
    {
        var section = new Section(titleKey, grid.Root, showAll ?? grid.ShowAll);
        grid.ShownChanged = (total, shown) =>
        {
            section.SetShowAll(total, shown);
            section.Visible = total > 0;
        };
        grid.Rows = rows;
        section.Visible = false;
        return section;
    }

    public bool Visible
    {
        set => Root.Visibility = Ui.Show(value);
    }

    public void SetShowAll(int total, int shown)
    {
        if (_showAll == null) return;
        _showAll.Content = Loc.Format("Search_ShowAllFormat", total);
        _showAll.Visibility = Ui.Show(total > shown);
    }
}
