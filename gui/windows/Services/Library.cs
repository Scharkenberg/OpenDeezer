// What several views share: Home, the user's playlists, liked tracks and ids, and the last
// opened collections. UI thread only.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;

namespace OpenDeezer;

internal sealed class Library
{
    public const string HomeKey = "home";
    public const string PlaylistsKey = "playlists";
    public const string LikedKey = "liked";
    public const string FlowKey = "flow";
    public const string ChartsKey = "charts";
    // The favorite albums, artists and podcasts: Key(LibraryKind, "album"), ...
    public const string LibraryKind = "library";
    // What the account pinned to Home.
    public const string PinnedKey = "pinned";

    private static readonly TimeSpan MaxAge = TimeSpan.FromMinutes(Timing.LibraryMaxAgeMinutes);

    private sealed class Entry(Reply value, DateTime fetchedAt)
    {
        public Reply Value = value;
        public readonly DateTime FetchedAt = fetchedAt;
        public bool Refreshing;
    }

    private readonly Dictionary<string, Entry> _entries = new();
    // Collections (kind:id keys) in use order, most recent first, and the same for lyrics. Other keys are kept.
    private readonly LinkedList<string> _recent = new(), _recentOther = new();
    private readonly Dictionary<string, Task<Reply>> _inFlight = new();
    private readonly Dictionary<string, bool> _likesSinceSeed = new();
    // Every stored track by id, kept with the entries, for Enrich.
    private readonly Dictionary<string, Track> _known = new();
    private HashSet<string> _likedIds = new();
    private int _session;

    public event Action<string>? Changed;
    public event Action<string, Reply>? Stored;

    public static string Key(string kind, string id) => kind + ":" + id;

    public HomeReply? Home => Peek<HomeReply>(HomeKey);
    // Home carries the same playlists until the full list is loaded.
    public IReadOnlyList<Playlist> Playlists => Peek<PlaylistsReply>(PlaylistsKey)?.Playlists ?? Home?.Playlists ?? [];
    public IReadOnlyList<Track>? LikedTracks => Peek<TracksReply>(LikedKey)?.Tracks;

    public bool IsLiked(string id) => _likedIds.Contains(id);

    // A playlist is the account's own when its owner is the signed-in user.
    public bool IsOwn(string ownerId) => ownerId.Length > 0 && App.Session.AccountKnown && ownerId == App.Session.Account.UserId;

    // The owner of one of the user's playlists, for a page opened from where the owner is not named.
    public string OwnerOf(string playlistId) => Playlists.FirstOrDefault(p => p.Id == playlistId)?.OwnerId ?? "";

    // The cached value at once (refreshed behind it when old), else a fetch; failures are never cached.
    public async Task<T> GetAsync<T>(string key, CancellationToken ct = default) where T : Reply
    {
        if (_entries.TryGetValue(key, out var e))
        {
            Touch(key);
            if (DateTime.UtcNow - e.FetchedAt > MaxAge) RefreshInBackground(key, e);
            return (T)e.Value;
        }
        return (T)await FetchAsync(key).WaitAsync(ct);
    }

    public T? Peek<T>(string key) where T : Reply => _entries.TryGetValue(key, out var e) ? (T)e.Value : null;

    // Swaps each track for a cached copy with the same id, which carries artwork, album and duration.
    public List<Track> Enrich(IReadOnlyList<Track> tracks)
    {
        var result = new List<Track>(tracks.Count);
        foreach (var t in tracks) result.Add(!t.IsEpisode && _known.TryGetValue(t.Id, out var k) ? k : t);
        return result;
    }

    private static IReadOnlyList<Track> TracksOf(Reply r) => r switch
    {
        TracksReply t => t.Tracks,
        HomeReply h => h.TopTracks,
        CatalogReply c => c.Tracks,
        ArtistProfile a => a.Top,
        _ => [],
    };

    private void Index(Reply r)
    {
        foreach (var t in TracksOf(r)) _known[t.Id] = t;
    }

    // Drops a reply that has left `_entries` from the index: each of its tracks still indexed goes,
    // and is put back from another cached reply that has the same id.
    private void Unindex(Reply r)
    {
        HashSet<string>? dropped = null;
        foreach (var t in TracksOf(r))
            if (_known.TryGetValue(t.Id, out var k) && ReferenceEquals(k, t) && _known.Remove(t.Id)) (dropped ??= new()).Add(t.Id);
        if (dropped == null) return;
        foreach (var e in _entries.Values)
            foreach (var t in TracksOf(e.Value))
                if (dropped.Contains(t.Id)) _known.TryAdd(t.Id, t);
    }

