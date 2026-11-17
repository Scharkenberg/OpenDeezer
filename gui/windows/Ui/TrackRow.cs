// One track row: index, art, title and artist, album, like, duration and More. A queue row is
// compact: art (with the play button and the now-playing mark over it), title and artist across
// the row, and the duration, which hover or focus swaps for Remove and More. Built once per container.

using System;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Input;

namespace OpenDeezer;

internal readonly record struct RowOptions(bool ShowAlbum, int QueueIndex = -1);

internal sealed class TrackRow
{
    private static readonly string s_columns =
        FormattableString.Invariant($"{Size.RowButton} {Size.RowArt} * * {Size.RowButton} {Size.DurationColumn} {Size.RowButton}");
    private static readonly string s_queueColumns = FormattableString.Invariant($"{Size.QueueArt} * auto");

    private readonly TextBlock _index = Ui.Time(0, TextKind.SecondaryCaption);
    private readonly FontIcon _playing;
    private readonly Action<TrackRow> _activate;
    // Play, Like and More: a full row makes them on first need (hover, focus, a liked track), as
    // most rows of a long list never need them; a queue row has them from the start.
    internal Button? _play, _like, _more;
    private readonly Grid? _indexCell, _likeCell, _moreCell;
    private readonly Art _art;
    private readonly Border _explicit = Ui.ExplicitChip();
    private readonly TextBlock _title = Ui.Text("");
    private readonly TextBlock _artist = Ui.Text("", TextKind.SecondaryCaption);
    private readonly TextBlock _album = Ui.Text("", TextKind.SecondaryCaption);
    private readonly TextBlock _duration = Ui.Time(0);
    // Shown in the trailing cell while the track is available offline.
    internal readonly FontIcon _pinned = Ui.Icon(Icons.Pinned, IconSize.Small);
    private readonly ColumnDefinition? _albumColumn;
    // Queue rows only.
    private readonly bool _queue;
    private readonly Button? _remove;
    private readonly Border? _scrim;
    private readonly StackPanel? _actions;
    private int _queueIndex = -1;
    private bool _hover, _focused;

    public Control Container { get; }
    public Track? Track { get; private set; }
    public int Index { get; private set; } = -1;

