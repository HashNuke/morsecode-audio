# Binary releases

`VERSION` is the single source of the stable semantic version. Release binaries
embed it and expose it through `morsecode-audio version`; ordinary `go run` builds
report `dev`. Releases use `vMAJOR.MINOR.PATCH` tags.

The scripts follow the repository conventions used by `ircxd`: bump first,
review and commit, create a local tag, then push it explicitly. The dry-run
option follows the release workflow in `topics.club`.

```sh
bin/bump-version patch
# Review VERSION and update docs/release-notes.md, then commit the release.
bin/release --dry-run
bin/release
git push origin main
git push origin v0.1.2
```

The last command is an example for the next patch version. Push the exact tag
printed by `bin/release`. `minor` and `major` are supported too. Bumping does not
commit, tag or push. Release refuses a dirty checkout, duplicate local/remote
tag, invalid version or failed origin lookup. No tags are rewritten.

## GitHub Actions

The Check workflow runs on pushes and pull requests. It checks formatting,
Go vet, the race-enabled test suite, version-script tests, a container build,
read-only/nonroot container startup, speech smoke and graceful termination.

On a `v*` tag, a release job runs only after those checks succeed. It validates
that the tag matches `VERSION`, builds six archives, smoke-tests the extracted
Linux AMD64 package, then publishes a GitHub Release with SHA-256 checksums.
Only that job has `contents: write`; it uses GitHub's ephemeral workflow token.
A failing test or build prevents publication. No developer API token is needed.

Targets are Linux, macOS and Windows, each for AMD64 and ARM64. Builds use
`CGO_ENABLED=0`, `-trimpath`, and an embedded version. Linux/macOS use tar.gz;
Windows uses zip. Archives contain the executable, README, docs and third-party
license notices. macOS/Windows binaries are currently unsigned. Cross-compilation
checks every target; native execution is verified on Linux AMD64 only.

To build without publishing:

```sh
bin/build-release /tmp/morse-release
```

The output directory must be empty. Building needs Go, Bash, Python 3, tar and
sha256sum (the standard Ubuntu CI environment). Running a downloaded executable
needs none of those development tools. Do not commit release archives.