    internal void Remove(string key)
    {
        if (_entries.Remove(key, out var e)) Unindex(e.Value);
        _recent.Remove(key);
        _recentOther.Remove(key);
    }

    // Drops every collection of these kinds, and forgets their fetches under way; a page showing one
    // fetches it again, others at their next visit.
    internal void Drop(params string[] kinds)
    {
        foreach (var key in _inFlight.Keys.Where(k => kinds.Contains(Split(k).Kind)).ToList()) _inFlight.Remove(key);
        foreach (var key in _entries.Keys.Where(k => kinds.Contains(Split(k).Kind)).ToList())
        {
            Remove(key);
            Changed?.Invoke(key);
        }
    }

    public void Clear()
    {
        _session++;
        _known.Clear();
        _entries.Clear();
        _recent.Clear();
        _recentOther.Clear();
        _inFlight.Clear();
        _likesSinceSeed.Clear();
        _likedIds = new();
    }

    // A value from an earlier run, shown until the fetch in flight replaces it.
    public void Seed(string key, Reply value)
    {
        Store(key, value, DateTime.MinValue);
        Changed?.Invoke(key);
    }

    // Drops a value seeded from an earlier run that no fetch has replaced yet.
    public void Unseed(string key)
    {
        if (!_entries.TryGetValue(key, out var e) || e.FetchedAt != DateTime.MinValue) return;
        Remove(key);
        Changed?.Invoke(key);
    }

    private Task<Reply> FetchAsync(string key)
    {
        if (!_inFlight.TryGetValue(key, out var task))
        {
            task = FetchAndStore(key);
            _inFlight[key] = task;
            Forget(key, task);
        }
        return task;
    }

    // A fetch leaves the list once it ended, unless a drop let a newer one take its key.
    private async void Forget(string key, Task<Reply> task)
    {
        await Task.WhenAny(task);
        if (_inFlight.TryGetValue(key, out var t) && t == task) _inFlight.Remove(key);
    }

    private async Task<Reply> FetchAndStore(string key)
    {
        int session = _session;
        // Album and playlist replies differ between offline mode and online (pinned lists, online lists).
        bool offline = App.Offline.Mode;
        var reply = await Task.Run(() => FetchReply(key));
        if (session != _session || reply.Error.Length > 0) return reply;
        if (offline != App.Offline.Mode && Split(key).Kind is "album" or "playlist") return reply;
        Store(key, reply, DateTime.UtcNow);
        Stored?.Invoke(key, reply);
        if (key == LikedKey)
            foreach (var t in ((TracksReply)reply).Tracks) _likedIds.Add(t.Id);
        return reply;
    }

    // A key's kind and id ("home" has no id).
    public static (string Kind, string Id) Split(string key)
    {
        int colon = key.IndexOf(':');
        return colon < 0 ? (key, "") : (key[..colon], key[(colon + 1)..]);
    }

    // The engine read behind a fetch (the harness holds it).
    internal Func<string, Reply> FetchReply = Fetch;

    // In offline mode collections come from what is pinned.
    private static Reply Fetch(string key)
    {
        var (kind, id) = Split(key);
        bool offline = App.Offline.Mode;
        return kind switch
        {
            HomeKey => DeezerCore.Home(),
            PlaylistsKey => DeezerCore.Playlists(),
            LikedKey => DeezerCore.Favorites(),
            FlowKey => DeezerCore.Flow(),
            ChartsKey => DeezerCore.Charts(),
            "playlist" or "album" when offline => DeezerCore.OfflineTracks(kind, id),
            Offline.TrackKind => DeezerCore.OfflineTracks(kind, id),
            "playlist" => DeezerCore.PlaylistTracks(id),
            "album" => DeezerCore.AlbumTracks(id),
            "podcast" => DeezerCore.PodcastEpisodes(id),
            "artist" => DeezerCore.ArtistProfile(id),
            "lyrics" => DeezerCore.Lyrics(id),
            LibraryKind => DeezerCore.LibraryList(id),
            PinnedKey => DeezerCore.Pinned(),
            _ => throw new ArgumentException("unknown library key " + key),
        };
    }

    private async void RefreshInBackground(string key, Entry e)
    {
        if (e.Refreshing) return;
        e.Refreshing = true;
        var reply = await FetchAsync(key);
        e.Refreshing = false;
        if (reply.Error.Length == 0 && _entries.ContainsKey(key)) Changed?.Invoke(key);
    }

    internal void Store(string key, Reply value, DateTime fetchedAt)
    {
        _entries.TryGetValue(key, out var old);
        _entries[key] = new Entry(value, fetchedAt);
        if (old != null) Unindex(old.Value);
        Index(value);
        Touch(key);
    }

