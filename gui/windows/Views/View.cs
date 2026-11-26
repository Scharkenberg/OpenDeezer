// Base of every page shown in the window's view host.

using System;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal abstract class View
{
    private CancellationTokenSource _cts = new();
    private double? _restoreOffset;
    private object? _filledFrom;

    protected static Player Player => App.Player;
    protected static Library Library => App.Library;
    protected static Session Session => App.Session;
    protected static Navigator Nav => App.Navigator;

    public abstract FrameworkElement Root { get; }

    public virtual ScrollViewer? Scroller => null;

    // Canceled when the view is hidden: loaders check it after every await.
    protected CancellationToken Ct => _cts.Token;

    public abstract void Show(object? arg, bool isBack);

    public virtual void Hide() { }

    // Whether `arg` opens what `shown` already shows (Navigator.Go then stays put).
    public virtual bool SameArg(object? shown, object? arg) => Equals(shown, arg);

    protected virtual void OnLibraryChanged(string key) { }

    internal void Enter(object? arg, bool isBack, double? offset)
    {
        _cts = new CancellationTokenSource();
        _restoreOffset = offset;
        Library.Changed += OnLibraryChanged;
        Show(arg, isBack);
    }

    internal void Leave()
    {
        Library.Changed -= OnLibraryChanged;
        _cts.Cancel();
        Hide();
    }

    // A Library value, with the skeleton shown while it loads; null when the view was left meanwhile.
    protected async Task<T?> LoadAsync<T>(string key, StateView state) where T : Reply
    {
        var ct = Ct;
        if (Library.Peek<T>(key) == null) state.ShowLoading();
        var value = await Library.GetAsync<T>(key);
        return ct.IsCancellationRequested ? null : value;
    }

    // True when the view was last filled from this very instance (Library swaps the instance on
    // every change), so a visit or Back keeps the rows and tiles it has; else remembers `data`.
    protected bool Same(object data)
    {
        if (ReferenceEquals(data, _filledFrom)) return true;
        _filledFrom = data;
        return false;
    }

    // Called once the data is in: after a Back it restores the scroll position, once.
    protected void ContentReady()
    {
        if (_restoreOffset is not double y || Scroller is not { } sv) return;
        _restoreOffset = null;
        // A virtualized extent grows over several layout passes and a refilled list scrolls back: retry.
        int passes = 0;
        void Restore(object? sender, object e)
        {
            if (Math.Abs(sv.VerticalOffset - y) < 1 || ++passes > Count.RestorePasses) sv.LayoutUpdated -= Restore;
            else sv.ChangeView(null, y, null, disableAnimation: true);
        }
        sv.LayoutUpdated += Restore;
        sv.ChangeView(null, y, null, disableAnimation: true);
    }

    protected void KeepScroll(Action refill)
    {
        _restoreOffset = Scroller?.VerticalOffset;
        refill();
        ContentReady();
    }
}