    // `container` is the focusable element around `g`: the ListViewItem, or the row itself in a section.
    public TrackRow(Grid g, Control container, Action<TrackRow> activate, Action<TrackRow>? remove = null)
    {
        Container = container;
        container.Tag = this;
        _activate = activate;
        _queue = remove != null;
        _art = new Art(ArtShape.Small, _queue ? Size.QueueArt : Size.RowArt, reuse: true);
        _playing = _queue ? new FontIcon { Glyph = Icons.Volume, Style = Ui.Styles.Get("ScrimIcon16") } : Ui.Icon(Icons.Volume, accent: true);
        Ui.Name(_playing, Loc.S("Tooltip_NowPlaying"));
        Ui.Name(_pinned, Loc.S("Offline_Available"));
        _pinned.VerticalAlignment = VerticalAlignment.Center;
        _pinned.HorizontalAlignment = HorizontalAlignment.Center;
        foreach (var tb in new[] { _index, _duration, _album })
            tb.VerticalAlignment = VerticalAlignment.Center;
        _index.HorizontalAlignment = HorizontalAlignment.Center;
        _duration.TextAlignment = TextAlignment.Right;
        _duration.HorizontalAlignment = HorizontalAlignment.Right;
        var lines = Ui.Stack(0, Ui.Grid("auto *", 0, _explicit, _title), _artist);
        lines.VerticalAlignment = VerticalAlignment.Center;
        _art.Root.VerticalAlignment = VerticalAlignment.Center;
        if (_queue)
        {
            CreateButtons();
            _remove = RowButton(Icons.Remove, "Queue_Remove", () => remove!(this));
            // The play button covers the art; the scrim keeps its glyph and the now-playing mark legible.
            _play!.Style = Ui.Styles.Get("ScrimButtonStyle");
            Ui.Square(_play, Size.QueueArt);
            _scrim = new Border { Style = Ui.Styles.Get("ArtScrimBorder") };
            var artCell = Ui.Square(new Grid { VerticalAlignment = VerticalAlignment.Center }, Size.QueueArt);
            foreach (var e in new UIElement[] { _art.Root, _scrim, _playing, _play }) artCell.Children.Add(e);
            _actions = Ui.Row(0, _remove, _more!);
            _actions.VerticalAlignment = VerticalAlignment.Center;
            var trailing = new Grid();
            var atRest = Ui.Row(Space.XS, _pinned, _duration);
            atRest.HorizontalAlignment = HorizontalAlignment.Right;
            trailing.Children.Add(atRest);
            trailing.Children.Add(_actions);
            g.ColumnSpacing = Space.S;
            Ui.Fill(g, s_queueColumns, artCell, lines, trailing);
        }
        else
        {
            _indexCell = new Grid();
            _indexCell.Children.Add(_index);
            _indexCell.Children.Add(_playing);
            _likeCell = new Grid();
            _moreCell = new Grid();
            _moreCell.Children.Add(_pinned);
            g.ColumnSpacing = Space.M;
            Ui.Fill(g, s_columns, _indexCell, _art.Root, lines, _album, _likeCell, _duration, _moreCell);
            _albumColumn = g.ColumnDefinitions[3];
        }

        // Double-click or Enter plays; a single click only focuses the row.
        container.DoubleTapped += (_, e) =>
        {
            e.Handled = true;
            activate(this);
        };
        container.AddHandler(UIElement.KeyDownEvent, new KeyEventHandler((_, e) =>
        {
            if (!ReferenceEquals(e.OriginalSource, container)) return;
            if (e.Key == Windows.System.VirtualKey.Enter) activate(this);
            else if (e.Key == Windows.System.VirtualKey.Delete && remove != null) remove(this);
            else return;
            e.Handled = true;
        }), true);
        if (container is not SelectorItem)
        {
            container.Tapped += (_, _) => container.Focus(FocusState.Pointer);
        }
        // Mouse and pen only: touch opens the menu by press-and-hold.
        container.PointerEntered += (_, e) => SetHover(e.Pointer.PointerDeviceType != PointerDeviceType.Touch);
        container.PointerExited += (_, _) => SetHover(false);
        container.PointerCanceled += (_, _) => SetHover(false);
        container.GotFocus += (_, _) => SetFocused(true);
        container.LostFocus += (_, _) => SetFocused(false);
    }

    public static Grid ColumnHeader(out ColumnDefinition album)
    {
        var index = Ui.Text("#", TextKind.SecondaryCaption);
        index.HorizontalAlignment = HorizontalAlignment.Center;
        var duration = Ui.Icon(Icons.Clock, IconSize.Small);
        duration.HorizontalAlignment = HorizontalAlignment.Right;
        Ui.Name(duration, Loc.S("Column_Duration"));
        var title = Ui.Text(Loc.S("Column_Title"), TextKind.SecondaryCaption);
        Grid.SetColumnSpan(title, 2);
        var g = new Grid { Style = Ui.Styles.Get("ColumnHeaderGrid"), ColumnSpacing = Space.M };
        Ui.Fill(g, s_columns, index, title, null, Ui.Text(Loc.S("Column_Album"), TextKind.SecondaryCaption), null, duration);
        album = g.ColumnDefinitions[3];
        return g;
    }

    public static GridLength AlbumWidth(bool shown) => shown ? new GridLength(1, GridUnitType.Star) : new GridLength(0);

