# Plan: the remaining to-do items in three steps

Status on 2026-11-23: items 1–4 of `apk-parity-todo.md` are done. This plan
covers everything else in three steps; each lands as one change.

The protocol and behaviour below come from Deezer's web player (bundles
fetched 2026-11-19/23) and the Android app 9.0.28.1, decompiled. Two
reviewers extracted them and I spot-checked the key ones myself.

OpenDeezer presents itself as the web player (`deezer-sessions.md`). So where
the clients differ, it sends what the **web** player sends. The Android form
is noted only where the web player has nothing.

## Labels and rules

- **✔**: the literal was checked in the source by me.
- **R**: from a reviewer's report, quoted literal with location, not
  re-checked.
- **M**: needs a live measurement before or while implementing.

Every item follows the repo's rules:
- the failing test comes first (Go tests on loopback);
- any layout claim is measured in the harness;
- every number lives in `Tokens.cs`;
- every new string is translated into the 7 languages;
- Go files stay CRLF and are checked with gofmt on copies.

---

## Step A: engine and Deezer protocol (Go: `internal/deezer`, `internal/engine`, `corelib`, `mobile`)

Comes first; steps B and C build on its exports.

**Status 2026-11-23: done, except A8 (the owner decides) and the Pipe form
of A6's playlist creation.** What the work measured or decided differently:

- **A1:** the tuner is Pipe's `discoveryTuner` (`"DEFAULT"`, `"DISCOVERY"`)
  in lowercase; gw echoes it as `tuner`. Mood ids are `chill`,
  `motivation`, `focus`, `party`, `melancholy` and `you_and_me`, with titles
  from Pipe `FlowTunerHeader`. The mood listen context is
  `dynamic_page_flow_config` with the mood id. The engine takes the source
  from the listen context instead of a queue source kind, so hosts need no
  new call (`deezer.RadioFill`, shared with the TUI). The Android app (phone
  and TV) now names each list it plays from, as Windows does.
- **A2:** bookmarks come from gw `deezer.userMenu` (`EPISODE_BOOKMARKS.data[]
  {EPISODE_ID, OFFSET, BOOKMARK_TIME, IS_HEARD:"1"}`), not from
  `getUserData`. The exports are `DZEpisodeBookmarksJSON`,
  `DZMarkEpisodePlayed`, `DZSetPlaybackSpeed` and `DZPlaybackSpeed`. Speed
  runs plain MP3 streams through the resampler, so pitch is not kept.
- **A5:** `DZLibraryJSON(kind)` (`albums`, `artists`, `podcasts`) and
  `DZSetFavorite`.
- **A6:** the web bundle no longer has gw `playlist.create`; it uses Pipe
  `CreatePlaylist` and `AddTracksToPlaylist`. Their input fields cannot be
  checked without creating a playlist on the owner's account, so playlist
  creation stays on gw until they can be.
- **A7:** as the web player: no ping of its own, TCP keepalive from the
  dialer, and a 5 s write deadline on `AnnouncePlay`.
- **A9:** measured on three tracks: an `Info` frame with a LAME tag (CRC
  valid), delay 576, padding 1092–1452. go-mp3 decodes the Info frame as
  1152 silent samples. The decoder skips 1152 + delay + 529 samples and ends
  after frames·1152 − delay − padding.

### A0. Pipe client (new: `internal/deezer/pipe.go`)

Prerequisite for A4, A5, A6, A10 and C.

- **JWT:** get one with `POST https://auth.deezer.com/login/arl?jo=p&rto=c&i=c`
  and the session's cookies (measured 2026-11-23: `{jwt, refresh_token}`).
- **Renewal:** on expiry, `login/renew` with the refresh token (measured to
  work without the arl). The JWT's `exp` minus 15 s is the renewal point, as
  the web player's `requestDelay: 15` ✔.
- **Calls:** `POST https://pipe.deezer.com/api` with
  `{operationName, query, variables}` and `Authorization: Bearer <jwt>`, plus
  the web headers (`webHeaders`).
- **Query texts:** web op texts, kept as constants in one file (R,
  `3802.js`).
- **Errors:** GraphQL `errors[]` becomes a Go error; HTTP 401 renews once.
- **Tests:** a fake auth and Pipe server (loopback). It checks the JWT is
  reused until expiry, renewed after it, and the 401 retry happens.

### A1. Endless Flow, mixes and radios (todo 5, Flow moods)

