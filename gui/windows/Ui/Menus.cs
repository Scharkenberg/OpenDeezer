// Context menus for tracks, episodes, albums and playlists, and the commands behind them. Queue
// additions confirm with the toasts of Deezer's app.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

// Where a track menu opens. PlaylistId: the own playlist listing the track (adds "Remove from this
// playlist"); Queue: a queue row, Index its row (adds "Remove from queue"); Bar: the player bar's
// menu (add to playlist, radio, download); Page: the track's own page (no Like, Radio or details).
internal sealed record TrackMenu(string PlaylistId = "", bool Queue = false, bool Bar = false, bool Page = false)
{
    public int Index { get; init; } = -1;
}

internal static class Menus
{
    private static Player Player => App.Player;
    private static Library Library => App.Library;
    private static Session Session => App.Session;

    public static void Track(IList<MenuFlyoutItemBase> items, Track t, TrackMenu context)
    {
        bool row = !context.Bar;
        Group(items,
            row ? Item("Menu_PlayNext", Icons.PlayNext, () => QueueTrack(t, next: true)) : null,
            row ? Item("Menu_AddToQueue", Icons.Queue, () => QueueTrack(t, next: false)) : null);
        if (t.IsEpisode)
        {
            Episode(items, t, context);
            return;
        }
        bool liked = Player.IsLiked(t.Id);
        Group(items,
            row && !context.Page ? Net(Item(liked ? "Menu_Unlike" : "Menu_Like", liked ? Icons.HeartFill : Icons.Heart, () => Player.Like(t, !liked))) : null,
            Net(AddToPlaylist(t)),
            context.PlaylistId.Length > 0 ? Net(Item("Menu_RemoveFromPlaylist", Icons.Remove, () => RemoveFromPlaylist(context.PlaylistId, t))) : null,
            context.Queue ? Item("Queue_Remove", Icons.Remove, () => Player.RemoveAt(context.Index)) : null);
        Group(items,
            context.Page ? null : Net(Item("Menu_TrackDetails", Icons.Info, () => TrackView.Open(t))),
            row && t.ArtistId.Length > 0 ? Net(Item("Menu_GoToArtist", Icons.Person, () => ArtistView.Open(t.ArtistId))) : null,
            context.Page ? null : Net(Item("Menu_StartRadio", Icons.Flow, () => StartRadio(t))));
        Group(items,
            Pin(new SearchItem { Type = "Track", Id = t.Id, Title = t.Name, Subtitle = t.ArtistLine, Picture = t.ArtworkUrl }),
            Item("Menu_CopyLink", Icons.Share, () => CopyLink("track", t.Id)),
            Net(Item("Ban_Track", Icons.Remove, () => DontRecommend("track", t.Id))),
            context.Bar && t.ArtistId.Length > 0 ? Net(Item("Ban_Artist", Icons.Remove, () => DontRecommend("artist", t.ArtistId))) : null);
        Group(items,
            Net(Disabled(Item("Menu_Download", Icons.Download, () => Download(t)), !Session.CanDownload, "Menu_DownloadRequiresPremium")),
            OfflineItem(Offline.TrackKind, t.Id, App.Offline.IsPinned(t.Id)));
    }

    // In the web player's order: mark played or not, share, the description.
    private static void Episode(IList<MenuFlyoutItemBase> items, Track t, TrackMenu context)
    {
        bool heard = Episodes.Mark(t.Id) is { Heard: true };
        Group(items,
            Net(Item(heard ? "Episode_MarkUnplayed" : "Episode_MarkPlayed", heard ? Icons.Remove : Icons.Check, () => Episodes.SetPlayed(t, !heard))),
            Item("Menu_CopyLink", Icons.Share, () => CopyLink("episode", t.Id)),
            Episodes.Info(t) is { Description.Length: > 0 } ep ? Item("Episode_LearnMore", Icons.Info, () => _ = Dialogs.Message(t.Name, ep.Description)) : null,
            context.Queue ? Item("Queue_Remove", Icons.Remove, () => Player.RemoveAt(context.Index)) : null);
    }

