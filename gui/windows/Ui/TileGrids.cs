// The tile grids of each kind, with the open, play and menu every page gives them. `open`
// replaces the opening (search records the pick first); `map` the tile's text.

using System;

namespace PurpleDisc;

internal static class TileGrids
{
    public static TileGrid<Album> Albums(Action<Album>? open = null) =>
        new(Tiles.Album, open ?? (a => CollectionView.Open(Collection.Of(a))), a => Menus.PlayCollection(Collection.Of(a)),
            (items, a) => Menus.Collection(items, Collection.Of(a)));

    public static TileGrid<Playlist> Playlists(Func<Playlist, TileData>? map = null, Action<Playlist>? open = null) =>
        new(map ?? Tiles.Playlist, open ?? (p => CollectionView.Open(Collection.Of(p))), p => Menus.PlayCollection(Collection.Of(p)),
            (items, p) => Menus.Collection(items, Collection.Of(p), p.TrackCount));

    public static TileGrid<ArtistInfo> Artists(Action<ArtistInfo>? open = null) =>
        new(Tiles.Artist, open ?? (a => ArtistView.Open(a.Id)), null, (items, a) => Menus.Artist(items, a));

    public static TileGrid<Podcast> Podcasts(Action<Podcast>? open = null) =>
        new(Tiles.Podcast, open ?? (p => CollectionView.Open(Collection.Of(p))), null, (items, p) => Menus.Collection(items, Collection.Of(p)));
}