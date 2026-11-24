# Seeing what another Deezer session plays

Question: can OpenDeezer learn what another session of the same account (for
example the Deezer web player) is playing?

Answer: yes, through the live socket Deezer's own players use to keep an
account to one playing stream. OpenDeezer takes part in it like those
players: it pauses when another session starts, announces its own starts so
the others pause, and shows the other session's track in the Windows player
bar. The queue sync over Pipe exists but holds nothing for the account
tested.

Findings come from Deezer's public web-player bundles, two open-source
projects, and a probe on 2026-11-17 with the owner's signed-in web player
(a Deezer Free account in Germany) and OpenDeezer's own login of the same
account. None of this is a public API.

## Single-instance playback on the live socket (used)

Each Deezer player announces every track it starts on the account's live
socket, and pauses when another session's announcement arrives
(`SingleInstancePlaybackManager`, module `com.deezer.kmp.sip`; gatekeep
`single_instance_playback_relive`).

- Socket: `wss://live.deezer.com/ws/<USER_TOKEN>?version=<releaseNumeric>`
  (the web player's build; the Android app sends the token alone), where
  `USER_TOKEN` comes from `deezer.getUserData`. OpenDeezer connects as the web
  player does (deezer-sessions.md). Frames: `["sub", channel]`,
  `["send", channel, payload]`, `["msg", channel, payload]`.
- Channel: `<userId>_<userId>_STREAM`.
- Payload, as the web player sent it in the probe:

  ```json
  {"ACTION": "PLAY", "APP": "LIMITATION",
   "VALUE": {"USER_ID": "...", "UNIQID": "<session uuid>", "SNG_ID": "..."}}
  ```

  This matches pleezer's documentation.
- Observed on 2026-11-17: the web player sent it twice in a row on each
  start, on resume after a pause, and on most skips (one quick skip a few
  seconds after a resume sent nothing). It sends nothing on pause. A second
  subscriber, both in the browser and from Go with OpenDeezer's own login,
  received every message the web player sent.
- Observed on 2026-11-23, with a subscriber in a second browser tab:
  - a manual skip sent it once;
  - three automatic advances to the next track sent nothing;
  - a pause and resume sent nothing.

  A test message on a private channel between two of the probe's own
  sockets arrived at once, so the live server relays. The web player does
  not announce what it starts on its own. A second web player window of the
  account did not pause either.
- Observed on 2026-11-23 with the Android app (9.0.28.1, shuffle in
  Favourite tracks), with a subscriber in a browser tab:
  - four automatic advances to a song each sent it once, at the moment the
    song began;
  - each audio ad between tracks (a Free account) sent it too, with
    `SNG_ID` `965070877`, which `song.getData` does not know ("No song
    data"); the song after it was announced about 14 s later;
  - resuming after a stall in which the app still reported playing sent
    nothing.

  The app's code agrees. It keeps `SipState(isPlaying, canPause,
  playableIdentifier, mediaId)` and publishes on every change of that state
  while `isPlaying`, except for `RAW_URL` playables. So a new track, playing
  or a resume after a real pause is announced, and a pause is not. The web
  bundle has the same rule, so its player evidently does not report a new
  media id when it advances by itself.
- Not observed live: what the web player does when OpenDeezer publishes.
  The probe did not publish; pleezer documents that Deezer's players pause.

What OpenDeezer does with it:

- `deezer.Client.WatchStreams` (`internal/deezer/live.go`) subscribes to the
  stream channel after login, reconnects after a drop, and reports
  `StreamStart{TrackID, Session}` of other sessions. Each client has its own
  random `UNIQID`; the socket's echo of its own announcements is skipped.
- `deezer.Client.AnnouncePlay` sends the payload above on the open
  connection, once per start (the web player sends it twice).
- The engine (`internal/engine/elsewhere.go`), with single-device playback
  on (the default):
  - pauses local playback when another session starts a track (once per
    start; the duplicate is dropped);
  - announces every local song start: a play, an auto-advance, a gapless
    promote, and a resume. Episodes are not announced (the payload carries a
    song id). Announcing also forgets the other session's track, which
    pauses on it.
  - keeps the last start of another session with its track's metadata,
    `sinceMs` and `untilMs` (start plus the track's length and a minute,
    since Deezer announces no pause and the history entry of its end can
    take that long). Exports: `DZElsewhereJSON`, `DZElsewhereVersion`.
  - follows automatic advances the web player does not announce through the
    listening history, which it reads every 10 s while another session
    plays. Once the announced track has its entry, that session counts as
    playing a track it did not name: the payload has no `track`, `sinceMs` is
    when the entry was first seen, and `untilMs` is 6 minutes later. Each
    newer entry moves that on; none by `untilMs` ends it. An Android app's
    next announcement replaces the untitled track as soon as it arrives.
- Single-device playback is a setting in `playback.json`
  (`playAlongside: true` turns it off): `DZSetSingleDevice` /
  `DZSingleDevice`, and `SetSingleDevice` / `SingleDevice` in the mobile
  API. Off, OpenDeezer neither pauses nor announces and only shows the other
  session's track.
- Windows: Settings › Playback › "One player at a time"; the player bar
  shows the other session's track (marked with a speaker glyph, with Play
  here) while nothing plays locally and until `untilMs`; a track it did not
  name shows as the glyph with "Playing in another player".
- The engine log (`opendeezer.log`) records each connection, each drop and
  each start of another session, with a failed track lookup.

Reading the web player's code (`SingleInstancePlaybackManager`, 2026-11-23
bundle) suggested that every state change to playing with a new media id is
published, except for `RAW_URL` playables. The probe on the same day shows
otherwise for automatic advances. A publish while the web player's own
socket is reconnecting is dropped (`live.publish` returns false and nothing
retries).

The listening history (`user.getSongsHistory`) gets each track when it ends
or is skipped, with its start time (`TS`) and length. So it lags a whole
track: it shows that the other player went on, not what it plays now.
Measured with the Android app on 2026-11-23:
- entries showed 3 s, 28 s, 31–41 s and 22–32 s after their track ended;
- ads got none;
- a track that stalled for about seven minutes kept its start and its
  nominal length, so `TS` plus `DURATION` is not when a track ended.

Pipe's `lastKnownQueueState` was still `null` on 2026-11-23.

Limits:
- A pause or stop elsewhere is not visible, so the bar can show a paused
  track until its length has passed.
- The web player's automatic advances are not announced. The history shows
  only that the player went on, not to what, so the bar shows no title then,
  and OpenDeezer does not pause for it. When the web player's queue ends,
  the bar still shows it playing for up to 6 minutes.
- An ad on another player pauses OpenDeezer, as it would pause Deezer's
  players. Its id names no track, so the bar keeps the previous track until
  the next song is announced.
- When another player stops, the bar keeps its announced track for up to a
  minute past the track's length.
- There is no position, so Play here starts from the beginning.

## Queue sync over Pipe (exists, empty here)

The web player ships a queue synchronizer (`com.deezer.kmp.queuesync`,
gated by `QUEUE_SYNCHRONIZER` / `KMP_QUEUE_SYNCHRONIZER`) that stores the
playing queue on Deezer's GraphQL API at `https://pipe.deezer.com/api`:

```graphql
query KmpQsQueueState {
  me {
    lastKnownQueueState {
      position progressMS repeatMode version
      lastClientDetails { userAgent localQueueID locallyUpdatedAt }
      queueConfiguration
      queue { mediaList { id type contextId localQueueItemId } contexts { id content } }
    }
  }
}
```

- Auth: a JWT from `POST https://auth.deezer.com/login/arl?jo=p&rto=c&i=c`
  (or `/login/renew`) with the account's cookies; both answered with
  `{jwt, refresh_token}`. Pipe accepts this ad-hoc query.
- Probe result: `lastKnownQueueState` was `null` while the web player had a
  queue and played. The account's gatekeeps list no queue-sync flag, so the
  feature is off for it (and may be for most accounts).
- It would give queue, position and progress, which the live socket does
  not. Worth re-checking later or on a Premium account.

## Ruled out

- Deezer Connect (`REMOTEDISCOVER` / `REMOTECOMMAND`): a phone controlling a
  receiver; Deezer has retired it, and the web player was never a receiver.
- `wss://messaging.deezer.com/websocket` (XMPP): an old proof of concept
  (nSimonFR/deezer-playing-notifications) read `PLAY` events there; the
  current player uses the live socket instead.
- Listening history (`log.listen`): written after a track plays, so it lags
  and has no "currently playing" state.

## Not done

- Pausing and announcing run in the engine, so every app does them; the
  setting and the other player's track show only in the Windows app.
  The other GUIs (GNOME, KDE, macOS, iOS, Android) read none of the new
  exports yet.

## Sources

- Deezer web player bundles: `https://cdn-files.dzcdn.net/cache/js/app-web.*.js` and lazy chunks.
- Deezer for Android 9.0.28.1 (`deezer.android.app`), decompiled with jadx.
- pleezer: https://github.com/roderickvd/pleezer (`src/remote.rs`, `src/protocol/connect/stream.rs`, `src/gateway.rs`).
- deezer-playing-notifications: https://github.com/nSimonFR/deezer-playing-notifications
- Deezer Connect end of life: https://support.deezer.com/hc/en-gb/articles/5449309457949-Deezer-Connect