- **Flow:** `radio.getUserRadio {user_id, tuner}` ✔. A mood/genre Flow sends
  `{config_id, user_id, tuner}` ✔.
- **Tuner:** from Pipe `query Tuner { me { id flowTuner { discoveryTuner } } }`
  (R). Values `"default"` / `"discovery"` (R, Android enum). **M**: what the
  web player sends.
- **Track mix:** `song.getSearchTrackMix {sng_id, start_with_input_track}` (R).
- **Artist radio:** `smart.getSmartRadio {art_id}` (R).
- **Refill:** when `trackList.length − trackListIndex <= 4`, fetch again with
  the same call and append ✔. Drop tracks already in the queue, by `SNG_ID`
  ✔. If nothing new comes, stop (R).
- **Engine:** `Queue` gets a source kind (Flow, mix, artist radio, with its
  id or tuner).
  - It refills itself when it owns the queue (`engineQ`).
  - For host-owned queues (Windows), new exports: `DZQueueSourceSet(json)` and
    `DZQueueNeedsMore()`. The host appends through `DZQueueAppend`; the engine
    tells the host to append with a version bump.
- **Listen context:** radios get `type:1` (done in step 1-4).
- **Moods:** Pipe `GetMyFlowMoods` / web `FlowConfig` (R) feed C-step UI. The
  engine only exposes them: `DZFlowMoodsJSON`.
- **Tests:** a queue near its end triggers exactly one fetch. Duplicates are
  dropped. A no-new-tracks reply stops refilling.

### A2. Podcast progress (todo 7, engine part)

- **Write:** `episode.bookmarkSet {EPISODE_ID, OFFSET (s), DURATION (s),
  ISHEARD}`, with no USER_ID on the web (R).
- **When to write:**
  - every 30 s of media time (Android, R);
  - on pause, stop and track change (R);
  - **M**: the web cadence was not found.
- **Read:** `deezer.getUserData` → `EPISODE_BOOKMARKS.data[] {EPISODE_ID,
  OFFSET, BOOKMARK_TIME, IS_HEARD}` (R, web).
- **States:** "played" when `pos >= 0.9·dur && dur − pos <= 120 s` ✔ (both
  clients). "Not played" when `pos < 10 s` (R, web).
- **Mark played/unplayed:** `episode.markAsPlayed` / `episode.markAsNotPlayed
  {EPISODE_ID}` ✔.
- **Resume:** `PlayEpisode` starts at the bookmark's offset. A played episode
  starts from 0.
- **Speed:** the player gets a playback rate. The Android cycle is 0.5, 0.75,
  1.0, 1.25, 1.5, 1.75, 2.0 ✔, for episodes only. The resampler takes it, and
  pitch is not kept.
  - **M**: whether a time-stretch is needed for speech quality. Decide after
    listening.
- **Exports:** `DZEpisodeBookmarksJSON`, `DZMarkEpisodePlayed(id, played)`,
  `DZSetPlaybackRate`.
- **Tests:** bookmark writes on the 30 s ticks and on pause, the played rule
  at its edges, and resume from the offset.

### A3. Liked songs beyond 5000 (todo 10)

- `song.getFavoriteIds {checksum, start, nb: 10000}` ✔ (`batchSize:1e4`).
- Then `ceil((total − start − nb)/nb)` more pages; stop on "Not modified"
  (R).
- Keep the checksum, so a later sign-in that sees "Not modified" reuses the
  cached ids.
- Remove the `maxTracks` cap for favourites. Tracks with metadata come
  through `song.getListData` in pages, as today.
- **Tests:** 23 000 liked ids over 3 pages; a "Not modified" reply reuses the
  cache.

### A4. Lyrics from Pipe (todo 13)

- Web `GetLyrics($trackId)` with `SynchronizedLines { lrcTimestamp line
  lineTranslated milliseconds duration }` and `SynchronizedWordByWordLines`
  (R).
- gw `song.getLyrics` stays as the fallback if Pipe fails.
- The `Lyrics` wire DTO gains `translated` per line (shown in step C) and
  `copyright` / `writers`.
- **Test:** the Pipe reply maps to the same `Lyrics`, and a Pipe error falls
  back to gw.

### A5. Favourite albums, artists, playlists, podcasts (app feature)

