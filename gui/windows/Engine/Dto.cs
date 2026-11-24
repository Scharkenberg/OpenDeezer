// Engine JSON responses (internal/bridge/dto.go), each deserialized once through WireJson.

using System;
using System.Collections.Generic;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Text.Json.Serialization.Metadata;

namespace OpenDeezer;

// Every object-shaped export answers {"error":"..."} on failure.
internal record Reply
{
    public string Error { get; init; } = "";
}

// DZLastErrorJSON: Network is set when the reason is a lost network, not the track.
internal sealed record LastErrorReply : Reply
{
    public bool Network { get; init; }
}

internal sealed record ArtistRef
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
}

internal sealed record Album
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
    public IReadOnlyList<ArtistRef> Artists { get; init; } = [];
    public string ArtworkUrl { get; init; } = "";

    [JsonIgnore]
    public string ArtistLine => Artists.Count > 0 ? Artists[0].Name : "";
}

internal sealed record Playlist
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
    public string Owner { get; init; } = "";
    public int TrackCount { get; init; }
    public string ArtworkUrl { get; init; } = "";
    public string OwnerId { get; init; } = "";
}

internal sealed record ArtistInfo
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
    public string ArtworkUrl { get; init; } = "";
    public long NbFans { get; init; }
}

// DZMediaCacheStatsJSON: what the stream cache holds; pinned (offline) bytes are outside the limit.
internal sealed record MediaCacheStats
{
    public long Bytes { get; init; }
    public long PinnedBytes { get; init; }
    public string Dir { get; init; } = "";
}

// DZTrackInfoJSON: everything known about one track, for its overview page.
internal sealed record TrackInfo : Reply
{
    public string Id { get; init; } = "";
    public string Title { get; init; } = "";
    public string Version { get; init; } = "";
    public IReadOnlyList<TrackArtist> Artists { get; init; } = [];
    public TrackAlbum Album { get; init; } = new();
    public int TrackNumber { get; init; }
    public int DiskNumber { get; init; }
    public long DurationMs { get; init; }
    public string ReleaseDate { get; init; } = "";
    public double Bpm { get; init; }
    public string Isrc { get; init; } = "";
    public bool Explicit { get; init; }
    public string Copyright { get; init; } = "";
    public IReadOnlyList<Contributor> Contributors { get; init; } = [];
}

internal sealed record TrackArtist
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
    public string Role { get; init; } = "";
}

internal sealed record TrackAlbum
{
    public string Id { get; init; } = "";
    public string Title { get; init; } = "";
    public string CoverUrl { get; init; } = "";
    public string ReleaseDate { get; init; } = "";
    public string Label { get; init; } = "";
    public int TrackCount { get; init; }
    public IReadOnlyList<string> Genres { get; init; } = [];
}

internal sealed record Contributor
{
    public string RoleKey { get; init; } = "";
    public IReadOnlyList<string> Names { get; init; } = [];
}

internal sealed record Podcast
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
    public string Description { get; init; } = "";
    public string ArtworkUrl { get; init; } = "";
    public int EpisodeCount { get; init; }
}

internal sealed record Episode
{
    public string Id { get; init; } = "";
    public string Title { get; init; } = "";
    public string Description { get; init; } = "";
    public string ArtworkUrl { get; init; } = "";
    public long DurationMs { get; init; }
    public string ReleaseDate { get; init; } = "";
}

// Where the account stopped in an episode (seconds) and whether it heard it (DZEpisodeBookmarksJSON).
internal sealed record EpisodeBookmark
{
    public int Offset { get; init; }
    public bool Heard { get; init; }
}

internal sealed record TracksReply : Reply
{
    public IReadOnlyList<Track> Tracks { get; init; } = [];
}

internal sealed record PlaylistsReply : Reply
{
    public IReadOnlyList<Playlist> Playlists { get; init; } = [];
}

internal sealed record CatalogReply : Reply
{
    public IReadOnlyList<Track> Tracks { get; init; } = [];
    public IReadOnlyList<Album> Albums { get; init; } = [];
    public IReadOnlyList<ArtistInfo> Artists { get; init; } = [];
    public IReadOnlyList<Playlist> Playlists { get; init; } = [];
}