    // Copies the item's deezer.com link (kind: track, album, playlist, artist, show, episode), with
    // the toast Deezer's apps show.
    public static void CopyLink(string kind, string id)
    {
        var data = new Windows.ApplicationModel.DataTransfer.DataPackage();
        data.SetText($"https://www.deezer.com/{kind}/{id}");
        Windows.ApplicationModel.DataTransfer.Clipboard.SetContent(data);
        Toast.Info(Loc.S("Link_Copied_" + kind), "");
    }

    // "Don't recommend": kind "track" or "artist". The playing track, or one by that artist, is
    // skipped, as Deezer's apps do.
    public static async void DontRecommend(string kind, string id)
    {
        bool ok = await Task.Run(() => DeezerCore.DZSetRecommendationBan(kind, id, 1) != 0);
        if (ok) SkipBanned(kind, id);
        if (ok) Toast.Info(Loc.S("Ban_Done_" + kind), "");
        else Toast.Error(Loc.S("Error_Generic"), "");
    }

    // After a ban: the playing track, or one by that artist, is skipped; with nothing after it,
    // playback stops.
    internal static void SkipBanned(string kind, string id)
    {
        if (Player.Current is not { } cur || (kind == "track" ? cur.Id : cur.ArtistId) != id) return;
        if (Player.HasNext) Player.Next();
        else Player.Stop();
    }

    // A mix, a list or the queue kept as a new playlist, named by the user.
    public static async void SaveAsPlaylist(string suggested, IReadOnlyList<Track> tracks)
    {
        string name = await Dialogs.Prompt(Loc.S("Dialog_SaveAsPlaylistTitle"), Loc.S("Dialog_PlaylistNamePlaceholder"), suggested);
        if (name.Length == 0) return;
        if (await Library.CreatePlaylistAsync(name, tracks) == null) Toast.Error(Loc.S("Dialog_CreateFailTitle"), Loc.S("Dialog_CreateFailBody"));
        else Toast.Success(Loc.Format("Dialog_SavedAsPlaylistFormat", name), "");
    }

    // Title, description, private or collaborative (not both), as the web player edits them.
    public static async void EditPlaylist(string id)
    {
        var d = await Task.Run(() => DeezerCore.PlaylistDetails(id));
        if (d.Error.Length > 0)
        {
            Toast.Error(Loc.S("Playlist_EditFailTitle"), d.Error);
            return;
        }
        var title = new TextBox { Header = Loc.S("Playlist_Title"), Text = d.Title };
        var description = new TextBox { Header = Loc.S("Playlist_Description"), Text = d.Description, AcceptsReturn = true, TextWrapping = Microsoft.UI.Xaml.TextWrapping.Wrap };
        var isPrivate = new ToggleSwitch { Header = Loc.S("Playlist_Private"), IsOn = d.IsPrivate };
        var collaborative = new ToggleSwitch { Header = Loc.S("Playlist_Collaborative"), IsOn = d.IsCollaborative };
        isPrivate.Toggled += (_, _) => { if (isPrivate.IsOn) collaborative.IsOn = false; };
        collaborative.Toggled += (_, _) => { if (collaborative.IsOn) isPrivate.IsOn = false; };
        var form = Ui.Stack(Space.M, title, description, isPrivate, collaborative);
        form.Width = Size.FlyoutWidth;
        if (!await Dialogs.Form(Loc.S("Dialog_EditPlaylistTitle"), form, Loc.S("Btn_Save"))) return;
        var change = new System.Text.Json.Nodes.JsonObject();
        if (title.Text.Trim() is { Length: > 0 } t && t != d.Title) change["title"] = t;
        if (description.Text != d.Description) change["description"] = description.Text;
        if (isPrivate.IsOn != d.IsPrivate) change["isPrivate"] = isPrivate.IsOn;
        if (collaborative.IsOn != d.IsCollaborative) change["isCollaborative"] = collaborative.IsOn;
        if (change.Count == 0) return;
        string json = change.ToJsonString();
        if (!await Task.Run(() => DeezerCore.DZUpdatePlaylist(id, json) != 0))
        {
            Toast.Error(Loc.S("Playlist_EditFailTitle"), "");
            return;
        }
        if (change["title"] is { } newTitle) Library.RenamedPlaylist(id, (string)newTitle!);
    }