    public void Bind(Track t, int index, RowOptions options)
    {
        Track = t;
        Index = index;
        _queueIndex = options.QueueIndex;
        if (_albumColumn != null) _albumColumn.Width = AlbumWidth(options.ShowAlbum);
        Ui.SetText(_index, (index + 1).ToString(System.Globalization.CultureInfo.CurrentCulture));
        _art.Set(t.ArtworkUrl);
        _explicit.Visibility = Ui.Show(t.IsExplicit);
        Ui.SetText(_title, t.Name);
        Ui.SetText(_artist, t.ArtistLine);
        Ui.SetText(_album, t.AlbumName);
        Ui.SetText(_duration, t.DurationMs > 0 ? Fmt.Time(t.DurationMs) : "");
        AutomationProperties.SetName(Container, t.ArtistLine.Length == 0 ? t.Name : t.Name + ", " + t.ArtistLine);
        Render();
    }

    internal void SetHover(bool on)
    {
        _hover = on;
        if (Container is not SelectorItem) Container.Style = on ? Ui.Styles.TrackRowHover : Ui.Styles.TrackRow;
        Render();
    }

    private void SetFocused(bool on)
    {
        _focused = on;
        Render();
    }

    // In the queue the playing row is the queue's own, and the rows before it are dimmed.
    private void Render()
    {
        if (Track is not { } t) return;
        bool playable = t.Id.Length > 0;
        bool playing = _queue ? Index == _queueIndex : playable && t.Id == App.Player.Current?.Id;
        bool played = _queue && Index < _queueIndex;
        bool menus = playable && !t.IsEpisode;
        bool reveal = _hover || _focused;
        bool pinned = menus && App.Offline.IsPinned(t.Id);
        _title.Style = playing ? Ui.Styles.RowTitlePlaying : played ? Ui.Styles.RowTitlePlayed : Ui.Styles.RowTitle;
        _artist.Style = Ui.TextStyle(played ? TextKind.TertiaryCaption : TextKind.SecondaryCaption);
        _playing.Visibility = Ui.Show(playing && !_hover);
        if (_queue)
        {
            // The trailing cell holds the duration at rest and the actions on hover or focus,
            // so the title gets the row's whole width until then.
            _scrim!.Visibility = Ui.Show(playing || (_hover && playable));
            _play!.Visibility = Ui.Show(_hover && playable);
            _actions!.Visibility = Ui.Show(reveal);
            _duration.Visibility = Ui.Show(!reveal);
            _pinned.Visibility = Ui.Show(pinned && !reveal);
            _more!.Visibility = Ui.Show(menus);
            return;
        }
        // The mark gives way to More on hover or focus.
        _pinned.Visibility = Ui.Show(pinned && !reveal);
        bool liked = menus && App.Player.IsLiked(t.Id);
        _index.Visibility = Ui.Show(!playing && !(_hover && playable));
        _index.Style = _artist.Style;
        if (reveal || liked) CreateButtons();
        if (_play == null) return;
        Ui.Reveal(_play, _hover && playable);
        Ui.ShowLiked(_like!, liked);
        Ui.Reveal(_like!, menus && (liked || reveal));
        Ui.Reveal(_more!, menus && reveal);
    }

    private void CreateButtons()
    {
        if (_play != null) return;
        _play = RowButton(Icons.Play, "Tooltip_Play", () => _activate(this));
        _more = RowButton(Icons.More, "Tooltip_More", () => Container.ContextFlyout?.ShowAt(Container));
        if (_queue) return;
        _like = RowAction(Ui.LikeButton(Size.RowButton, () =>
        {
            if (Track is { } t) App.Player.Like(t, !App.Player.IsLiked(t.Id));
        }));
        _indexCell!.Children.Add(_play);
        _likeCell!.Children.Add(_like);
        _moreCell!.Children.Add(_more);
    }

    private static Button RowButton(string glyph, string nameKey, Action onClick) =>
        RowAction(Ui.Square(Ui.IconButton(glyph, nameKey, onClick), Size.RowButton));

    // Out of the tab order (the row is the stop), a double-click does not play, and it fades in.
    private static Button RowAction(Button b)
    {
        b.IsTabStop = false;
        b.DoubleTapped += (_, e) => e.Handled = true;
        Ui.Fades(b, Timing.RevealMs);
        return b;
    }
}