- **Add/remove:** Pipe `AddAlbumToFavorite` / `RemoveAlbumFromFavorite`,
  `AddArtistFavorite` / `RemoveArtistFromFavorite`, `AddPlaylistToFavorite` /
  …, `AddPodcastToFavorite` / … (R, web names). The `Add…` mutations of
  albums, artists and podcasts ✔ exist.
- **Lists:** `me{userFavorites{rawAlbums{id favoritedAt}}}` etc. (R), then
  metadata through the existing REST/gw lookups.
- **Exports:** `DZFavoriteJSON(kind)` and `DZSetFavorite(kind, id, on)`.
- **Library guard:** these change the account's library. Tests run against
  fakes only; nothing is tried on the owner's account.

### A6. Recommendations, playlists from a queue, search history, explicit filter (app features)

- **Don't recommend:** `BanTrackFromRecommendation($trackId)` /
  `BanArtistFromRecommendation($artistId)` and the Unban pairs (R, web).
- **Save a mix or the queue as a playlist:** `CreatePlaylist($input:{title,
  description?, isPrivate, isCollaborative})`, then
  `AddTracksToPlaylist($input:{playlistId, trackIds})` (R).
- **Search history:**
  - read: Pipe `History($first,$cursor)` (R);
  - add after a picked result: `add<Type>InSearchSuccessResult(<type>Id)`
    (R);
  - clear: `clearSearchSuccessResult` ✔.
- **Explicit filter:** `user.updateExplicitContentLevel
  {EXPLICIT_CONTENT_LEVEL}`, values `explicit_display` /
  `explicit_no_recommendation` / `explicit_hide` ✔. Read from user data (R).
- **Dropped, not found in either client:** private listening, go to album
  from a track.

### A7. Live socket keepalive (todo 29)

- WebSocket PING frames every 25 s, as Android's `relive_websocket_ping` (R).
  The web player sends none; it checks every 10 s and reconnects (R).
- Do both:
  - ping every 25 s, and drop the connection when no pong comes within 10 s;
  - give `AnnouncePlay` a 5 s write deadline.
- **M**: count `live: connection ended` before and after.

### A8. Offline copies and the licence (todo 30)

- The web player refuses to play once `now − USER.OPTIONS.timestamp > 14 days`
  (1 209 600 s ✔) and no fresh `deezer.getUserData` succeeds.
- Do the same for offline pins: after 14 days without a successful login,
  pinned tracks stop playing until one succeeds. Store the last good
  `OPTIONS.timestamp`.
- **Decision for the owner:** this is a rights rule that limits offline use.

### A9. MP3 gapless (todo 11)

- **M first:** decrypt one MP3_128 and one MP3_320 and look for a Xing/Info
  LAME header with delay and padding.