    private void Touch(string key)
    {
        if (!key.Contains(':')) return;
        // Lyrics are kept in a list of their own: they never push a collection out.
        if (_entries[key].Value is Lyrics)
        {
            _recentOther.Remove(key);
            _recentOther.AddFirst(key);
            while (_recentOther.Count > Count.CachedCollections) Remove(_recentOther.Last!.Value);
            return;
        }
        _recent.Remove(key);
        _recent.AddFirst(key);
        int tracks = 0;
        foreach (var k in _recent) tracks += TracksOf(_entries[k].Value).Count;
        // The most recent one always stays.
        while (_recent.Count > 1 && (_recent.Count > Count.CachedCollections || tracks > Count.CachedTracks))
        {
            string last = _recent.Last!.Value;
            tracks -= TracksOf(_entries[last].Value).Count;
            Remove(last);
        }
    }

    private void Edit<T>(string key, Func<T, T> edit) where T : Reply
    {
        if (!_entries.TryGetValue(key, out var e)) return;
        var old = e.Value;
        e.Value = edit((T)old);
        Unindex(old);
        Index(e.Value);
        Changed?.Invoke(key);
    }

    // Seeds the liked ids at sign-in. Likes made while it runs are kept.
    public async Task RefreshLikedIdsAsync()
    {
        _likesSinceSeed.Clear();
        int session = _session;
        var (ids, error) = await Task.Run(DeezerCore.FavoriteIds);
        if (session != _session || error.Length > 0) return;
        var set = new HashSet<string>(ids);
        foreach (var (id, liked) in _likesSinceSeed)
        {
            if (liked) set.Add(id);
            else set.Remove(id);
        }
        _likedIds = set;
    }

    // Applied at once and rolled back when Deezer refuses it; false then.
    public async Task<bool> SetLikedAsync(Track t, bool liked)
    {
        string id = t.Id;
        ApplyLike(t, liked);
        _likesSinceSeed[id] = liked;
        int session = _session;
        bool ok = await Task.Run(() => (liked ? DeezerCore.DZAddFavorite(id) : DeezerCore.DZRemoveFavorite(id)) != 0);
        if (ok || session != _session) return ok;
        _likesSinceSeed.Remove(id);
        ApplyLike(t, !liked);
        return false;
    }

    private void ApplyLike(Track t, bool liked)
    {
        if (liked) _likedIds.Add(t.Id);
        else _likedIds.Remove(t.Id);
        var tracks = LikedTracks;
        if (tracks == null) return;
        int i = FindById(tracks, t.Id);
        if (liked == (i >= 0)) return;
        var list = new List<Track>(tracks);
        if (liked) list.Insert(0, t);
        else list.RemoveAt(i);
        Edit<TracksReply>(LikedKey, r => r with { Tracks = list });
    }

    private static int FindById(IReadOnlyList<Track> tracks, string id)
    {
        for (int i = 0; i < tracks.Count; i++)
            if (tracks[i].Id == id) return i;
        return -1;
    }

    // `tracks`: the tracks it starts with (a mix or the queue saved), none for an empty one.
    public async Task<Playlist?> CreatePlaylistAsync(string name, IReadOnlyList<Track>? tracks = null)
    {
        int session = _session;
        var ids = tracks?.Where(t => !t.IsEpisode && t.Id.Length > 0).Select(t => t.Id).ToList();
        var reply = await Task.Run(() => DeezerCore.CreatePlaylist(name, ids));
        if (reply.Id.Length == 0) return null;
        var p = new Playlist { Id = reply.Id, Name = name, OwnerId = App.Session.Account.UserId };
        if (session == _session) EditPlaylists(list => list.Insert(0, p));
        return p;
    }

    // Whether an album, artist, playlist or podcast is a favorite, from the kept lists; null when
    // they are not loaded.
    public bool? IsFavorite(string kind, string id)
    {
        if (kind == "playlist") return Peek<PlaylistsReply>(PlaylistsKey) is { } p ? p.Playlists.Any(x => x.Id == id) : null;
        if (Peek<LibraryReply>(Key(LibraryKind, kind)) is not { } r) return null;
        return r.Albums.Any(a => a.Id == id) || r.Artists.Any(a => a.Id == id) || r.Podcasts.Any(p => p.Id == id);
    }

    // Adds or removes a favorite.
    public async Task<bool> SetFavoriteAsync(string kind, string id, bool on)
    {
        int session = _session;
        bool ok = await Task.Run(() => DeezerCore.DZSetFavorite(kind, id, on ? 1 : 0) != 0);
        if (ok && session == _session) KeepFavorite(kind, id, on);
        return ok;
    }

