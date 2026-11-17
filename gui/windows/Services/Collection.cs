// What a collection page shows: liked songs, Flow, a playlist, an album, a podcast's
// episodes, or tracks fetched elsewhere (radio, search).

using System.Collections.Generic;

namespace OpenDeezer;

// Pinned: the tracks kept for offline use one by one.
internal enum CollectionKind { Liked, Flow, Playlist, Album, Podcast, Tracks, Pinned }

internal sealed record Collection(CollectionKind Kind, string Id, string Title, string Owner, string ArtUrl)
{
    public static readonly Collection Liked = new(CollectionKind.Liked, "", Loc.S("Nav_LikedSongs"), "", "");
    public static readonly Collection Flow = new(CollectionKind.Flow, "", Loc.S("Nav_Flow"), "", "");

    // Already fetched (radio, search): shown as is, not through the Library.
    public IReadOnlyList<Track>? Tracks { get; init; }

    public static Collection Of(Playlist p) => new(CollectionKind.Playlist, p.Id, p.Name, p.Owner, p.ArtworkUrl);
    public static Collection Of(Album a) => new(CollectionKind.Album, a.Id, a.Name, a.ArtistLine, a.ArtworkUrl);
    public static Collection Of(Podcast p) => new(CollectionKind.Podcast, p.Id, p.Name, "", p.ArtworkUrl);
    public static Collection Of(string title, IReadOnlyList<Track> tracks) => new(CollectionKind.Tracks, "", title, "", "") { Tracks = tracks };
    public static Collection Of(OfflineCollection o) => o.Kind switch
    {
        "album" => new(CollectionKind.Album, o.Id, o.Title, "", o.Cover),
        "playlist" => new(CollectionKind.Playlist, o.Id, o.Title, "", o.Cover),
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

    public string KindLabel => Kind switch
    {
        CollectionKind.Playlist => Loc.S("Kind_Playlist"),
        CollectionKind.Album => Loc.S("Kind_Album"),
        CollectionKind.Podcast => Loc.S("Kind_Podcast"),
        _ => "",
    };

    public IReadOnlyList<Track> TracksOf(Reply r)
    {
        if (r is TracksReply t) return t.Tracks;
        if (r is not EpisodesReply e) return [];
        var list = new List<Track>(e.Episodes.Count);
        foreach (var ep in e.Episodes)
            list.Add(new Track
            {
                Id = ep.Id,
                Name = ep.Title,
                ArtistLine = Title,
                AlbumName = Title,
                ArtworkUrl = ep.ArtworkUrl.Length > 0 ? ep.ArtworkUrl : ArtUrl,
                DurationMs = ep.DurationMs,
                IsEpisode = true,
            });
        return list;
    }
}
