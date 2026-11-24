# How a Deezer session ends, and how OpenDeezer presents itself

Measured on 2026-11-23 with the owner's account, read-only except where noted.

## The ARL is not renewed

OpenDeezer signs in with the `arl` cookie of a Deezer web session.

- No response sets a new `arl`, whether a gateway call (`deezer.getUserData`),
  `auth.deezer.com/login/arl` or the web player's page. The cookies that are
  set are `sid`, `dzr_uniq_id`, `account_id` (deleted), `refresh-token` (30
  days) and Akamai's bot-manager cookies `_abck` and `bm_sz`.
- `auth.deezer.com/login/renew` renews the JWT from a refresh token alone,
  without the arl. Neither the JWT nor the refresh token opens a gateway
  session, as cookie or as bearer: `deezer.getUserData` answers as for a
  visitor. So once Deezer refuses an ARL, only a new sign-in brings a session
  back.
- Deezer's Android app signs in as its own device: gateway `mobile_auth`,
  `api_checkToken` with an auth token, `login/arl` with
  `{arl, account_id}`, and mail and password for a new login. It keeps that
  session; nothing in it depends on a browser's.

## What ended a session

The ARL saved on 2026-11-16 worked until 2026-11-23 15:41. In the week before,
OpenDeezer had sent `log.listen` with `next_media` only. On 15:41:09 and
15:41:22 it sent its first listens with `params` (lt, stat, ctxt, dev and so
on), whose `dev.v` was OpenDeezer's version ("3.2.0") where the web player
sends its build number. Both were accepted, and the play reached the listening
history. At 15:41:25 a call's re-login was refused: Deezer had ended the
session. The browser session of the same account stayed signed in.

The timing points at those listens. Which field, if any, Deezer objected to is
not known.

## What OpenDeezer sends now

Every request to Deezer and its CDN looks like the web player's:

- the User-Agent of a current desktop Edge on the platform (it was
  `Mozilla/5.0 OpenDeezer/0.1`);
- on gateway calls: `Origin` and `Referer` of www.deezer.com, `X-Deezer-User`,
  and a random `cid`, as the web player's `getRequest` sets them;
- in each listen's `dev`: the web player's build (`releaseNumeric`, e.g.
  `10020261123132810`) with type 0, and `ts_listen` on Deezer's clock (the
  login's `SERVER_TIMESTAMP`); `l_30sec` only for a preview;
- on the live socket: `?version=<releaseNumeric>`.

The build changes with each of Deezer's web releases (19 and 23 November
seen). After each login the engine and the TUI read the script the logged-in
page names (`app-web.<hash>.js`) and, only when it is a new one, the build in
it (about 3 MB into the 4 MB file). They keep it in `web-release.json`. Until a
login reads one, the build of OpenDeezer's release is sent.

## When a session ends anyway

The engine marks the client refused when a login, or a call's re-login, is
refused (`DZSessionExpired`, `SessionExpired` in the mobile API). In the
Windows app:

- **At start:** with music kept offline, the app opens on it with "Session
  expired" and "Sign in again", instead of the sign-in page.
- **While running:** a sticky notice offers "Sign in again". The same account
  signing in again keeps the queue.
- **On the sign-in page:** only the session's cookies (`arl`, `sid`,
  `refresh-token`, `account_id`) are deleted, not the device's.

Live test, 16:15–16:18 the same day, as the web player:
- The engine read the current build from the logged-in page.
- It played a track for 35 s and another for 8 s, then stopped.
- It sent the same three listens as at 15:41, now with the browser's identity
  and `dev.v` 10020261123132810. All were accepted, and the 35-second play
  reached the history.
- The saved ARL then logged in fine four times over 90 s. At 15:41 it had been
  refused within 3 s of the reports.

That one run is consistent with Deezer having ended the session for the old
identity. It doesn't prove it.

## Not done

- The other GUIs do not read `DZSessionExpired` yet.
