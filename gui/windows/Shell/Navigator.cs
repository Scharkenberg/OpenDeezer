// The back stack over cached views: one instance per view type, made on first use.

using System;
using System.Collections.Generic;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media.Animation;

namespace OpenDeezer;

internal sealed class Navigator
{
    private sealed class Entry(View view, object? arg)
    {
        public readonly View View = view;
        public readonly object? Arg = arg;
        public double Offset;
    }

    private readonly Dictionary<Type, View> _views = new();
    private readonly List<Entry> _stack = new();

    public ContentPresenter Host { get; } = new()
    {
        ContentTransitions = new TransitionCollection { new ContentThemeTransition() },
    };

    public event Action? Navigated;

    public View? Current => _stack.Count > 0 ? _stack[^1].View : null;
    public bool CanGoBack => _stack.Count > 1;

    // The back stack from the current page down.
    public IEnumerable<(View View, object? Arg)> Trail
    {
        get
        {
            for (int i = _stack.Count - 1; i >= 0; i--) yield return (_stack[i].View, _stack[i].Arg);
        }
    }

    public bool IsCurrent<T>() where T : View => Current is T;

    private T Get<T>() where T : View, new()
    {
        if (!_views.TryGetValue(typeof(T), out var v))
        {
            v = new T();
            _views[typeof(T)] = v;
        }
        return (T)v;
    }

    public void Root<T>(object? arg = null) where T : View, new()
    {
        Current?.Leave();
        _stack.Clear();
        Enter(new Entry(Get<T>(), arg), isBack: false);
    }

    public void Push<T>(object? arg = null) where T : View, new()
    {
        if (_stack.Count > 0)
        {
            var top = _stack[^1];
            top.Offset = top.View.Scroller?.VerticalOffset ?? 0;
            top.View.Leave();
        }
        if (_stack.Count == Count.BackStack) _stack.RemoveAt(0);
        Enter(new Entry(Get<T>(), arg), isBack: false);
    }

    // A page from the navigation pane, the title bar or a shortcut: pushed, so Back returns
    // to where the user was; the page already shown stays as it is.
    public void Go<T>(object? arg = null) where T : View, new()
    {
        if (Current is T v && v.SameArg(_stack[^1].Arg, arg)) return;
        Push<T>(arg);
    }

    public bool Back()
    {
        if (_stack.Count < 2) return false;
        _stack[^1].View.Leave();
        _stack.RemoveAt(_stack.Count - 1);
        var e = _stack[^1];
        Show(e, isBack: true, e.Offset);
        return true;
    }

    // Account switch: no view of the previous session survives.
    public void Reset()
    {
        Current?.Leave();
        _stack.Clear();
        _views.Clear();
        Host.Content = null;
    }

    private void Enter(Entry e, bool isBack)
    {
        _stack.Add(e);
        Show(e, isBack, null);
    }

    private void Show(Entry e, bool isBack, double? offset)
    {
        Host.Content = e.View.Root;
        e.View.Enter(e.Arg, isBack, offset);
        Navigated?.Invoke();
    }
}
