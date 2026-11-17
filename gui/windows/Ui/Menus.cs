// Context menus for tracks and playlists, and the commands behind them.

using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;

namespace OpenDeezer;

// Where a track menu opens: an own playlist adds "Remove from this playlist", the queue
// "Remove from queue" (Index is the row); the player bar keeps add to playlist, radio and download.
internal sealed record TrackMenu(string PlaylistId = "", bool Queue = false, bool Bar = false)
{
    public int Index { get; init; } = -1;
}

internal static class Menus
{
    private static Player Player => App.Player;
    private static Library Library => App.Library;
    private static Session Session => App.Session;

    public static MenuFlyout Track(Track t, TrackMenu context)
    {
        var menu = new MenuFlyout();
        Track(menu.Items, t, context);
        return menu;
    }

    public static void Track(IList<MenuFlyoutItemBase> items, Track t, TrackMenu context)
    {
        bool row = !context.Bar;
        bool liked = Player.IsLiked(t.Id);
        Group(items,
            row ? Item("Menu_PlayNext", Icons.PlayNext, () => Player.PlayNext(t)) : null,
            row ? Item("Menu_AddToQueue", Icons.Queue, () => Player.Enqueue(t)) : null);
        Group(items,
            row ? Item(liked ? "Menu_Unlike" : "Menu_Like", liked ? Icons.HeartFill : Icons.Heart, () => Player.Like(t, !liked)) : null,
            AddToPlaylist(t),
            context.PlaylistId.Length > 0 ? Item("Menu_RemoveFromPlaylist", Icons.Remove, () => RemoveFromPlaylist(context.PlaylistId, t)) : null,
            context.Queue ? Item("Queue_Remove", Icons.Remove, () => Player.RemoveAt(context.Index)) : null);
        Group(items,
            t.IsEpisode ? null : Item("Menu_TrackDetails", Icons.Info, () => TrackView.Open(t)),
            row ? Disabled(Item("Menu_GoToArtist", Icons.Person, () => ArtistView.Open(t.ArtistId)), t.ArtistId.Length == 0, "Menu_NoArtistPage") : null,
            Item("Menu_StartRadio", Icons.Flow, () => StartRadio(t)));
        Group(items,
            Disabled(Item("Menu_Download", Icons.Download, () => Download(t)), !Session.CanDownload, "Menu_DownloadRequiresPremium"),
            t.IsEpisode ? null : OfflineItem(t));
    }

    // "Available offline", ticked while the track is (also through a pinned album or playlist,
    // which unticking it leaves pinned); the same plans that download may pin.
    private static ToggleMenuFlyoutItem OfflineItem(Track t)
    {
        var item = new ToggleMenuFlyoutItem { Text = Loc.S("Offline_Available"), Icon = Ui.Icon(Icons.Pinned), IsChecked = App.Offline.IsPinned(t.Id) };
        item.Click += (_, _) => SetOffline(Offline.TrackKind, t.Id, item.IsChecked);
        // Not again while its last pin or unpin is on the way.
        item.IsEnabled = !App.Offline.Busy(Offline.TrackKind, t.Id);
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

    public static MenuFlyout Playlist(Playlist p)
    {
        var menu = new MenuFlyout();
        menu.Items.Add(Item("Menu_Rename", Icons.Rename, () => Rename(p)));
        menu.Items.Add(Item("Menu_Delete", Icons.Delete, () => Delete(p)));
        return menu;
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
        if (Library.Playlists.Count > 0) sub.Items.Add(new MenuFlyoutSeparator());
        foreach (var p in Library.Playlists)
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

    private static async void Rename(Playlist p)
    {
        string name = await Dialogs.Prompt(Loc.S("Dialog_RenamePlaylistTitle"), Loc.S("Dialog_PlaylistNamePlaceholder"), p.Name);
        if (name.Length > 0 && !await Library.RenamePlaylistAsync(p, name))
            Toast.Error(Loc.S("Dialog_RenameFailTitle"), Loc.S("Dialog_RenameFailBody"));
    }

    private static async void Delete(Playlist p)
    {
        if (!await Dialogs.Confirm(Loc.S("Dialog_DeletePlaylistTitle"), Loc.Format("Dialog_DeletePlaylistBodyFormat", p.Name), Loc.S("Btn_Delete"))) return;
        if (!await Library.DeletePlaylistAsync(p)) Toast.Error(Loc.S("Dialog_DeleteFailTitle"), Loc.S("Dialog_DeleteFailBody"));
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
        if (tracks.Count > 0) Player.Play(tracks, 0);
        else if (reply.Error.Length > 0) Toast.Error(Loc.S("Playback_FailTitle"), reply.Error);
    }

    public static async void StartRadio(Track t)
    {
        var mix = await Task.Run(() => DeezerCore.TrackMix(t.Id));
        PlayRadio(Loc.S("Radio_SongHeader"), mix.Tracks);
    }

    public static async void StartArtistRadio(string artistId)
    {
        var mix = await Task.Run(() => DeezerCore.ArtistMix(artistId));
        PlayRadio(Loc.S("Radio_ArtistHeader"), mix.Tracks);
    }

    private static void PlayRadio(string title, IReadOnlyList<Track> tracks)
    {
        if (!Session.SignedIn) return;
        if (tracks.Count == 0)
        {
            Toast.Error(Loc.S("Dialog_RadioFailTitle"), Loc.S("Dialog_RadioFailBody"));
            return;
        }
        CollectionView.Open(Collection.Of(title, tracks));
        Player.Play(tracks, 0);
    }
}
