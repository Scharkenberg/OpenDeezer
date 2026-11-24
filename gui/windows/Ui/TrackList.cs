// Track rows in a ListView when the list is the whole view or the queue (arrow keys, drag
// reorder), else in an ItemsRepeater inside the view's scroller. The list follows the player.

using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Markup;
using Microsoft.UI.Xaml.Media.Animation;

namespace OpenDeezer;

internal enum ListMode { Section, Full, Queue }

internal sealed class TrackList
{
    private static DataTemplate? s_template;
    private readonly MenuFlyout _menu = new();
    private readonly List<TrackRow> _rows = new();
    private readonly ListMode _mode;
    private readonly ListView? _list;
    private readonly ItemsRepeater? _repeater;
    private ObservableCollection<Track> _items = new();
    // The whole list: the rows may show the first part of it only, and a row plays from all of it.
    private IReadOnlyList<Track> _all = [];
    private Track[] _dragOrder = [];
    private ColumnDefinition? _headerAlbum;
    private ScrollViewer? _scroller;
    private bool _showAlbum = true, _albumFits = true;

    public FrameworkElement Root { get; }
    public IReadOnlyList<Track> Items => _items;
    // The ListView's own scroller, found once its template is applied; a section list has none.
    public ScrollViewer? Scroller => _scroller ??= _list == null ? null : FindScroller(_list);

    public TrackMenu MenuContext { get; set; } = new();
    // The list its rows play from, for Deezer's listen log.
    public ListenContext Context { get; set; } = ListenContext.None;
    public Action<int>? Activate { get; set; }
    public Action<int>? Remove { get; set; }
    public Action<int, int>? Reorder { get; set; }

    // Rows reorder by drag (the queue; a playlist of one's own in its own order).
    public bool CanReorder
    {
        set
        {
            if (_list == null) return;
            _list.CanDragItems = value;
            _list.CanReorderItems = value;
            _list.AllowDrop = value;
        }
    }

    public bool ShowAlbum
    {
        get => _showAlbum;
        set
        {
            _showAlbum = value;
            ApplyAlbum();
        }
    }

    public TrackList(ListMode mode = ListMode.Section)
    {
        _mode = mode;
        if (mode != ListMode.Section)
        {
            s_template ??= (DataTemplate)XamlReader.Load(
                "<DataTemplate xmlns=\"http://schemas.microsoft.com/winfx/2006/xaml/presentation\"><Grid/></DataTemplate>");
            _list = new ListView
            {
                SelectionMode = ListViewSelectionMode.None,
                ItemTemplate = s_template,
                ItemContainerStyle = Ui.Styles.TrackListItem,
                ItemsSource = _items,
                ItemContainerTransitions = new TransitionCollection { new AddDeleteThemeTransition(), new ReorderThemeTransition() },
            };
            _list.ContainerContentChanging += (_, args) =>
            {
                if (args.InRecycleQueue || args.ItemContainer.ContentTemplateRoot is not Grid g || args.Item is not Track t) return;
                var row = args.ItemContainer.Tag as TrackRow ?? NewRow(g, args.ItemContainer);
                Bind(row, t, args.ItemIndex);
                args.Handled = true;
            };
            EnableReorder(_list);
            CanReorder = mode == ListMode.Queue;
            Root = _list;
        }
        else
        {
            var recycler = new Recycler<ContentControl, Track>(() =>
            {
                var g = new Grid();
                var row = new ContentControl { Style = Ui.Styles.TrackRow, Content = g, Height = Size.Row, Padding = Pad.Row };
                NewRow(g, row);
                return row;
            }, (row, t, i) => Bind((TrackRow)row.Tag, t, i));
            _repeater = recycler.Attach(new ItemsRepeater
            {
                ItemsSource = _items,
                TabFocusNavigation = KeyboardNavigationMode.Once,
                XYFocusKeyboardNavigation = XYFocusKeyboardNavigationMode.Enabled,
            });
            _repeater.ElementIndexChanged += (_, e) =>
            {
                if (e.Element is FrameworkElement { Tag: TrackRow row } && e.NewIndex < _items.Count) Bind(row, _items[e.NewIndex], e.NewIndex);
            };
            Root = _repeater;
        }
        _menu.Opening += (_, _) =>
        {
            _menu.Items.Clear();
            if ((_menu.Target as FrameworkElement)?.Tag is TrackRow { Track: { } t } row)
                Menus.Track(_menu.Items, t, MenuContext with { Index = row.Index });
        };
        Ui.OnWidthAbove(Root, Size.AlbumColumnMin, fits =>
        {
            _albumFits = fits;
            ApplyAlbum();
        });
        // Rows miss the player's events while unloaded: catch up on the way back.
        Root.Loaded += (_, _) =>
        {
            Refresh();
            App.Player.TrackChanged += Refresh;
            App.Player.LikeChanged += Refresh;
            App.Offline.Changed += Refresh;
            Episodes.Changed += Refresh;
        };
        Root.Unloaded += (_, _) =>
        {
            App.Player.TrackChanged -= Refresh;
            App.Player.LikeChanged -= Refresh;
            App.Offline.Changed -= Refresh;
            Episodes.Changed -= Refresh;
        };
    }

