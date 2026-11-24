# To do: OpenDeezer vs Deezer for Android 9.0.28.1

Reviewed on 2026-11-23: the library (`internal`, `corelib`, `mobile`, `sdk`) and the Windows
GUI (`gui/windows`), against the decompiled Android app (jadx). APK paths are under
`sources/defpackage/` unless named; `strings.xml` numbers are lines.

## Evidence labels

- **CV**: code-verified. Both sides were read, and the reviewer and I both checked them.
- **M**: the code divergence is verified, but its effect needs a live measurement, which
  is named.
- **I**: inferred. Obfuscation leaves doubt, which is named.

## 1. Correctness: done (2026-11-23)

1. **Media URL errors.** Done. Top-level and 4xx errors are parsed. Licence codes
   1000–1002 renew the licence. Track-token codes 2000 and 2001 fetch a new token.
   Only 2002 and 2004 lead to the fallback version, then the preview, which comes
   from the media service (`PREVIEW`). Everything else is an error. `get_url`
   goes to the login's `URL_MEDIA`.
   - Measured: a bad licence answers HTTP 400 with code 1000; a bad track token
     answers code 2000; `PREVIEW` serves a plain MP3 preview.
2. **ReplayGain.** Done. The app's formula with the account's `GAIN.TARGET`
   (measured: -15 in `getUserData`). The target is saved in playback.json for
   offline starts, and the SDK exposes it.
3. **Listen reporting.** Done. Every local start reports, through one shared
   logger (`internal/listens`) in the engine and the TUI:
   - `params` of the listen that ended: seconds heard, pauses, seeks, format,
     cache, preview, context, shuffle and repeat;
   - `next_media` of the track that starts;
   - listens that could not be sent go in one `log.batch_listen`.

   Contexts use Deezer's page names. Hosts set them with
   `DZSetListenContext` / `SetListenContext`; Windows sets one per queued track.
   - Unknown: the Android payload's device key (obfuscated at runtime) is not
     sent.
4. **Windows lyrics.** Done. A page reopened on the same track loads again, and a
   failed load shows Retry. Measured in the harness.

## 2. Claimed features that fall short

5. **Flow and radios end.** **CV** Engine done 2026-11-23: the engine and the TUI top the queue up (≤ 4 left, new tracks only) for Flow, its moods and both mixes, from the listen context; Flow sends the account's tuner and a mood its `config_id`. The Android app names its lists too (phone and TV), so it gets the top-up.
   - Flow is one fetch (`Services/Library.cs:188`), and `Next()` stops at the end
     (`Player.Queue.cs:64-69`). Track and artist radios work the same way (`Ui/Menus.cs:183-205`).
   - The README calls Flow "endless". The app keeps adding tracks (`strings.xml` 3498,
     3500, 3491).
   - Do: fetch more and append when near the end. Also send `CONFIG_ID` and `tuner`
     with `radio_getUserRadio` (`IIIllllIIllI.java:53-65`), and a Flow context in listens.
6. **Windows doesn't resume the last track.** **CV** Done 2026-11-23 (local only): `queue.json` keeps the queue, position, shuffle and repeat; restored stopped at sign-in, and Play resumes there. The server copy (`KmpQsQueueState`) is not done.
   - Nothing is stored (`Services/Config.cs:15-42`), and the queue starts empty
     (`Player.cs:92-100`).
   - The README claims "resume the last track on launch", but only the TUI does it
     (`internal/ui/config.go:242-252`).
   - Do: save the queue, index and position, and restore them paused.
7. **Podcasts.** Done 2026-11-23 on Windows too (episode menu, date and progress per row, speed in the bar from 900 px, in More below). Engine: bookmarks read (gw `deezer.userMenu`), written every 30 s and on pause or track change, resume, mark played, and speed 0.5–2 for episodes. The app UI is step C.
   - No resume or progress sync. The app uses `episode.bookmarkSet {OFFSET, DURATION,
     ISHEARD}` (`lIIlIlIlIIIllI.java:199-203`) and `episode.bookmarkGetList`. **CV**
   - On Windows, episode rows have no menu (`Ui/TrackList.cs:236`). Description, date
     and podcast name are parsed but never shown (`Engine/Dto.cs:120-124`). **CV**
   - Missing:
     - playback speed (`strings.xml` 3319);
     - "end of episode" sleep (3931);
     - mark as played (730).
