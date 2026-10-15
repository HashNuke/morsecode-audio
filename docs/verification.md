# Local verification — 2026-10-15

The simplified PCM implementation passes `go vet ./...`, `go test ./...` and
`go test -race -count=1 ./...` (35 passing tests/subtests, no failures).
The new PCM-default test first failed with codec-unavailable when FFmpeg was
absent; it now passes without any executable on PATH. Unsupported extension
tests initially found working MP3/token endpoints; those have been removed.

Streaming checks verify an early flush for a nearly five-minute synthesis,
handler retirement after client disconnect, bounded slow-reader writes,
repeated realtime utterances, partials, independent Morse signals and socket
drain. These checks retain streaming behavior while removing extra protocols.

The production image was built from this change and tested read-only with a
512-MiB limit, two CPUs and `PORT=9876` behind a different loopback host port.
Image: `sha256:581f4a5f0395510fffde6f52b70ddbb3fb3d6a24ffe711d4b1e6ecae859bb8cb`.
Docker reports 2,895,429 bytes and runtime user `65532:65532`.

`cmd/smoke` passed readiness, arbitrary-key authentication, discovery, streamed
PCM, batch STT and realtime STT against that image. Without an explicit origin
it exits 1. SIGTERM via `docker stop --timeout 10` produced exit code 0.
The owned test container was removed afterward.

Hosted Railway/domain checks remain pending the user's deployment. Vxpipe
integration against the simplified binary is tracked in its milestone; previous
broader compatibility evidence does not replace that check.
