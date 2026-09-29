# Native media backend

The v1.2.2 Linux amd64 release uses this backend exclusively. FFmpeg and Chafa
are linked into the hitvid executable and are called through their C APIs, so
playback does not spawn `ffmpeg`, `ffprobe`, or `chafa` processes.

## Build

```sh
make build
```

`make build` performs four stages:

1. downloads the pinned FFmpeg, GLib, and Chafa source releases;
2. builds static libraries below `native/build/`;
3. runs the native Go tests, including an embedded decode/render smoke test;
4. links `dist/hitvid-linux-amd64` as a fully static executable and verifies
   that it has no ELF `NEEDED` entries and can start with an empty `PATH`.

Build-time tools such as a C compiler, Meson, Autotools, and Go are required
only when compiling from source. They are not runtime requirements of the
official standalone binary.

FFmpeg is configured with file input and the video demuxers/decoders used by
hitvid. Chafa is built without its command-line program or image loaders
because hitvid passes decoded RGB frames directly to the Chafa canvas API.

The compatibility Go backend remains available via `make compat` for
development and unsupported targets. That developer build uses external
FFmpeg/Chafa commands and is not an official v1.2.2 release artifact.