    private static void QueueTrack(Track t, bool next)
    {
        if (next) Player.PlayNext(t);
        else Player.Enqueue(t);
        Toast.Show(InfoBarSeverity.Informational, Loc.S(next ? "Queue_TrackNext" : "Queue_TrackAdded"), "",
            next ? null : OpenQueueButton("Btn_Open"));
    }

    // An album's, playlist's or podcast's menu, the same on its tile and its page: Play next and Add
    // to queue (none for an empty list) | Favorite (when the kept lists know), Available offline |
    // Go to artist | Pin, Copy link | Edit, Delete for one's own playlist.
    public static void Collection(IList<MenuFlyoutItemBase> items, Collection c, int trackCount = -1)
    {
        string kind = c.Kind switch { CollectionKind.Album => "album", CollectionKind.Playlist => "playlist", CollectionKind.Podcast => "podcast", _ => "" };
        if (kind.Length == 0) return;
        bool own = kind == "playlist" && Library.IsOwn(c.OwnerId);
        if (trackCount != 0)
            Group(items,
                Item("Menu_PlayNext", Icons.PlayNext, () => QueueCollection(c, next: true)),
                Item("Menu_AddToQueue", Icons.Queue, () => QueueCollection(c, next: false)));
        Group(items,
            own ? null : Favorite(kind, c.Id),
            kind == "podcast" ? null : OfflineItem(kind, c.Id, App.Offline.IsPinned(kind, c.Id)));
        Group(items, c.ArtistId.Length > 0 ? Net(Item("Menu_GoToArtist", Icons.Person, () => ArtistView.Open(c.ArtistId))) : null);
        Group(items, Pin(new SearchItem { Type = c.Kind.ToString(), Id = c.Id, Title = c.Title, Subtitle = c.Owner, Picture = c.ArtUrl }), Item("Menu_CopyLink", Icons.Share, () => CopyLink(kind == "podcast" ? "show" : kind, c.Id)));
        if (own) Group(items, Net(Item("Playlist_Edit", Icons.Rename, () => EditPlaylist(c.Id))), Net(Item("Menu_Delete", Icons.Delete, () => Delete(c))));
    }

    // An artist's menu, on its tile and its page.
    public static void Artist(IList<MenuFlyoutItemBase> items, ArtistInfo a)
    {
        string id = a.Id;
        Group(items, Favorite("artist", id));
        Group(items,
            // No pin without a name (an artist page whose profile is not read yet).
            a.Name.Length > 0 ? Pin(new SearchItem { Type = "Artist", Id = id, Title = a.Name, Picture = a.ArtworkUrl }) : null,
            Item("Menu_CopyLink", Icons.Share, () => CopyLink("artist", id)),
            Net(Item("Ban_Artist", Icons.Remove, () => DontRecommend("artist", id))));
    }

    // A More button whose menu `fill` makes each time it opens.
    public static Button MoreButton(Action<IList<MenuFlyoutItemBase>> fill)
    {
        var more = Ui.IconButton(Icons.More, "Tooltip_More", null);
        var menu = new MenuFlyout();
        menu.Opening += (_, _) =>
        {
            menu.Items.Clear();
            fill(menu.Items);
        };
        more.Flyout = menu;
        return more;
    }

    // In offline mode an item that needs Deezer is disabled, not hidden: a menu has the same items
    // online and offline.
    private static T Net<T>(T item) where T : MenuFlyoutItemBase
    {
        if (App.Offline.Mode) item.IsEnabled = false;
        return item;
    }

    // Add to or Remove from favorites, offered only when the kept lists know which.
    private static MenuFlyoutItem? Favorite(string kind, string id)
    {
        if (Library.IsFavorite(kind, id) is not { } on) return null;
        return Net(Item(on ? "Menu_RemoveFavorite" : "Menu_AddFavorite", on ? Icons.HeartFill : Icons.Heart, async () =>
        {
            if (!await Library.SetFavoriteAsync(kind, id, !on)) Toast.Error(Loc.S("Like_FailTitle"), Loc.S("Like_FailBody"));
        }));
    }

    // Pin to Home, or Unpin from Home when it is there (Type as Deezer names it: "Album", ...).
    public static MenuFlyoutItem Pin(SearchItem item)
    {
        bool pinned = Library.IsPinned(item.Type, item.Id);
        return Net(Item(pinned ? "Pin_Remove" : "Pin_Add", Icons.Pinned, async () =>
        {
            int r = await Library.SetPinnedAsync(item, !pinned);
            if (r == 1) Toast.Info(Loc.S(pinned ? "Pin_Removed" : "Pin_Added"), "");
            else Toast.Error(Loc.S(r == 2 ? "Pin_Full" : "Error_Generic"), "");
        }));
    }

