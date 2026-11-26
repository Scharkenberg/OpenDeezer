// One track: large artwork over a backdrop of the same art, then everything Deezer knows about
// it (artists, album, release, length, label, BPM, ISRC) and the credits.

using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace PurpleDisc;

internal sealed class TrackView : View
{
    private readonly Backdrop _backdrop;
    internal readonly Hero _hero;
    private readonly Button _like, _radio;
    internal readonly Grid _details = Ui.Grid("auto *", Space.L);
    internal readonly Grid _credits = Ui.Grid("auto *", Space.L);
    private readonly Section _detailsSection, _creditsSection;
    private readonly ScrollViewer _scroller;
    private readonly StateView _state;
    internal Track _track = new();

    public override FrameworkElement Root => _backdrop.Root;
    public override ScrollViewer? Scroller => _scroller;

    public static void Open(Track t)
    {
        if (t.Id.Length > 0 && !t.IsEpisode) App.Navigator.Go<TrackView>(t);
    }

    // A track is the same page whichever list's copy opens it.
    public override bool SameArg(object? shown, object? arg) => shown is Track a && arg is Track b && a.Id == b.Id;

    public TrackView()
    {
        _like = Ui.LikeButton(Size.IconButton, () => Player.Like(_track, !Player.IsLiked(_track.Id)));
        _radio = Ui.Button(Loc.S("Menu_StartRadio"), () => Menus.StartRadio(_track), Icons.Flow);
        _hero = new Hero(ArtShape.Large, Size.TrackArt,
            Ui.Button(Loc.S("Btn_Play"), () => Player.Play(new[] { _track }, 0, new("track_page", _track.Id)), Icons.Play, accent: true),
            _radio,
            _like,
            Menus.MoreButton(items => Menus.Track(items, _track, new TrackMenu(Page: true))));
        _details.RowSpacing = _credits.RowSpacing = Space.S;
        _detailsSection = new Section("Section_Details", _details);
        _creditsSection = new Section("Section_Credits", _credits);
        // The hero shows what the list knew at once; the sections wait for the full record.
        _state = new StateView(Section.Stack(_detailsSection.Root, _creditsSection.Root), SkeletonShape.Rows, inset: false);
        _scroller = Ui.Page(Size.ContentMax, Section.Stack(_hero.Root, _state.Root));
        _backdrop = new Backdrop(_scroller);
    }

    public override void Show(object? arg, bool isBack)
    {
        if (arg is not Track t) return;
        Player.LikeChanged += OnLikeChanged;
        _track = t;
        Ui.ShowLiked(_like, Player.IsLiked(t.Id));
        // Disabled in offline mode, as in the menus.
        _like.IsEnabled = _radio.IsEnabled = !App.Offline.Mode;
        _hero.Set(Loc.S("Nav_Track"), t.Name, t.ArtistLine, t.ArtworkUrl);
        _backdrop.Set(t.ArtworkUrl);
        if (!isBack) _scroller.ChangeView(null, 0, null, disableAnimation: true);
        Load(t);
    }

    private async void Load(Track t)
    {
        var ct = Ct;
        _state.ShowLoading();
        var info = await Task.Run(() => DeezerCore.TrackInfo(t.Id));
        if (ct.IsCancellationRequested) return;
        if (info.Error.Length > 0)
        {
            _state.ShowFailed(info.Error, () => Load(t));
            return;
        }
        Fill(info);
        _state.ShowContent();
        ContentReady();
    }

    public override void Hide() => Player.LikeChanged -= OnLikeChanged;

    private void OnLikeChanged(string id)
    {
        if (id.Length == 0 || id == _track.Id) Ui.ShowLiked(_like, Player.IsLiked(_track.Id));
    }

