// The title-bar search box: searches as you type (two characters or more, after a
// pause) and on Enter.

using System;
using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class SearchBox
{
    private readonly Navigator _navigator;

    public AutoSuggestBox Root { get; } = new()
    {
        PlaceholderText = Loc.S("Search_Placeholder"),
        QueryIcon = Ui.Icon(Icons.Search),
        // One width, whatever is typed; it only narrows when the title bar has less room.
        MaxWidth = Size.SearchBox,
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
            debounce.Stop();
            debounce.Start();
        };
        Root.QuerySubmitted += (_, _) =>
        {
            debounce.Stop();
            Search(force: true);
        };
    }

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