    public object? Header
    {
        set => _list!.Header = value;
    }

    public Thickness Padding
    {
        set
        {
            if (_list != null) _list.Padding = value;
        }
    }

    public Grid ColumnHeader()
    {
        var header = TrackRow.ColumnHeader(out var album);
        _headerAlbum = album;
        ApplyAlbum();
        return header;
    }

    public void SetItems(IReadOnlyList<Track> tracks, int limit = int.MaxValue)
    {
        _all = tracks;
        _items = new ObservableCollection<Track>();
        for (int i = 0; i < tracks.Count && i < limit; i++) _items.Add(tracks[i]);
        if (_list != null) _list.ItemsSource = _items;
        else _repeater!.ItemsSource = _items;
    }

    public void Move(int from, int to)
    {
        _items.Move(from, to);
        Refresh();
    }

    public void Update(IReadOnlyList<Track> next)
    {
        _all = next;
        if (Ui.TryEdit(_items, next)) Refresh();
        else SetItems(next);
    }

    public bool Shows(IReadOnlyList<Track> tracks)
    {
        if (tracks.Count != _items.Count) return false;
        for (int i = 0; i < tracks.Count; i++)
            if (!ReferenceEquals(tracks[i], _items[i])) return false;
        return true;
    }

    public void ScrollIntoView(int index)
    {
        if (_list != null && index >= 0 && index < _items.Count) _list.ScrollIntoView(_items[index], ScrollIntoViewAlignment.Leading);
    }

    // The moved row is found by comparing orders: the queue can hold the same track twice.
    private void EnableReorder(ListView list)
    {
        list.DragItemsStarting += (_, _) => _dragOrder = [.. _items];
        list.DragItemsCompleted += (_, _) =>
        {
            var before = _dragOrder;
            _dragOrder = [];
            int a = 0, b = before.Length - 1;
            if (before.Length != _items.Count) return;
            while (a <= b && ReferenceEquals(before[a], _items[a])) a++;
            while (b > a && ReferenceEquals(before[b], _items[b])) b--;
            if (a >= b) return;
            Refresh();
            if (ReferenceEquals(_items[b], before[a])) Reorder?.Invoke(a, b);
            else Reorder?.Invoke(b, a);
        };
    }

    private TrackRow NewRow(Grid g, Control container)
    {
        if (container is ListViewItem)
        {
            container.MinHeight = _mode == ListMode.Queue ? Size.QueueRow : Size.Row;
            container.Padding = Pad.Row;
        }
        var row = new TrackRow(g, container, r =>
        {
            int i = r.Index;
            if (i < 0 || i >= _items.Count) return;
            if (Activate != null) Activate(i);
            else App.Player.Play(_all.Count > _items.Count ? _all : _items, i, Context);
        }, _mode == ListMode.Queue ? RemoveRow : null);
        _rows.Add(row);
        return row;
    }

    private void RemoveRow(TrackRow r)
    {
        int i = r.Index;
        if (i < 0 || i >= _items.Count) return;
        bool focused = r.Container.FocusState == FocusState.Keyboard;
        Remove?.Invoke(i);
        if (!focused || _list == null) return;
        _list.DispatcherQueue.TryEnqueue(() =>
        {
            if (_items.Count > 0 && _list.ContainerFromIndex(Math.Min(i, _items.Count - 1)) is Control c) c.Focus(FocusState.Keyboard);
        });
    }

    private void Bind(TrackRow row, Track t, int index)
    {
        row.Bind(t, index, new RowOptions(_showAlbum && _albumFits, _mode == ListMode.Queue ? App.Player.QueueIndex : -1));
        row.Container.ContextFlyout = t.Id.Length == 0 ? null : _menu;
    }

    private void ApplyAlbum()
    {
        if (_headerAlbum != null) _headerAlbum.Width = TrackRow.AlbumWidth(_showAlbum && _albumFits);
        Refresh();
    }

    public void Refresh() => Refresh("");

    private void Refresh(string id)
    {
        foreach (var row in _rows)
        {
            int i = _list != null ? _list.IndexFromContainer(row.Container) : _repeater!.GetElementIndex(row.Container);
            if (i >= 0 && i < _items.Count && (id.Length == 0 || _items[i].Id == id)) Bind(row, _items[i], i);
        }
    }

    private static ScrollViewer? FindScroller(DependencyObject root)
    {
        int n = Microsoft.UI.Xaml.Media.VisualTreeHelper.GetChildrenCount(root);
        for (int i = 0; i < n; i++)
        {
            var c = Microsoft.UI.Xaml.Media.VisualTreeHelper.GetChild(root, i);
            if (c is ScrollViewer sv) return sv;
            if (FindScroller(c) is { } deeper) return deeper;
        }
        return null;
    }
}
