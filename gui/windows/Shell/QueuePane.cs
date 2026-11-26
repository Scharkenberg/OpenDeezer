// The queue beside the views: inline on wide windows, an overlay below. A one-line header (title,
// how much is left, Clear) over a list that runs down to the player bar. It applies each queue
// change row by row.

using System;
using System.Linq;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class QueuePane
{
    private static Player Player => App.Player;

    private readonly TrackList _list = new(ListMode.Queue) { ShowAlbum = false, MenuContext = new TrackMenu(Queue: true) };
    private readonly StateView _state;
    private readonly Button _clear, _save;
    private readonly TextBlock _summary = Ui.Text("", TextKind.SecondaryCaption);

    public SplitView Root { get; }

    public event Action? Closed;

    // Asks the window to open the queue (a toast's Open).
    public static event Action? OpenRequested;

    public static void RequestOpen() => OpenRequested?.Invoke();

    // `window` is the window's root: its width class, not the pane's, decides inline or overlay.
    public QueuePane(UIElement content, FrameworkElement window)
    {
        _list.Activate = Player.PlayAt;
        _list.Remove = Player.RemoveAt;
        _list.Reorder = Player.Move;
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(_list.Root, Loc.S("Queue_Title"));
        _clear = Ui.IconButton(Icons.Delete, "Queue_Clear", ConfirmClear);
        var title = Ui.Text(Loc.S("Queue_Title"), TextKind.Strong);
        title.VerticalAlignment = VerticalAlignment.Center;
        _summary.VerticalAlignment = VerticalAlignment.Center;
        // The queue kept as a playlist (its tracks; episodes cannot be in one).
        _save = Ui.IconButton(Icons.Add, "Menu_SaveAsPlaylist", () => Menus.SaveAsPlaylist(Loc.S("Queue_Title"), Player.Queue));
        var header = Ui.Grid("auto * auto auto", Space.S, title, _summary, _save, _clear);
        header.Margin = Pad.PaneHeader;
        _state = new StateView(_list.Root, SkeletonShape.Rows);
        var pane = Ui.Rows("auto *", header, _state.Root);
        pane.Padding = Pad.Pane;
        Root = new SplitView
        {
            PanePlacement = SplitViewPanePlacement.Right,
            OpenPaneLength = Size.QueuePane,
            DisplayMode = SplitViewDisplayMode.Overlay,
            Pane = pane,
            Content = content,
        };
        Root.PaneClosed += (_, _) => Closed?.Invoke();
        Ui.OnWidthClass(window, c => Root.DisplayMode = c == WidthClass.Wide ? SplitViewDisplayMode.Inline : SplitViewDisplayMode.Overlay);
        _list.SetItems(Player.Queue);
        RenderState();
        Player.QueueChanged += Apply;
        App.Offline.Changed += RenderState; // Save as playlist follows offline mode
    }

    public bool IsOpen
    {
        get => Root.IsPaneOpen;
        set
        {
            Root.IsPaneOpen = value;
            if (value) _list.ScrollIntoView(Player.QueueIndex);
        }
    }

    private static async void ConfirmClear()
    {
        if (await Dialogs.Confirm(Loc.S("Queue_ClearTitle"), Loc.S("Queue_ClearBody"), Loc.S("Queue_ClearAll"))) Player.ClearQueue();
    }

    // A drag has already moved its row by the time the player reports the move.
    private void Apply(QueueChange change)
    {
        if (change.Kind == QueueChangeKind.Move && !_list.Shows(Player.Queue)) _list.Move(change.Index, change.To);
        else _list.Update(Player.Queue);
        RenderState();
    }

    // "12 tracks · 47 min left": the count, and the time from the playing track to the end.
    private void RenderState()
    {
        var q = Player.Queue;
        // Clear keeps the playing row: with only that left there is nothing to clear.
        _clear.IsEnabled = q.Count > (Player.QueueIndex >= 0 ? 1 : 0);
        _save.IsEnabled = q.Any(t => !t.IsEpisode) && !App.Offline.Mode;
        long left = 0;
        for (int i = Math.Max(0, Player.QueueIndex); i < q.Count; i++) left += Math.Max(0, q[i].DurationMs);
        Ui.SetText(_summary, q.Count == 0 ? ""
            : left <= 0 ? Loc.Plural("Tracks", q.Count)
            : Loc.Plural("Tracks", q.Count) + " · " + Loc.Format("Queue_LeftFormat", Fmt.Duration(left)));
        if (q.Count == 0) _state.ShowEmpty(Icons.Queue, Loc.S("Empty_Queue"));
        else _state.ShowContent();
    }
}