    private static async void QueueCollection(Collection c, bool next)
    {
        var tracks = c.TracksOf(await Library.GetAsync<Reply>(c.Key));
        if (tracks.Count == 0)
        {
            Toast.Error(Loc.S("Queue_AddFailed"), "");
            return;
        }
        if (next) Player.PlayNext(tracks, c.Listen);
        else Player.Enqueue(tracks, c.Listen);
        bool album = c.Kind == CollectionKind.Album;
        string key = "Queue_" + c.Kind + (next ? "Next" : "Added");
        // As Deezer's app: an album offers the queue, a playlist does not.
        var open = !album ? null : OpenQueueButton(next ? "Btn_OpenQueue" : "Btn_Open");
        Toast.Show(InfoBarSeverity.Informational, Loc.S(key), "", open);
    }

    private static Button OpenQueueButton(string key) => Ui.Button(Loc.S(key), () =>
    {
        Toast.Hide();
        QueuePane.RequestOpen();
    });

    // "Available offline", ticked while it is (a track also through a pinned album or playlist,
    // which unticking it leaves pinned); the same plans that download may pin.
    private static ToggleMenuFlyoutItem OfflineItem(string kind, string id, bool pinned)
    {
        var item = new ToggleMenuFlyoutItem { Text = Loc.S("Offline_Available"), Icon = Ui.Icon(Icons.Pinned), IsChecked = pinned };
        item.Click += (_, _) => SetOffline(kind, id, item.IsChecked);
        // Not again while its last pin or unpin is on the way.
        item.IsEnabled = !App.Offline.Busy(kind, id);
        return Disabled(item, !Session.CanDownload, "Menu_DownloadRequiresPremium");
    }

    public static async void SetOffline(string kind, string id, bool on)
    {
        string err = await App.Offline.SetPinnedAsync(kind, id, on);
        if (err.Length > 0) Toast.Error(Loc.S("Offline_FailTitle"), err);
    }

    // Adds the non-null items, a separator before them when the menu already has items.
    public static void Group(IList<MenuFlyoutItemBase> items, params MenuFlyoutItemBase?[] group)
    {
        bool first = true;
        foreach (var item in group)
        {
            if (item == null) continue;
            if (first && items.Count > 0) items.Add(new MenuFlyoutSeparator());
            first = false;
            items.Add(item);
        }
    }

    private static T Disabled<T>(T item, bool disabled, string reasonKey) where T : MenuFlyoutItemBase
    {
        if (!disabled) return item;
        item.IsEnabled = false;
        ToolTipService.SetToolTip(item, Loc.S(reasonKey));
        AutomationProperties.SetHelpText(item, Loc.S(reasonKey));
        return item;
    }

    public static MenuFlyoutItem Item(string key, string? glyph, Action onClick)
    {
        var item = new MenuFlyoutItem { Text = Loc.S(key), Icon = glyph == null ? null : Ui.Icon(glyph) };
        item.Click += (_, _) => onClick();
        return item;
    }

    private static MenuFlyoutSubItem AddToPlaylist(Track t)
    {
        var sub = new MenuFlyoutSubItem { Text = Loc.S("Menu_AddToPlaylist"), Icon = Ui.Icon(Icons.Add) };
        sub.Items.Add(Item("Menu_NewPlaylist", Icons.Add, () => AddToNewPlaylist(t)));
        // Only the account's own: Deezer refuses a track added to someone else's.
        var own = Library.Playlists.Where(p => Library.IsOwn(p.OwnerId)).ToList();
        if (own.Count > 0) sub.Items.Add(new MenuFlyoutSeparator());
        foreach (var p in own)
        {
            var item = new MenuFlyoutItem { Text = p.Name };
            item.Click += (_, _) => AddTo(p.Id, t);
            sub.Items.Add(item);
        }
        return sub;
    }

    public static async void NewPlaylist() => await CreatePlaylist();

    private static async void AddToNewPlaylist(Track t)
    {
        if (await CreatePlaylist() is { } created) AddTo(created.Id, t);
    }

