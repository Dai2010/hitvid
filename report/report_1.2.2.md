# hitvid v1.2.2

## Goal

Make the official Linux amd64 release self-contained at runtime.

## Changes

- Official release binaries use the native FFmpeg + Chafa backend.
- FFmpeg, GLib, and Chafa are linked statically into `hitvid`.
- The Debian package no longer declares `ffmpeg` or `chafa` runtime dependencies.
- Release CI produces both `hitvid-linux-amd64` and a standalone `.deb`.
- CI rejects release binaries with ELF shared-library `NEEDED` entries.
- CI runs the binary with `PATH=/nonexistent` to verify startup cannot depend on external commands.
- A native smoke test decodes a generated PPM frame through the embedded FFmpeg API and renders it through the embedded Chafa API.
- Release artifacts include third-party notices, LGPL texts, a source-offer document, and a SHA-256 manifest for the pinned native source archives.
- `make build` now builds the standalone release artifact. `make compat` retains the external-command development backend.

## Scope

The fully standalone release target in v1.2.2 is Linux amd64. Other targets can
still use the compatibility development build until their native release
toolchains are added.

## Runtime contract

The official `hitvid-linux-amd64` and `hitvid_*_amd64.deb` artifacts require
no separately installed `ffmpeg`, `ffprobe`, `chafa`, or shared media
libraries.

## Third-party and source materials

The static release links FFmpeg 7.1.1 (LGPL-2.1-or-later), GLib 2.84.4
(LGPL-2.1-or-later), and Chafa 1.14.5 (LGPL-3.0-or-later), plus PCRE2,
libffi, zlib, and the BSD-licensed Go modules listed in
`third_party/NOTICES.md`. The release contains the applicable LGPL texts and
`SOURCE-MANIFEST.txt`. The source-offer and any static-link relinking
requirements must be reviewed before distribution.
