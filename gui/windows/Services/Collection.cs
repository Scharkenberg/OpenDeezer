// What a collection page shows: liked songs, Flow, a playlist, an album, a podcast's
// episodes, or tracks fetched elsewhere (radio, search).

using System.Collections.Generic;
using System.Runtime.CompilerServices;

namespace OpenDeezer;

// The list a play comes from, as Deezer's players name it in their listen log: a page type and
// the id of what it shows (DZSetListenContext).
internal sealed record ListenContext(string Type, string Id)
{
    public static readonly ListenContext None = new("", "");
}

// Pinned: the tracks kept for offline use one by one.
internal enum CollectionKind { Liked, Flow, Playlist, Album, Podcast, Tracks, Pinned }

internal sealed record Collection(CollectionKind Kind, string Id, string Title, string Owner, string ArtUrl)
{
    public static readonly Collection Liked = new(CollectionKind.Liked, "", Loc.S("Nav_LikedSongs"), "", "");
    public static readonly Collection Flow = new(CollectionKind.Flow, "", Loc.S("Nav_Flow"), "", "");

    // Already fetched (radio, search): shown as is, not through the Library, with the list they came from.
    public IReadOnlyList<Track>? Tracks { get; init; }
    // A playlist's owner, the account's user id when it is the account's own.
    public string OwnerId { get; init; } = "";
    // An album's first artist, whose page its name opens.
    public string ArtistId { get; init; } = "";
    private ListenContext TracksListen { get; init; } = ListenContext.None;

    public static Collection Of(Playlist p) => new(CollectionKind.Playlist, p.Id, p.Name, p.Owner, p.ArtworkUrl) { OwnerId = p.OwnerId };
    public static Collection Of(Album a) => new(CollectionKind.Album, a.Id, a.Name, a.ArtistLine, a.ArtworkUrl) { ArtistId = a.Artists.Count > 0 ? a.Artists[0].Id : "" };
    public static Collection Of(Podcast p) => new(CollectionKind.Podcast, p.Id, p.Name, "", p.ArtworkUrl);
    public static Collection Of(string title, IReadOnlyList<Track> tracks, ListenContext listen) =>
        new(CollectionKind.Tracks, "", title, "", "") { Tracks = tracks, TracksListen = listen };
    public static Collection Of(OfflineCollection o) => o.Kind switch
    {
        "album" => new(CollectionKind.Album, o.Id, o.Title, "", o.Cover),
        "playlist" => new(CollectionKind.Playlist, o.Id, o.Title, "", o.Cover) { OwnerId = App.Library.OwnerOf(o.Id) },
        _ => new(CollectionKind.Pinned, "", Loc.S("Offline_Songs"), "", o.Cover),
    };

    public string Key => Kind switch
    {
        CollectionKind.Liked => Library.LikedKey,
        CollectionKind.Flow => Library.FlowKey,
        CollectionKind.Playlist => Library.Key("playlist", Id),
        CollectionKind.Album => Library.Key("album", Id),
        CollectionKind.Podcast => Library.Key("podcast", Id),
        CollectionKind.Pinned => Library.Key(Offline.TrackKind, ""),
        _ => "",
    };

    // The list as Deezer's listen log names it.
    public ListenContext Listen => Kind switch
    {
        CollectionKind.Liked => new("profile_loved", App.Session.Account.UserId),
        CollectionKind.Flow => new("dynamic_page_user_radio", App.Session.Account.UserId),
        CollectionKind.Playlist => new("playlist_page", Id),
        CollectionKind.Album => new("album_page", Id),
        CollectionKind.Podcast => new("show_page", Id),
        CollectionKind.Pinned => new("downloads_page", ""),
        _ => TracksListen,
    };

    // Album, Playlist and Podcast are named as Deezer names them; the other kinds have no label.
    public string KindLabel => SearchView.KindLabel(Kind.ToString());

    // The tracks made from a reply's episodes, once per reply.
    private static readonly ConditionalWeakTable<EpisodesReply, IReadOnlyList<Track>> s_episodeTracks = new();

    public IReadOnlyList<Track> TracksOf(Reply r) => r switch
    {
        TracksReply t => t.Tracks,
        EpisodesReply e => s_episodeTracks.GetValue(e, EpisodeTracks),
        _ => [],
    };

    private IReadOnlyList<Track> EpisodeTracks(EpisodesReply e)
    {
        var list = new List<Track>(e.Episodes.Count);
        foreach (var ep in e.Episodes)
        {
            var track = new Track
            {
                Id = ep.Id,
                Name = ep.Title,
                ArtistLine = Title,
                AlbumName = Title,
                ArtworkUrl = ep.ArtworkUrl.Length > 0 ? ep.ArtworkUrl : ArtUrl,
                DurationMs = ep.DurationMs,
                IsEpisode = true,
            };
            Episodes.Note(track, ep);
            list.Add(track);
        }
        return list;
    }
}
