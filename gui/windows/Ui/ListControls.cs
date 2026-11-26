// A list's search and sort, as Deezer's apps offer them on the client: the search matches the
// title and the names under it, lower-cased, after a pause; the sort is kept per list
// (settings.json). Changed runs on the UI thread after either moves.

using System;
using System.Collections.Generic;
using System.Linq;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed record SortOption<T>(string LabelKey, Func<T, string>? Key);

internal sealed class ListControls<T>
{
    private string _listKey;
    private readonly SortOption<T>[] _sorts;
    private readonly Func<T, string> _text;
    private readonly ComboBox _sort;
    private readonly TextBox _search;
    private string _query = "";

    public StackPanel Root { get; }

    public event Action? Changed;

    // As the list comes (no search, the first sort): its rows are its own order.
    public bool IsNatural => _query.Length == 0 && _sort.SelectedIndex <= 0;

    // `text`: what the search matches (title and the names under it); the first sort is the list's own order.
    public ListControls(string listKey, Func<T, string> text, params SortOption<T>[] sorts)
    {
        _listKey = listKey;
        _sorts = sorts;
        _text = text;
        App.Settings.ListSorts.TryGetValue(listKey, out int saved);
        _sort = Ui.Combo([.. sorts.Select(s => Loc.S(s.LabelKey))], Math.Clamp(saved, 0, sorts.Length - 1), i =>
        {
            App.Settings.ListSorts[_listKey] = i;
            App.Session.SaveSettingsSoon();
            Changed?.Invoke();
        });
        Ui.Name(_sort, Loc.S("List_Sort"));
        _search = new TextBox { PlaceholderText = Loc.S("List_Search"), Width = Size.FieldMin };
        Ui.Name(_search, Loc.S("List_Search"));
        var debounce = Microsoft.UI.Dispatching.DispatcherQueue.GetForCurrentThread().CreateTimer();
        debounce.Interval = TimeSpan.FromMilliseconds(Timing.SearchDebounceMs);
        debounce.IsRepeating = false;
        debounce.Tick += (_, _) =>
        {
            string q = _search.Text.Trim().ToLowerInvariant();
            if (q == _query) return;
            _query = q;
            Changed?.Invoke();
        };
        _search.TextChanged += (_, _) =>
        {
            debounce.Stop();
            debounce.Start();
        };
        Root = Ui.Row(Space.S, _search, _sort);
        Root.HorizontalAlignment = HorizontalAlignment.Left;
    }

    // Another list shows: its own sort, and no search.
    public void Use(string listKey)
    {
        _listKey = listKey;
        App.Settings.ListSorts.TryGetValue(listKey, out int saved);
        Ui.Quietly(_sort, () => _sort.SelectedIndex = Math.Clamp(saved, 0, _sorts.Length - 1));
        _search.Text = "";
        _query = "";
    }

    public IReadOnlyList<T> Apply(IReadOnlyList<T> all)
    {
        IEnumerable<T> rows = all;
        if (_query.Length > 0) rows = rows.Where(t => _text(t).ToLowerInvariant().Contains(_query));
        if (_sorts[Math.Max(0, _sort.SelectedIndex)].Key is { } key)
            rows = rows.OrderBy(key, StringComparer.Create(Loc.Culture, ignoreCase: true));
        return rows == all ? all : rows.ToList();
    }
}