internal sealed record HomeReply : Reply
{
    public IReadOnlyList<Track> TopTracks { get; init; } = [];
    public IReadOnlyList<Album> TopAlbums { get; init; } = [];
    public IReadOnlyList<Playlist> Playlists { get; init; } = [];
}

// Premium = false is Deezer Free. LoggedIn = false means the fetch failed (tier unknown).
internal sealed record Account : Reply
{
    public string UserId { get; init; } = "";
    public string Name { get; init; } = "";
    public string Offer { get; init; } = "";
    public string Picture { get; init; } = "";   // "" when the user has none
    public bool CanHq { get; init; }
    public bool CanHifi { get; init; }
    public bool Premium { get; init; }
    public bool LoggedIn { get; init; }
}

internal sealed record ArtistProfile : Reply
{
    public ArtistInfo Artist { get; init; } = new();
    public IReadOnlyList<Track> Top { get; init; } = [];
    public IReadOnlyList<Album> Albums { get; init; } = [];
    public IReadOnlyList<ArtistInfo> Related { get; init; } = [];
}

internal sealed record LyricLine
{
    public long TimeMs { get; init; }
    public string Text { get; init; } = "";
    // The line in the reader's language, where Deezer has it.
    public string Translated { get; init; } = "";
}

// DZLibraryJSON: the account's favorite albums, artists or podcasts (one of them per reply).
internal sealed record LibraryReply : Reply
{
    public IReadOnlyList<Album> Albums { get; init; } = [];
    public IReadOnlyList<ArtistInfo> Artists { get; init; } = [];
    public IReadOnlyList<Podcast> Podcasts { get; init; } = [];
}

// A search history entry or a search's best result: Type is Deezer's ("Track", "Album", "Artist",
// "Playlist", "Podcast", "PodcastEpisode", "Livestream").
internal sealed record SearchItem : Reply
{
    public string Type { get; init; } = "";
    public string Id { get; init; } = "";
    public string Title { get; init; } = "";
    public string Subtitle { get; init; } = "";
    public string Picture { get; init; } = "";
}

// DZPinnedJSON: what the account pinned to Home, in its order.
internal sealed record PinnedReply : Reply
{
    public IReadOnlyList<SearchItem> Items { get; init; } = [];
}

internal sealed record ExplicitContent : Reply
{
    public string Level { get; init; } = "";
    public IReadOnlyList<string> Available { get; init; } = [];
    public bool CanEdit { get; init; }
}

internal sealed record FlowMood
{
    public string Id { get; init; } = "";
    public string Title { get; init; } = "";
}

internal sealed record PlaylistDetails : Reply
{
    public string Id { get; init; } = "";
    public string Title { get; init; } = "";
    public string Description { get; init; } = "";
    public bool IsPrivate { get; init; }
    public bool IsCollaborative { get; init; }
    public string OwnerId { get; init; } = "";
}

internal sealed record Lyrics : Reply
{
    public string Plain { get; init; } = "";
    public IReadOnlyList<LyricLine> Synced { get; init; } = [];
    public bool IsSynced { get; init; }
}

internal sealed record PodcastsReply : Reply
{
    public IReadOnlyList<Podcast> Podcasts { get; init; } = [];
}

internal sealed record EpisodesReply : Reply
{
    public IReadOnlyList<Episode> Episodes { get; init; } = [];
}

internal sealed record AudioDevice
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
    public bool IsDefault { get; init; }
}

internal sealed record DevicesReply : Reply
{
    public IReadOnlyList<AudioDevice> Devices { get; init; } = [];
}

internal sealed record ConnectDevice
{
    public string Name { get; init; } = "";
    public string Addr { get; init; } = "";
    public string Client { get; init; } = "";
    public string Version { get; init; } = "";
}

// One DZHistoryRecentJSON row. Kind "episode" replays through DZPlayEpisode.
internal sealed record HistoryEntry
{
    public string TrackId { get; init; } = "";
    public string Title { get; init; } = "";
    public string Artist { get; init; } = "";
    public string Album { get; init; } = "";
    public string Kind { get; init; } = "";
    public string ArtworkUrl { get; init; } = "";
    public long DurationMs { get; init; }

