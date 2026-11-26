# Purple Disc — unified Linux client

One `purpledisc` command that runs **native on whatever desktop you're on** —
Qt6/Breeze on KDE-family desktops (KDE, LXQt, Deepin, UKUI…), GTK4/libadwaita
everywhere else. Same idea as LibreOffice's VCL backends: a shared engine, two
UI backends, picked at runtime.

## How it works

```
purpledisc (launcher)
  ├─ detect desktop ($XDG_CURRENT_DESKTOP / XDG_SESSION_DESKTOP / DESKTOP_SESSION)
  ├─ dlopen  libpurpledisc-qt.so   (KDE-family)   ─┐  whichever's toolkit
  └─ dlopen  libpurpledisc-gtk.so  (otherwise)    ─┘  is installed; falls
        → call purpledisc_run(argc, argv)              back to the other
```

Each backend is the corresponding GUI (`gui/kde`, `gui/gnome`) built as a shared
library exporting `purpledisc_run`, with the Go engine linked in. The launcher
is linked with rpath `$ORIGIN`, so the `.so` files just sit next to it.

## Build & run

```sh
cd gui/linux
./build.sh
./dist/purpledisc
```

Needs both toolchains (GTK4 + libadwaita + json-glib dev, Qt6 dev, libasound2-dev,
meson/ninja, cmake, Go 1.27+, gcc). At **runtime** only the toolkit for your
backend has to be present — if the preferred one's libraries are missing, the
launcher falls back to the other.

The standalone `purpledisc-gnome` / `purpledisc-kde` binaries still build too
(they wrap the same `purpledisc_run`); the unified client just bundles both
backends behind one auto-selecting command.
