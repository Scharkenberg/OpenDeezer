// The title-bar search box: searches as you type (two characters or more, after a
// pause) and on Enter. Empty and focused, it offers the account's recent searches, and clearing
// them (asked first).

using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class SearchBox
{
    private readonly Navigator _navigator;
    // The suggestions shown: recent searches, then Clear (null).
    private List<SearchItem?> _recent = new();

    public AutoSuggestBox Root { get; } = new()
    {
        PlaceholderText = Loc.S("Search_Placeholder"),
        QueryIcon = Ui.Icon(Icons.Search),
        // One width, whatever is typed; it only narrows when the title bar has less room.
        MaxWidth = Size.FieldMax,
        Height = Size.TitleBarField,
        HorizontalAlignment = HorizontalAlignment.Stretch,
        VerticalAlignment = VerticalAlignment.Center,
        TextBoxStyle = Ui.Styles.Get("TitleBarSearchTextBox"),
    };

    public SearchBox(DispatcherQueue dq, Navigator navigator)
    {
        _navigator = navigator;
        AutomationProperties.SetName(Root, Loc.S("Nav_Search"));
        var debounce = dq.CreateTimer();
        debounce.Interval = TimeSpan.FromMilliseconds(Timing.SearchDebounceMs);
        debounce.IsRepeating = false;
        debounce.Tick += (_, _) => Search(force: false);
        Root.TextChanged += (_, e) =>
        {
            if (e.Reason != AutoSuggestionBoxTextChangeReason.UserInput) return;
            if (Root.Text.Length > 0) Root.ItemsSource = null;
            else ShowRecent();
            debounce.Stop();
            debounce.Start();
        };
        Root.GotFocus += (_, _) =>
        {
            if (Root.Text.Length == 0) ShowRecent();
        };
        Root.QuerySubmitted += (_, e) =>
        {
            debounce.Stop();
            if (e.ChosenSuggestion is string chosen) Pick(chosen);
            else Search(force: true);
        };
        // Choosing a suggestion with the keys would put its text in the box: it opens instead.
        Root.SuggestionChosen += (_, _) => Root.Text = "";
    }

    private async void ShowRecent()
    {
        if (!App.Session.SignedIn || App.Offline.Mode) return;
        var (items, _) = await Task.Run(() => DeezerCore.SearchHistory(Count.SearchHistory));
        if (Root.Text.Length > 0 || items.Length == 0) return;
        _recent = [.. items, null];
        Root.ItemsSource = _recent.Select(Line).ToList();
        Root.IsSuggestionListOpen = Root.FocusState != FocusState.Unfocused;
    }

    private async void Pick(string chosen)
    {
        Root.ItemsSource = null;
        int i = _recent.Select(Line).ToList().IndexOf(chosen);
        if (i < 0) return;
        if (_recent[i] is { } item)
        {
            SearchView.OpenItem(item);
            return;
        }
        if (!await Dialogs.Confirm(Loc.S("Dialog_ClearRecentTitle"), Loc.S("Search_ClearRecentBody"), Loc.S("Queue_ClearAll"))) return;
        if (await Task.Run(() => DeezerCore.DZClearSearchHistory() != 0)) Toast.Info(Loc.S("Search_RecentCleared"), "");
        else Toast.Error(Loc.S("Error_Generic"), "");
    }

    // "Title · Kind", or Clear.
    private static string Line(SearchItem? r) =>
        r == null ? Loc.S("Search_ClearRecent") : string.Join(" · ", new[] { r.Title, SearchView.KindLabel(r.Type) }.Where(s => s.Length > 0));

    public bool Focus()
    {
        if (!App.Session.SignedIn) return false;
        Root.Focus(FocusState.Keyboard);
        return true;
    }

    // The box shows the query of the search page on screen, and is empty on any other page.
    public void Follow(View? current) => Root.Text = current is SearchView s ? s.Query : "";

    // Results open as a page of their own; emptying the box goes back to where the search began.
    private void Search(bool force)
    {
        string q = Root.Text.Trim();
        if (!App.Session.SignedIn) return;
        if (q.Length == 0)
        {
            if (_navigator.Current is SearchView) _navigator.Back();
            return;
        }
        if (!force && q.Length < Count.SearchMinChars) return;
        if (_navigator.Current is SearchView view) view.Run(q, force);
        else _navigator.Push<SearchView>(q);
    }
}