    public Track ToTrack() => new()
    {
        Id = TrackId,
        Name = Title,
        ArtistLine = Artist,
        AlbumName = Album,
        ArtworkUrl = ArtworkUrl,
        DurationMs = DurationMs,
        IsEpisode = Kind == "episode",
    };
}

internal sealed record TrackStat
{
    public string TrackId { get; init; } = "";
    public string Title { get; init; } = "";
    public string Artist { get; init; } = "";
    public int Plays { get; init; }
}

internal sealed record ArtistStat
{
    public string Artist { get; init; } = "";
    public int Plays { get; init; }
}

internal sealed record HistoryStats : Reply
{
    public IReadOnlyList<TrackStat> TopTracks { get; init; } = [];
    public IReadOnlyList<ArtistStat> TopArtists { get; init; } = [];
    public long TotalSeconds { get; init; }
}

internal sealed record UpdateInfo : Reply
{
    public string Current { get; init; } = "";
    public string Latest { get; init; } = "";
    public bool HasUpdate { get; init; }
    public string Url { get; init; } = "";

    // The engine compares the release with its own version; the app uses its assembly version.
    public UpdateInfo ForApp(string appVersion)
    {
        if (appVersion.Length == 0) return this;
        var latest = ParseRelease(Latest);
        return this with { Current = appVersion, HasUpdate = latest != null ? latest > new Version(appVersion) : HasUpdate };
    }

    private static Version? ParseRelease(string tag)
    {
        string s = tag.Trim().TrimStart('v', 'V');
        int dash = s.IndexOfAny(['-', '+']);
        if (dash >= 0) s = s[..dash];
        return Version.TryParse(s, out var v) ? v : null;
    }
}

// The engine persists EQ in eq.json. Preset is "custom" after a hand edit; gains are -12..+12 dB.
internal sealed record EQState : Reply
{
    public bool Enabled { get; init; }
    public bool Mono { get; init; }
    public double PreampDb { get; init; }
    public IReadOnlyList<double> GainsDb { get; init; } = new double[10];
    public string Preset { get; init; } = "flat";
    public IReadOnlyList<double> Bands { get; init; } = new double[] { 31.5, 63, 125, 250, 500, 1000, 2000, 4000, 8000, 16000 };
    public IReadOnlyList<string> Presets { get; init; } = [];
}

internal sealed record QueueReply : Reply
{
    public long Version { get; init; } = -1;
    public int Index { get; init; } = -1;
    public IReadOnlyList<Track> Tracks { get; init; } = [];
}

internal sealed record CreatedReply : Reply
{
    public string Id { get; init; } = "";
}

internal sealed record SavedReply : Reply
{
    public string Path { get; init; } = "";
}

internal sealed record BatchReply : Reply
{
    public int Saved { get; init; }
    public int Failed { get; init; }
    public string Dir { get; init; } = "";
}

// A collection kept for offline use ("track" pins show as their own collection of one).
internal sealed record OfflineCollection
{
    public string Kind { get; init; } = "";
    public string Id { get; init; } = "";
    public string Title { get; init; } = "";
    // A local file once the cover is stored, else its CDN URL.
    public string Cover { get; init; } = "";
    public int TrackCount { get; init; }
    // Tracks whose body is stored.
    public int Fetched { get; init; }
}

internal sealed record OfflineCollectionsReply : Reply
{
    public IReadOnlyList<OfflineCollection> Collections { get; init; } = [];
}

// One pin being fetched; Error is why it stopped (a full disk, for one).
internal sealed record OfflineJob
{
    public string Kind { get; init; } = "";
    public string Id { get; init; } = "";
    public int Done { get; init; }
    public int Total { get; init; }
    public int Failed { get; init; }
    public string Error { get; init; } = "";
}

internal sealed record OfflineProgress : Reply
{
    public bool Active { get; init; }
    public IReadOnlyList<OfflineJob> Jobs { get; init; } = [];
}

internal sealed record OfflineIdsReply : Reply
{
    public IReadOnlyList<string> Ids { get; init; } = [];
}

internal sealed record ControlConfig : Reply
{
    public bool Enabled { get; init; }
    public string Addr { get; init; } = "";
    public string Token { get; init; } = "";
    public bool Lan { get; init; }
}

