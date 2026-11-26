# Scoop

The manifest installs the 64-bit Windows terminal client as `purpledisc.exe`
and adds the `purpledisc` command to the user's path.

Until a publisher-maintained Scoop bucket is available, install the manifest
directly from this repository:

```powershell
scoop install https://raw.githubusercontent.com/Scharkenberg/PurpleDisc/main/packaging/scoop/purpledisc.json
```

The manifest tracks stable GitHub releases and includes an autoupdate template
that reads the Windows binary's checksum from `SHA256SUMS.txt`.

## Publishing as a bucket

For a stable `scoop install` name and automated manifest updates, create a Scoop
bucket repository, place this file at `bucket/purpledisc.json`, and register the
bucket with Scoop. A future submission to `ScoopInstaller/Main` should follow
its package-request and pull-request process; Main currently expects command-line
projects to meet its popularity criteria before inclusion.