    private static async Task<Playlist?> CreatePlaylist()
    {
        string name = await Dialogs.Prompt(Loc.S("Dialog_NewPlaylistTitle"), Loc.S("Dialog_PlaylistNamePlaceholder"), "");
        if (name.Length == 0) return null;
        var created = await Library.CreatePlaylistAsync(name);
        if (created == null) Toast.Error(Loc.S("Dialog_CreateFailTitle"), Loc.S("Dialog_CreateFailBody"));
        return created;
    }

    private static async void AddTo(string playlistId, Track t)
    {
        if (!await Library.AddToPlaylistAsync(playlistId, t)) Toast.Error(Loc.S("Dialog_AddFailTitle"), Loc.S("Dialog_AddFailBody"));
    }

    private static async void RemoveFromPlaylist(string playlistId, Track t)
    {
        if (!await Library.RemoveFromPlaylistAsync(playlistId, t)) Toast.Error(Loc.S("Menu_RemoveFromPlaylist"), Loc.S("Playlist_RemoveFailBody"));
    }

    private static async void Delete(Collection c)
    {
        if (!await Dialogs.Confirm(Loc.S("Dialog_DeletePlaylistTitle"), Loc.Format("Dialog_DeletePlaylistBodyFormat", c.Title), Loc.S("Btn_Delete"))) return;
        if (!await Library.DeletePlaylistAsync(c.Id)) Toast.Error(Loc.S("Dialog_DeleteFailTitle"), Loc.S("Dialog_DeleteFailBody"));
    }

    private static async void Download(Track t)
    {
        var reply = await Task.Run(() => DeezerCore.Download(t.Id, ""));
        if (reply.Path.Length > 0)
            Toast.Success(Loc.S("Dialog_DownloadDoneTitle"), Loc.Format("Dialog_DownloadDoneBodyFormat", reply.Path));
        else Toast.Error(Loc.S("Dialog_DownloadFailTitle"), reply.Error.Length > 0 ? reply.Error : Loc.S("Dialog_DownloadFailBody"));
    }

    public static async Task DownloadCollection(Collection c)
    {
        var reply = await Task.Run(() => c.Kind == CollectionKind.Album ? DeezerCore.DownloadAlbum(c.Id) : DeezerCore.DownloadPlaylist(c.Id));
        if (reply.Saved > 0)
            Toast.Success(Loc.S("Dialog_DownloadDoneTitle"), Loc.Format("Dialog_DownloadBatchDoneFormat", reply.Saved, reply.Failed, reply.Dir));
        else Toast.Error(Loc.S("Dialog_DownloadFailTitle"), reply.Error.Length == 0 ? Loc.S("Dialog_DownloadFailBody") : reply.Error);
    }

    public static async void PlayCollection(Collection c)
    {
        var reply = await Library.GetAsync<Reply>(c.Key);
        var tracks = c.TracksOf(reply);
        if (tracks.Count > 0) Player.Play(tracks, 0, c.Listen);
        else if (reply.Error.Length > 0) Toast.Error(Loc.S("Playback_FailTitle"), reply.Error);
    }

    public static async void StartRadio(Track t)
    {
        var mix = await Task.Run(() => DeezerCore.TrackMix(t.Id));
        PlayRadio(Loc.S("Radio_SongHeader"), mix, new("track_mix_contextual", t.Id));
    }

    public static async void StartArtistRadio(string artistId)
    {
        var mix = await Task.Run(() => DeezerCore.ArtistMix(artistId));
        PlayRadio(Loc.S("Radio_ArtistHeader"), mix, new("artist_smartradio", artistId));
    }

    // The engine's reason when it gave one, as for a mood.
    private static void PlayRadio(string title, TracksReply mix, ListenContext listen)
    {
        if (!Session.SignedIn) return;
        var tracks = mix.Tracks;
        if (tracks.Count == 0)
        {
            Toast.Error(Loc.S("Dialog_RadioFailTitle"), mix.Error.Length > 0 ? mix.Error : Loc.S("Dialog_RadioFailBody"));
            return;
        }
        CollectionView.Open(OpenDeezer.Collection.Of(title, tracks, listen));
        Player.Play(tracks, 0, listen);
    }
}
