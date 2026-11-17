// The track model, deserialised from the wire. gui/windows.tests links this file.

using System.Text.Json.Serialization;

namespace OpenDeezer;

// Public because Track instances are ListView items. A class, not a record: lists
// find rows by reference, and the same track can be queued twice.
public sealed class Track
{
    public string Id { get; init; } = "";
    public string Name { get; init; } = "";
    public string ArtistId { get; init; } = "";
    public string ArtistLine { get; init; } = "";
    public string AlbumName { get; init; } = "";
    public string ArtworkUrl { get; init; } = "";
    public long DurationMs { get; init; }

    [JsonPropertyName("explicit")]
    public bool IsExplicit { get; init; }

    // Plays through DZPlayEpisode and skips like, add to playlist and preload. Never on the wire.
    [JsonIgnore]
    public bool IsEpisode { get; init; }
}
