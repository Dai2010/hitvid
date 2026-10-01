# Third-party notices for the standalone Linux amd64 release

The release binary statically links the components listed below. The versions
and build inputs are recorded in `third_party/versions.lock` and
`third_party/SOURCE-OFFER.md`.

| Component | Version | License | Source |
|---|---:|---|---|
| FFmpeg libraries | 7.1.1 | LGPL-2.1-or-later (GPL components disabled by the release configuration) | https://ffmpeg.org/releases/ffmpeg-7.1.1.tar.xz |
| GLib | 2.84.4 | LGPL-2.1-or-later | https://download.gnome.org/sources/glib/2.84/glib-2.84.4.tar.xz |
| Chafa library | 1.14.5 | LGPL-3.0-or-later | https://github.com/hpjansson/chafa/releases/download/1.14.5/chafa-1.14.5.tar.xz |
| PCRE2 | build-host version | BSD-style | https://github.com/PCRE2Project/pcre2 |
| libffi | build-host version | MIT-style | https://github.com/libffi/libffi |
| zlib | build-host version | zlib license | https://zlib.net/ |
| golang.org/x/term | v0.33.0 | BSD-3-Clause | https://cs.opensource.google/go/x/term/+/v0.33.0 |
| golang.org/x/sys | v0.34.0 | BSD-3-Clause | https://cs.opensource.google/go/x/sys/+/v0.34.0 |

The full LGPL texts used by the release are included in `licenses/`. The
BSD/MIT-style and zlib terms are summarized in `licenses/OTHER-LICENSES.md`;
the exact copyright notices remain in the corresponding upstream source trees.
The build records concrete package versions before producing a release.