8. **Play from a trimmed list queues only the visible rows.** **CV** Done 2026-11-23.
   - `SetItems(tracks, limit)` keeps only `limit` rows (`Ui/TrackList.cs:148`), and a row
     plays `_items` (`:214`).
   - So Home and Charts queue 10, Search "All" 5, and the artist's top list 5. The
     artist hero queues all of them (`Views/Artist.cs:34`).
   - Do: keep the full list and play from it.
9. **History rows lose their length.** **CV** Done 2026-11-23.
   - The engine sends `durationMs` (`internal/history/history.go:50`).
   - `HistoryEntry`/`ToTrack` drops it (`Engine/Dto.cs:217-235`). Result: a blank
     duration, `DZPlay(id, 0)`, and a short "time left".
10. **Liked songs stop at 5000.** **CV** Done 2026-11-23: ids through `song.getFavoriteIds` (pages of 10 000, checksum), the list without a cap.
    - `maxTracks = 5000` (`internal/deezer/client.go:916, 1036`), so later likes show as
      unliked.
    - The app uses `NB=10000` (`IlIIllIlllIIll.java:79-81`). Do: page to the end.
11. **MP3 gapless keeps encoder delay and padding.** **I** Done 2026-11-23: Deezer's MP3s carry an Info frame with a LAME tag (valid CRC; delay 576, padding 1092–1452 measured), and the decoder now trims to the music.
    - go-mp3 ignores LAME/Xing/iTunSMPB (`internal/audio/mp3stream.go:40-53`).
    - ExoPlayer trims it (`lIIIllllllIIIll.java:44-49`).
    - Measure: do Deezer's MP3s carry the tags? Then trim them.
12. **The ad schedule is dead code.** Done 2026-11-23: removed, README corrected (no ads are played). **CV**
    - `AdBreakDue` and `AdCadenceInfo` have no caller (`internal/deezer/ads.go:63-113`).
    - The README says Free is "ad-supported, exactly like Deezer's own web player".
    - Do: delete the code, or wire it up. Fix the README either way.
13. **Lyrics come from gw `song.getLyrics`, which the app doesn't use.** **I** Done 2026-11-23: Pipe `GetLyrics` first (with translations), gw as the fallback.
    - The app uses Pipe `GetTrackLyricsDetail` (`IllllIlIIlll.java:62`).
    - Do: make Pipe the primary source and keep gw as the fallback.

## 3. Windows UX parity (all CV on the Windows side; app evidence is `strings.xml` lines)

14. Done 2026-11-23. Sorting, filtering and search for Liked songs and Playlists:
    - sort by recently added, A-Z track, A-Z artist (4040, 4033, 4031);
    - playlist filters "By you" / "By others" (1186, 1185);
    - "Search for a playlist" (2704).
15. Done 2026-11-23. Play next and Add to queue for albums and playlists (only Play today: `Views/Search.cs:18-19`,
    `Charts.cs:11,13`, `Artist.cs:12`), and a toast on every queue action (3504-3510).
16. Done 2026-11-23. Playlist editing: reorder tracks, description, collaborative (4050, 767). Today it is
    rename and delete only (`Ui/Menus.cs:91-97`).
17. Done 2026-11-23. Queue Clear: ask first (3481), and disable it when only the playing row is left
    (`Shell/QueuePane.cs:31,77`; `Player.Queue.cs:357`).
18. Done 2026-11-23. Credits are in English in every language: `Views/Track.cs:120` uses `Role` and never
    reads `RoleKey`. Map `RoleKey` to string keys.
19. Done 2026-11-23 (no custom value: the app has the strings but no code). Sleep timer: add 5, 10 and 20 min and a custom value (3929, 3924, 3926, 3930).
    Today: `Ui/Tokens.cs:221`.
20. Done 2026-11-23. Offline page: "Shuffle downloads" (3225), storage used (3639), and failed jobs
    (`OfflineJob.Failed` is never shown).
21. Done 2026-11-23. Search: history and "Best result" (3565, 3562). The box has no suggestions
    (`Shell/SearchBox.cs:16-47`).
