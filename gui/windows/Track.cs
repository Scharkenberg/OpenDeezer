// The track model, deserialized from the wire. gui/windows.tests links this file.

using System.Text.Json.Serialization;

namespace PurpleDisc;

// Public because Track instances are ListView items. A class, not a record: lists
// find rows by reference, and the same track can be queued twice.
public sealed class Track
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
    public string ArtistId { get; init; } = "";
    public string ArtistLine { get; init; } = "";
    public string AlbumName { get; init; } = "";
    // "" for a track saved before tracks carried it (queue and history files).
    public string AlbumId { get; init; } = "";
    public string ArtworkUrl { get; init; } = "";
    public long DurationMs { get; init; }

    [JsonPropertyName("explicit")]
    public bool IsExplicit { get; init; }

    // Deezer streams it nowhere and has no other version of it: greyed out and stepped over.
    [JsonPropertyName("unavailable")]
    public bool Unavailable { get; init; }

    // Plays through DZPlayEpisode and skips like, add to playlist and preload. Never on the wire.
    [JsonIgnore]
    public bool IsEpisode { get; init; }
}
