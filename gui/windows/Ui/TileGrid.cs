// A virtualized grid of tiles that fills the width of the view's scroller,
// optionally limited to a number of rows ("Show all" lifts the limit).

using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class TileGrid<T> where T : class
{
    private IReadOnlyList<T> _all = [];
    // A closed type: under Native AOT WinUI sees a collection as a vector only for the closed
    // types CsWinRT's generator saw, never one made inside a generic class.
    private ObservableCollection<object> _items = new();
    // _columns is 0 until the first measure; _rows is the limit every SetItems starts from.
    private int _columns, _rows, _maxRows, _shown = -1;

    public ItemsRepeater Root { get; }

    public Action<int, int>? ShownChanged { get; set; }

    // menu fills the grid's one context menu for the tile it opens on.
    public TileGrid(Func<T, TileData> map, Action<T> open, Action<T>? play = null, Action<IList<MenuFlyoutItemBase>, T>? menu = null)
    {
        MenuFlyout? flyout = null;
        if (menu != null)
        {
            var f = flyout = new MenuFlyout();
            f.Opening += (_, _) =>
            {
                f.Items.Clear();
                if (f.Target is MediaTile { Item: T item }) menu(f.Items, item);
            };
        }
        var recycler = new Recycler<MediaTile, object>(
            () => new MediaTile(tile => open((T)tile.Item!), play == null ? null : tile => play((T)tile.Item!)) { ContextFlyout = flyout },
            (tile, item, _) => tile.Bind(item, map((T)item)));
        Root = recycler.Attach(new ItemsRepeater
        {
            Layout = new UniformGridLayout
            {
                MinItemWidth = Size.Tile,
                MinColumnSpacing = Space.S,
                MinRowSpacing = Space.S,
                ItemsStretch = UniformGridLayoutItemsStretch.Fill,
            },
        });
        // The same column count UniformGridLayout arrives at.
        Ui.OnWidth(Root, w => Math.Max(1, (int)((w + Space.S) / (Size.Tile + Space.S))), columns =>
        {
            _columns = columns;
            if (_maxRows > 0) Apply();
        });
    }

    public int Rows
    {
        set => _rows = value;
    }

    public void SetItems(IReadOnlyList<T> items) => SetItems(items, _rows);

    public void SetItems(IReadOnlyList<T> items, int maxRows)
    {
        _all = items;
        _maxRows = maxRows;
        _shown = -1;
        Apply();
    }

    public void ShowAll()
    {
        _maxRows = 0;
        // "Show all" fills the grid afresh: the added rows do not fly in.
        _shown = -1;
        Apply();
    }

    public void Update(IReadOnlyList<T> items)
    {
        _all = items;
        if (_maxRows == 0 && Ui.TryEdit<object>(_items, items))
        {
            _shown = items.Count;
            ShownChanged?.Invoke(_all.Count, _shown);
        }
        else SetItems(items);
    }

    private void Apply()
    {
        // A row-limited grid waits for its width rather than building one column first.
        if (_maxRows > 0 && _columns == 0)
        {
            ShownChanged?.Invoke(_all.Count, 0);
            return;
        }
        int shown = _maxRows > 0 ? Math.Min(_all.Count, _columns * _maxRows) : _all.Count;
        if (shown != _shown)
        {
            if (_shown < 0)
            {
                _items = new ObservableCollection<object>();
                for (int i = 0; i < shown; i++) _items.Add(_all[i]);
                Root.ItemsSource = _items;
            }
            else
            {
                // A new column count (the window resized): the tiles shown stay and the row's end
                // grows or shrinks, without the edit transitions.
                if (Root.ItemTransitionProvider is EditTransitions t) t.QuietOnce();
                while (_items.Count > shown) _items.RemoveAt(_items.Count - 1);
                while (_items.Count < shown) _items.Add(_all[_items.Count]);
            }
            _shown = shown;
        }
        ShownChanged?.Invoke(_all.Count, shown);
    }
}