- If present: trim `delay + 529` samples at the start and `padding` at the end
  (as ExoPlayer's GaplessInfoHolder, R).
- No server field exists (R).

### A10. Audio ads for Free accounts (todo 12)

- The web player plays VAST/Triton audio ads for Free:
  - `deezer.adConfig` gives the cadence (default `{interval:2, start:0}`);
  - `deezer.getAudiobreak {consent_string, listening_context, track_list,
    player_track_id, player_content_type, player_content_id, focus, preroll,
    vastFormat, CONSENT_OBJECT}` ✔ gets the ad;
  - play is blocked while an ad is due (R).
- **Decision for the owner:**
  - (a) implement VAST playback and consent;
  - (b) delete the dead cadence code and fix the README, which wrongly claims
    "ad-supported like the web player".

  Decided 2026-11-23: **(b)**, done.

---

## Step B: Windows playback and queue (needs step A's exports)

**Status 2026-11-23: done, but for B1's optional server copy.** What changed on the way:

- **B1:** `queue.json` beside `settings.json`, for the account that saved it. The queue is restored
  stopped at the saved position. A seek before Play moves that position; the first Play starts
  there. An episode resumes from its bookmark instead.
- **B2:** the speed button shows from the medium width (900 px) up. Below that it lives in More:
  measured, the button kept the bar from narrowing below 738 px, so the narrow layout never
  engaged. "Share" copies the episode's deezer.com link.
- **B3:** Add to queue places tracks after the last ones added that way, ahead of the rest of the
  list, as the web player does; Play next places them right after the playing track.
- **B7:** the storage line uses the existing size format (MB below 1 GB, then GB).

### B1. Resume at launch (todo 6)

- **Local:** save queue, index, position, repeat and shuffle on change and at
  exit (`settings.json` or `queue.json`). Restore them **paused** (no
  play/pause field exists in either client's state, R).
- **Server (optional, behind a setting):**
  - read `KmpQsQueueState`; push with `KmpQsPushQueueState` (full text R,
    persisted query hashes R);
  - the newer of local and server wins (both clients, R);
  - pushes on `QueueChanged`, `SeekApplied` and `PlayPauseChange` (R).
  - **M**: debounce, as the web player's isn't known. Start with 2 s.

### B2. Podcast UI (todo 7)

- **Episode menu**, in the web order ✔: Mark as played / unplayed, Share,
  Learn more (the description).
- **Row:** date, duration, and a progress bar from the bookmark.
- **Speed button** in the player bar for episodes: cycles 0.5→2.0.

### B3. Queue actions and toasts (todos 15, 17)

- **Albums and playlists:** add Play next and Add to queue.
  - Play next inserts after the current track ✔ (web `splice(index+1)`).
  - Add to queue inserts after the last manually added item (R).
  - On playlists, offer them only when `NB_SONG != 0` (R).
- **Toasts** (Android texts, R):

  | Action | Toast | Button |
  |---|---|---|
  | Track added | "Track added to queue." | Open |
  | Track next | "This track will play next." | — |
  | Album added | "Album added to queue." | Open |
  | Album next | "This album will play next." | Open queue |
  | Playlist added | "Playlist added to queue." | — |
  | Playlist next | "This playlist will play next." | — |

  Failure: "Unable to add to queue".
- **Clear queue:**
  - confirm with "This will clear all the tracks in your queue.", buttons
    "Clear all" / "Cancel" (R);
  - disable Clear when only the playing row is left.

### B4. Sleep timer (todo 19)

- Presets, exactly as Android offers them ✔: 5, 10, 20, 30, 45, 60 min, Turn
  off.
- Android lists end-of-track and end-of-episode but never offers them ✔.
  OpenDeezer keeps end of track (its own feature) and adds end of episode for
  episodes.
- Toasts "Timer set!" / "Timer off!" (R).
- Custom duration: strings exist but no code uses them (R): not added.

### B5. Media keys and SMTC (todo 22)

- Shuffle and repeat through `ShuffleEnabled` / `AutoRepeatMode` with their
  change requests. Android's session also offers like and ban (R).
- On a player reset, clear SMTC and the tray tooltip (todo 23).

### B6. Lists and history (todos 8, 9)

- `TrackList` keeps the full list and shows `limit` rows; a row plays from
  the full list.
- `HistoryEntry` reads `durationMs`.

### B7. Offline page (todo 20)

- Storage line: −1 → "… MB", < 1024 → "%1$s MB", else "%.1f GB" (R, Android
  format).
- A "Shuffle downloads" action (string R).
- Failed fetches listed with Retry.
- **Tests:** harness checks for B2–B7 layouts at 480 and 1000 px.

---

## Step C: Windows library, search and cleanup (needs A0, A5, A6)

**Status 2026-11-23: done.** Measured read-only on the way (Pipe
introspection and the web player's queries):

- `PlaylistUpdateMutationInput` is `{playlistId!, title, description, isPrivate,
  isCollaborative, picture}`, and `PlaylistCreateMutationInput` is `{title!, description,
  isPrivate!, isCollaborative!, picture}`. Item queries take `playlist(playlistId)`,
  `album(albumId)`, `artist(artistId)`, `podcast(podcastId)` and `track(trackId)`, each with
  `isFavorite`.
- The search history fragments name each kind's fields. `instantSearch(query).bestResult` is an
  `InstantSearch<Kind>BestResult`: "daft punk" gives the artist.
- **C1:** the Playlists page became *Favourites*, with tabs for playlists, albums, artists and
  podcasts. Each tab and each list page has a search and a sort, kept per list. "Recently
  played" sorts are not offered: no list carries a play date.
- **C2:** reorder goes through gw `playlist.updateOrder`, and only works while the list shows in
  its own order. Edit goes through Pipe `UpdatePlaylist`.
- **C5:** Pin to Home reads `me.quickAccess(context: HOMEPAGE).pinnedItems(support: [ALBUM,
  ARTIST, PLAYLIST, PODCAST, TRACK]) { item }`. The list must be inline: passed as a variable, the
  query fails. It costs 6004 of the 25000 limit. Pins go through the web player's `PinItem`
  (`quickAccessAppendItem {id, type, context: HOMEPAGE}`, which answers `isLimitExceed` when
  Home is full) and `quickAccessUnpinItem`. Home shows them as its first row of tiles. Flow
  moods sit under Flow's hero. Lyrics translation is a switch on the lyrics page.
- **C6:** the liked ids already come from a separate cheap read (A3), so item 24 needed no change.

### C1. Sorting and filtering (todo 14)

All client-side, as both clients do it ✔ R.

| List | Default | Options |
|---|---|---|
| Liked tracks | — | default (manual), recently added, A–Z, artists A–Z, albums A–Z (Android tracklist sort, R) |
| Playlists | recently played (Android) | recently added, recently updated, A–Z |
| Albums | recently played | recently added, A–Z album, A–Z artist |
| Artists | recently played | recently added, A–Z |
| Podcasts | recently added | recently played, A–Z |

- **Playlist filters:** All / Personal / Added / Collaborative ✔ R. Show the
  tags only when ≥ 2 filters match (web, R).
- **In-list search:** title and contributor names, lower-cased, 300 ms
  debounce (web, R).
- The choice persists per list.

### C2. Playlist editing (todo 16)

- **Reorder:** gw `playlist.updateOrder {playlist_id, position:"0", order}`
  ✔. On failure, restore the order with "An error occurred."
- **Title, description, private, collaborative:**
  `UpdatePlaylist($input: PlaylistUpdateMutationInput!)` (R). Private and
  collaborative exclude each other (R).
- **Add:** keep `playlist.addSongs` (web).
- **Library guard:** tested against fakes only.

### C3. Credits (todo 18)

- Map `RoleKey` like the web player (R): author → "Author", composer →
  "Composer", misc → "Other contributors".
- Join names with " - ", localised into the 7 languages.

### C4. Search history and the best result (todo 21)

- Recent searches from A6's `History`, cleared with confirmation ("Recent
  searches cleared!", R).
- A top result from `instantSearch.bestResult` / web `SearchTopResult` (R).
  - **M**: the tab order, which wasn't found.

### C5. App features

| Feature | Spec |
|---|---|
| Share / copy link | `https://www.deezer.com/{track\|album\|playlist\|artist\|show\|episode}/{id}` (R); toasts "Link to this … has been copied" (R) |
| Favourite albums, artists, playlists, podcasts | A5 exports; heart on tiles and heroes; "Favourites" sections in Collection |
| Don't recommend | track and artist menus; toast "Got it! We won't recommend this …" (R) |
| Save a mix or the queue as a playlist | A6 |
| Explicit filter | Settings, three levels (A6), shown when `CAN_EDIT_EXPLICIT_CONTENT_LEVEL` |
| Pin to Home | `quickAccessAppendItem` / `quickAccessUnpinItem`, context HOMEPAGE, limit 8 ✔ R |
| Theme | Default / Light / Dark (R) |
| Flow moods | from A1 |
| Lyrics translation | `lineTranslated` from A4 |
| Manage plan | link to `https://www.deezer.com/<lang>/offers` (web, R) |

Not added: a separate download quality (the web player has none, R).

### C6. Cleanup (todos 24–28)

- **24:** derive the liked ids from the Liked reply; one fetch at sign-in.
- **25:** lyric lines reachable by keyboard; accessible names for the EQ
  preset and the folder boxes.
- **26:** show engine refusals of the EQ and the device, and don't save a
  refused device.
- **27:** `:7654`, 16 and 32000 move to `Tokens.cs`; remove the unread DTO
  fields, or show them where the app does (`TopAlbums` on Home).
- **28:** Settings subscribe to `AccountChanged`.

---

## Order, size and checks

| Step | Depends on | Rough size | Live checks (read-only unless noted) |
|---|---|---|---|
| A | — | largest: Pipe client and 10 items | tuner value, Flow batch size, MP3 LAME header, live drops |
| B | A1, A2 | medium | none beyond A's |
| C | A0, A5, A6 | medium | search tab order |

Each step ends like the last ones:
- full `check.ps1`;
- harness checks for the new UI;
- a rebuilt DLL;
- updated `CHANGELOG.md` and `apk-parity-todo.md`.

Any call that writes to the account (favourites, playlist edits, history,
bans) is tested against fakes only, never on the owner's library.