22. Done 2026-11-23. SMTC shuffle and repeat (`Shell/MediaControls.cs:22-27`). The README implies them.

## 4. Smaller fixes and cleanup

23. Done 2026-11-23. After a reset (account switch), the tray tooltip and SMTC keep the old track. Both
    skip a null `Current` (`MainWindow.cs:113`, `MediaControls.cs:71`). **CV**
24. Done 2026-11-23 (the ids are a separate cheap read since A3). Liked songs are fetched twice at sign-in (`Session.cs:252` and `Library.cs:187`).
    Take the ids from the Liked reply. **CV** (cost **M**)
25. Done 2026-11-23. Accessibility. **CV**/**I**
    - Lyric lines seek on `Tapped` only (`Views/Lyrics.cs:95`), so a keyboard can't
      reach them.
    - These may have no accessible name: the EQ preset combo (`Equalizer.cs:72`) and
      the folder boxes (`SettingsParts.cs:132`).
26. Done 2026-11-23. Engine refusals are ignored. **CV**
    - `DZSetEQJSON` (`Equalizer.cs:141`).
    - `DZSetAudioDevice`: the device is saved even when the engine refuses it
      (`SettingsParts.cs:25-27`).
27. Done 2026-11-23. Rule violations. **CV**
    - Numbers outside Tokens.cs: `RemoteControl.cs:17` (":7654"), `:44` (16),
      `Config.cs:118` (32000).
    - DTO fields never read:
      - `HomeReply.TopAlbums`, `UpdateInfo.Notes`, `TrackInfo.Gain`;
      - `TrackArtist.PictureUrl`, `OfflineCollection.PinnedAt`;
      - `ControlConfig.Addr`/`Running`, `WebRemoteInfo.Port`, `MediaCacheStats.MaxBytes`.
28. Done 2026-11-23. Settings that are open when the account finishes loading keep "unknown tier"
    (`Views/Settings.cs:93-94`; no `AccountChanged` subscriber). **I** (timing)
29. Done 2026-11-23 as the web player does it: no ping of our own (the dial's TCP keepalive finds a dead peer), and `AnnouncePlay` writes with a 5 s deadline. The live socket has no keepalive ping. The app pings every 25 s
    (`llIlIIIIlIlIIII.java:94-95`). `AnnouncePlay` writes with no deadline under `liveMu`
    (`internal/deezer/live.go:173-175`). **I**
    - Measure: how often does `live: connection ended` appear in the log?
30. Offline copies are never re-validated. The app expires them with the access token
    (`IllIllIlIIllII.java:38`). This is a rights question: decide whether to follow it.
31. `docs/research/deezer-session-sync.md:25`: the `?version=` parameter is the web
    player's. The app connects with the token alone (`IllIlIIllIIlll.java:44`).

## 5. App features with no OpenDeezer counterpart (desktop-plausible)

- Favourite albums, artists, playlists and podcasts: Pipe `AddAlbumToFavorite`,
  `AddArtistToFavorite`, `AddPlaylistToFavorite` and `AddPodcastToFavorite`.
- Flow moods, genres and the tuner (`GetMyFlowMoods`, `IIIIIlIIlIIIIII.java:76`).
- Share and copy link (`strings.xml` 732-736, 4378).
- Go to album from a track: no album id reaches Windows.
- "Don't recommend this track or artist" (`BanTrackFromRecommendation`; `strings.xml` 596-597, 383).
- Explicit-content filter and hiding unavailable songs (3673, 3621, 3624).
- Lyrics translation and "Report a problem" (2730, 2736).
- Save a mix or the queue as a playlist (3957).
- Theme choice: light, dark or automatic (898).
- A separate download quality (579).
- Private listening (3636).
- "Manage / change my plan" link (3610, 4247).
- Pin to Home (741).
- Server-side search history (`GetMySearchHistory`) and listening history (`user.getSongsHistory`).
- Live radio stations (`livestream.getData`).
- Smart tracklists (`mobile.pageSmartTracklist`).

## Dropped after verification

- "Offline fetch polling reloads everything every second": wrong. `Services/Offline.cs:164-166`
  reloads only when a job's done count moves or the fetch ends, and the harness measured that.
- Mix parameters, download Range/If-Range resume and URL refresh, and offline pins
  refusing preview plans all match the app or are sound.
