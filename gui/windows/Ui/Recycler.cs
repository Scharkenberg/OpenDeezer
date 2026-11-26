// The one per-item element factory: an ItemsRepeater template that reuses
// elements from a pool and binds them to their data when realized.

using System;
using System.Collections.Generic;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed partial class Recycler<TElement, TData>(Func<TElement> create, Action<TElement, TData, int> bind) : IElementFactory
    where TElement : UIElement
{
    private readonly Stack<TElement> _pool = new();

    public ItemsRepeater Attach(ItemsRepeater repeater)
    {
        repeater.ItemTemplate = this;
        repeater.ItemTransitionProvider = new EditTransitions();
        repeater.ElementPrepared += (r, e) =>
        {
            if (e.Element is TElement el && r.ItemsSourceView?.GetAt(e.Index) is TData data) bind(el, data, e.Index);
        };
        return repeater;
    }

    public UIElement GetElement(ElementFactoryGetArgs args) => _pool.Count > 0 ? _pool.Pop() : create();

    public void RecycleElement(ElementFactoryRecycleArgs args)
    {
        if (args.Element is TElement el) _pool.Push(el);
    }
}

// The built-in add, remove and reposition transitions, for single-item edits only.
internal sealed partial class EditTransitions : LinedFlowLayoutItemCollectionTransitionProvider
{
    private long _quietUntil;

    // The edits of the next layout pass are not animated (a grid that follows the window's width).
    public void QuietOnce() => _quietUntil = Environment.TickCount64 + 250;

    protected override bool ShouldAnimateCore(ItemCollectionTransition transition) =>
        Environment.TickCount64 >= _quietUntil
        && (transition.Triggers & (ItemCollectionTransitionTriggers.CollectionChangeReset | ItemCollectionTransitionTriggers.LayoutTransition)) == 0
        && base.ShouldAnimateCore(transition);
}