    // A removed favorite leaves the kept list at once; an added one drops the list, so its page
    // reads it again. Either way the list's page hears it.
    internal void KeepFavorite(string kind, string id, bool on)
    {
        string key = kind == "playlist" ? PlaylistsKey : Key(LibraryKind, kind);
        if (!on && kind == "playlist") EditPlaylists(list => list.RemoveAll(p => p.Id == id));
        else if (!on) Edit<LibraryReply>(key, r => r with
        {
            Albums = r.Albums.Where(a => a.Id != id).ToList(),
            Artists = r.Artists.Where(a => a.Id != id).ToList(),
            Podcasts = r.Podcasts.Where(p => p.Id != id).ToList(),
        });
        else
        {
            Remove(key);
            Changed?.Invoke(key);
        }
    }

    public bool IsPinned(string type, string id) => Peek<PinnedReply>(PinnedKey)?.Items.Any(i => i.Type == type && i.Id == id) == true;

    // Pins an item to Home or unpins it: 1 done, 2 Home is full, 0 refused.
    public async Task<int> SetPinnedAsync(SearchItem item, bool on)
    {
        int r = await Task.Run(() => DeezerCore.DZSetPinned(item.Type, item.Id, on ? 1 : 0));
        if (r == 1) KeepPinned(item, on);
        return r;
    }

    // The kept pins with the item added (with the title and picture its caller knew) or removed, so
    // the menus and Home show it at once; Home reads them again at its next visit (kept as old).
    internal void KeepPinned(SearchItem item, bool on)
    {
        if (Peek<PinnedReply>(PinnedKey) is not { } kept) return;
        var items = kept.Items.Where(i => i.Type != item.Type || i.Id != item.Id).ToList();
        if (on) items.Add(item);
        Seed(PinnedKey, kept with { Items = items });
    }

    // A playlist renamed elsewhere (its edit dialog): the lists show the new name.
    public void RenamedPlaylist(string id, string name) => EditPlaylist(id, x => x with { Name = name });

    // A playlist's tracks changed order: its next visit fetches them again.
    public void Reordered(string playlistId) => Remove(Key("playlist", playlistId));

    public async Task<bool> DeletePlaylistAsync(string id)
    {
        int session = _session;
        bool ok = await Task.Run(() => DeezerCore.DZDeletePlaylist(id) != 0);
        if (!ok || session != _session) return ok;
        EditPlaylists(list => list.RemoveAll(x => x.Id == id));
        Remove(Key("playlist", id));
        return true;
    }

    public async Task<bool> AddToPlaylistAsync(string playlistId, Track t)
    {
        int session = _session;
        bool ok = await Task.Run(() => DeezerCore.DZAddToPlaylist(playlistId, t.Id) != 0);
        if (!ok || session != _session) return ok;
        EditPlaylist(playlistId, x => x with { TrackCount = x.TrackCount + 1 });
        Edit<TracksReply>(Key("playlist", playlistId), r => r with { Tracks = new List<Track>(r.Tracks) { t } });
        return true;
    }

    public async Task<bool> RemoveFromPlaylistAsync(string playlistId, Track t)
    {
        int session = _session;
        bool ok = await Task.Run(() => DeezerCore.DZRemoveFromPlaylist(playlistId, t.Id) != 0);
        if (!ok || session != _session) return ok;
        EditPlaylist(playlistId, x => x with { TrackCount = Math.Max(0, x.TrackCount - 1) });
        Edit<TracksReply>(Key("playlist", playlistId), r =>
        {
            var list = new List<Track>(r.Tracks);
            list.Remove(t);
            return r with { Tracks = list };
        });
        return true;
    }

    private void EditPlaylist(string id, Func<Playlist, Playlist> edit) =>
        EditPlaylists(list =>
        {
            int i = list.FindIndex(x => x.Id == id);
            if (i >= 0) list[i] = edit(list[i]);
        });

    // The user's playlists appear in both the playlists list and Home.
    private void EditPlaylists(Action<List<Playlist>> edit)
    {
        List<Playlist> Edited(IReadOnlyList<Playlist> playlists)
        {
            var list = new List<Playlist>(playlists);
            edit(list);
            return list;
        }
        Edit<PlaylistsReply>(PlaylistsKey, r => r with { Playlists = Edited(r.Playlists) });
        Edit<HomeReply>(HomeKey, r => r with { Playlists = Edited(r.Playlists) });
    }
}
