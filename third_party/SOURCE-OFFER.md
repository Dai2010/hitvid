# Corresponding source and rebuild information

The standalone Linux amd64 binary is built from this repository revision using
`make build` and `scripts/build-native.sh`. The native source archives are
selected by `third_party/versions.lock` and are downloaded from their upstream
URLs listed in `third_party/NOTICES.md`.

For every release, the maintainer must publish the exact SHA-256 values of the
source archives and the build log alongside the binary. The release workflow
checks these files into the artifact so recipients can identify the precise
inputs used for the build.

The repository contains the hitvid source, C bridge, build scripts, linker
flags, and dependency configuration. Any legal determination about whether
this source and the accompanying relink materials satisfy the applicable LGPL
static-linking conditions must be made before distribution.
