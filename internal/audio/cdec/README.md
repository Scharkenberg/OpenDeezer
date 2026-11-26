Single-header C decoders, used when the engine is built with cgo (Android, iOS, the
Windows DLL and the desktop builds); the pure-Go decoders serve builds without it.

- minimp3.h: https://github.com/lieff/minimp3, `master` at commit
  b172fd6020124e6dc5fa86d87a80e38da84b7143 (2026-11-22), unchanged. Newer than the last
  tagged release (it has `hdr_is_null_or_tag`, for example). CC0 1.0 (public domain
  dedication).
- dr_flac.h: https://github.com/mackron/dr_libs, `master` at commit
  09e1d68709127b32e74c0029e04198df6ee0b4b2 (2026-11-16), unchanged (v0.13.5 in
  development). Public domain (Unlicense) or MIT No Attribution, at the reader's choice;
  the license text is at the end of the file.
