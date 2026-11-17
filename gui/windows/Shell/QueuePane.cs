// The queue beside the views: inline on wide windows, an overlay below. A one-line header (title,
// how much is left, Clear) over a list that runs down to the player bar. It applies each queue
// change row by row.

using System;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

internal sealed class QueuePane
{
    private static Player Player => App.Player;

    private readonly TrackList _list = new(ListMode.Queue) { ShowAlbum = false, MenuContext = new TrackMenu(Queue: true) };
    private readonly StateView _state;
    private readonly Button _clear;
    private readonly TextBlock _summary = Ui.Text("", TextKind.SecondaryCaption);

    public SplitView Root { get; }

    public event Action? Closed;

    // `window` is the window's root: its width class, not the pane's, decides inline or overlay.
    public QueuePane(UIElement content, FrameworkElement window)
    {
        _list.Activate = Player.PlayAt;
        _list.Remove = Player.RemoveAt;
        _list.Reorder = Player.Move;
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(_list.Root, Loc.S("Queue_Title"));
        _clear = Ui.IconButton(Icons.Delete, "Queue_Clear", Player.ClearQueue);
        var title = Ui.Text(Loc.S("Queue_Title"), TextKind.Strong);
        title.VerticalAlignment = VerticalAlignment.Center;
        _summary.VerticalAlignment = VerticalAlignment.Center;
        var header = Ui.Grid("auto * auto", Space.S, title, _summary, _clear);
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
        _clear.IsEnabled = q.Count > 0;
        long left = 0;
        for (int i = Math.Max(0, Player.QueueIndex); i < q.Count; i++) left += Math.Max(0, q[i].DurationMs);
        Ui.SetText(_summary, q.Count == 0 ? ""
            : left <= 0 ? Loc.Plural("Tracks", q.Count)
            : Loc.Plural("Tracks", q.Count) + " · " + Loc.Format("Queue_LeftFormat", Fmt.Duration(left)));
        if (q.Count == 0) _state.ShowEmpty(Icons.Queue, Loc.S("Empty_Queue"));
        else _state.ShowContent();
    }
}