    internal void Fill(TrackInfo i)
    {
        string title = i.Version.Length > 0 ? $"{i.Title} ({i.Version})" : i.Title;
        string kind = Loc.S("Nav_Track") + (i.Explicit ? " · " + Loc.S("Tooltip_Explicit") : "");
        string year = (i.ReleaseDate.Length > 0 ? i.ReleaseDate : i.Album.ReleaseDate) is { Length: >= 4 } d ? d[..4] : "";
        var meta = new[] { string.Join(", ", i.Artists.Where(a => a.Role == "main").Select(a => a.Name)), i.Album.Title, year }.Where(s => s.Length > 0);
        string art = i.Album.CoverUrl.Length > 0 ? i.Album.CoverUrl : _track.ArtworkUrl;
        _hero.Set(kind, title, string.Join(" · ", meta), art);
        _backdrop.Set(art);

        _details.Children.Clear();
        _details.RowDefinitions.Clear();
        if (i.Artists.Count > 0) Row(_details, "Track_Artists", Links(i.Artists.Select(a => (a.Name, a.Id.Length > 0 ? () => ArtistView.Open(a.Id) : (System.Action?)null))));
        if (i.Album.Title.Length > 0)
            Row(_details, "Track_Album", Links(new (string, System.Action?)[] { (i.Album.Title, i.Album.Id.Length > 0
                ? () => CollectionView.Open(Collection.Of(new Album { Id = i.Album.Id, Name = i.Album.Title, ArtworkUrl = art, Artists = i.Artists.Where(a => a.Role == "main").Select(a => new ArtistRef { Id = a.Id, Name = a.Name }).ToList() }))
                : null) }));
        string released = i.ReleaseDate.Length > 0 ? i.ReleaseDate : i.Album.ReleaseDate;
        Row(_details, "Track_Released", System.DateTime.TryParse(released, System.Globalization.CultureInfo.InvariantCulture,
            System.Globalization.DateTimeStyles.None, out var date) ? date.ToString("D", Loc.Culture) : released);
        if (i.TrackNumber > 0)
            Row(_details, "Track_Number", i.Album.TrackCount > 0 ? Loc.Format("Track_NumberFormat", i.TrackNumber, i.Album.TrackCount) : i.TrackNumber.ToString());
        if (i.DiskNumber > 1) Row(_details, "Track_Disc", i.DiskNumber.ToString());
        if (i.DurationMs > 0) Row(_details, "Track_Length", Fmt.Time(i.DurationMs));
        Row(_details, "Track_Genre", string.Join(", ", i.Album.Genres));
        Row(_details, "Track_Label", i.Album.Label);
        if (i.Bpm > 0) Row(_details, "Track_Bpm", i.Bpm.ToString("0"));
        Row(_details, "Track_Isrc", i.Isrc);
        Row(_details, "Track_Copyright", i.Copyright);
        _detailsSection.Visible = _details.Children.Count > 0;

        _credits.Children.Clear();
        _credits.RowDefinitions.Clear();
        // As the web player groups them: authors, composers, then everyone else, names joined by " - ".
        var groups = new System.Collections.Generic.Dictionary<string, System.Collections.Generic.List<string>>();
        foreach (var c in i.Contributors)
        {
            string key = c.RoleKey switch { "author" => "Credit_Author", "composer" => "Credit_Composer", _ => "Credit_Other" };
            if (!groups.TryGetValue(key, out var names)) groups[key] = names = new();
            foreach (var n in c.Names)
                if (!names.Contains(n)) names.Add(n);
        }
        foreach (var key in new[] { "Credit_Author", "Credit_Composer", "Credit_Other" })
            if (groups.TryGetValue(key, out var names) && names.Count > 0) Row(_credits, key, string.Join(" - ", names));
        _creditsSection.Visible = _credits.Children.Count > 0;
    }

    private static void Row(Grid g, string label, string value)
    {
        if (value.Length == 0) return;
        Row(g, label, Ui.Text(value, wrap: true));
    }

    private static void Row(Grid g, string label, UIElement value)
    {
        int r = g.RowDefinitions.Count;
        g.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        var name = Ui.Text(Loc.S(label), TextKind.SecondaryBody);
        Grid.SetRow(name, r);
        Grid.SetRow((FrameworkElement)value, r);
        Grid.SetColumn((FrameworkElement)value, 1);
        g.Children.Add(name);
        g.Children.Add(value);
    }

    // One name per line, opening its page where it has one.
    private static UIElement Links(IEnumerable<(string Name, System.Action? Open)> items)
    {
        var panel = Ui.Stack(0);
        foreach (var (name, open) in items)
        {
            FrameworkElement e = open == null ? Ui.Text(name) : Ui.Link(name, open, value: true);
            e.HorizontalAlignment = HorizontalAlignment.Left;
            panel.Children.Add(e);
        }
        return panel;
    }
}