internal sealed record WebRemoteInfo : Reply
{
    public bool Enabled { get; init; }
    public string Code { get; init; } = "";
    public string Url { get; init; } = "";
}

// DZElsewhereJSON: what another session of the account plays. Deezer announces no pause, so it
// counts as playing there until UntilMs (Unix ms). Track is null when that session went on to a
// track it did not name.
internal sealed record Elsewhere
{
    public Track? Track { get; init; }
    public long UntilMs { get; init; }

    public bool PlayingAt(long unixMs) => unixMs < UntilMs;
}

[JsonSourceGenerationOptions(PropertyNamingPolicy = JsonKnownNamingPolicy.CamelCase)]
[JsonSerializable(typeof(Reply))]
[JsonSerializable(typeof(LastErrorReply))]
[JsonSerializable(typeof(MediaCacheStats))]
[JsonSerializable(typeof(Track))]
[JsonSerializable(typeof(TracksReply))]
[JsonSerializable(typeof(PlaylistsReply))]
[JsonSerializable(typeof(CatalogReply))]
[JsonSerializable(typeof(HomeReply))]
[JsonSerializable(typeof(Account))]
[JsonSerializable(typeof(ArtistProfile))]
[JsonSerializable(typeof(TrackInfo))]
[JsonSerializable(typeof(Lyrics))]
[JsonSerializable(typeof(PodcastsReply))]
[JsonSerializable(typeof(EpisodesReply))]
[JsonSerializable(typeof(DevicesReply))]
[JsonSerializable(typeof(ConnectDevice[]))]
[JsonSerializable(typeof(HistoryEntry[]))]
[JsonSerializable(typeof(string[]))]
[JsonSerializable(typeof(HistoryStats))]
[JsonSerializable(typeof(UpdateInfo))]
[JsonSerializable(typeof(EQState))]
[JsonSerializable(typeof(QueueReply))]
[JsonSerializable(typeof(CreatedReply))]
[JsonSerializable(typeof(SavedReply))]
[JsonSerializable(typeof(BatchReply))]
[JsonSerializable(typeof(OfflineCollectionsReply))]
[JsonSerializable(typeof(OfflineProgress))]
[JsonSerializable(typeof(OfflineIdsReply))]
[JsonSerializable(typeof(ControlConfig))]
[JsonSerializable(typeof(WebRemoteInfo))]
[JsonSerializable(typeof(Settings))]
[JsonSerializable(typeof(SavedQueue))]
[JsonSerializable(typeof(LibraryReply))]
[JsonSerializable(typeof(PinnedReply))]
[JsonSerializable(typeof(SearchItem))]
[JsonSerializable(typeof(SearchItem[]))]
[JsonSerializable(typeof(ExplicitContent))]
[JsonSerializable(typeof(FlowMood[]))]
[JsonSerializable(typeof(PlaylistDetails))]
[JsonSerializable(typeof(IReadOnlyList<string>))]
[JsonSerializable(typeof(Dictionary<string, EpisodeBookmark>))]
[JsonSerializable(typeof(Elsewhere))]
internal sealed partial class WireJson : JsonSerializerContext;

internal static class Dto
{
    // UTF-8 JSON as the engine wrote it. An empty or unreadable payload reads as an empty reply.
    public static T Read<T>(ReadOnlySpan<byte> json, JsonTypeInfo<T> type) where T : new()
    {
        if (json.Length == 0) return new T();
        try { return JsonSerializer.Deserialize(json, type) ?? new T(); }
        catch (JsonException ex)
        {
            AppLog.Write($"bad {typeof(T).Name} payload: {ex.Message}");
            return new T();
        }
    }

    // Bare-array exports answer an {"error"} object instead of the array on failure.
    public static (T[] Items, string Error) ReadArray<T>(ReadOnlySpan<byte> json, JsonTypeInfo<T[]> type)
    {
        var start = json.TrimStart(" \t\r\n"u8);
        if (start.Length == 0) return ([], "");
        if (start[0] == (byte)'{') return ([], Read(start, WireJson.Default.Reply).Error);
        try { return (JsonSerializer.Deserialize(start, type) ?? [], ""); }
        catch (JsonException ex)
        {
            AppLog.Write($"bad {typeof(T).Name} list: {ex.Message}");
            return ([], ex.Message);
        }
    }
}
