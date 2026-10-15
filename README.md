# MorseCode Audio

Run a small, standalone Morse audio API on your own computer or server.
Text becomes Morse audio; Morse audio becomes text. The binary includes its
homepage, API docs and font. No database, account, external codec or Go
installation is needed to run it.

## Download and run

Download the archive for your system from
[GitHub Releases](https://github.com/HashNuke/morsecode-audio/releases/latest).
Extract it, then run:

```sh
./morsecode-audio serve
```

On Windows, run `morsecode-audio.exe serve` in PowerShell.
Open **http://127.0.0.1:8787** for the local API and its documentation.

| System | Archive suffix |
| --- | --- |
| Linux, Intel/AMD 64-bit | `linux_amd64.tar.gz` |
| Linux, ARM64 | `linux_arm64.tar.gz` |
| macOS, Intel | `darwin_amd64.tar.gz` |
| macOS, Apple Silicon | `darwin_arm64.tar.gz` |
| Windows, Intel/AMD 64-bit | `windows_amd64.zip` |
| Windows, ARM64 | `windows_arm64.zip` |

Each release includes `checksums.txt` with SHA-256 hashes. macOS binaries are
not Apple-notarized; Windows binaries are not Authenticode-signed.

```sh
./morsecode-audio serve --host 127.0.0.1 --port 9000
./morsecode-audio version
./morsecode-audio --help
```

The default listener is loopback-only. To expose it on your network, use
`serve --host 0.0.0.0`. `HOST` and `PORT` environment settings also work;
command flags take precedence. The server uses HTTP locally; place it behind a
TLS proxy for public HTTPS/WSS. There is no authentication or persistence.
Stop it with Ctrl-C; active streams are canceled and HTTP drains within five seconds.

## Four services

- **Text to Audio:** submit text and receive streamed PCM audio over HTTP.
- **Audio to Text:** upload a Morse PCM/WAV file and receive its transcript.
- **Realtime Text to Audio:** stream text fragments over WebSocket and receive
  audio immediately, with `isFinal` completion.
- **Realtime Audio to Text:** send PCM chunks over WebSocket and receive
  cumulative partials and committed transcripts.

These follow the supported ElevenLabs request/event conventions while producing
and recognizing Morse tones, not human speech. Audio is mono PCM16 at a fixed
20-WPM PARIS timing profile and 700 Hz. Both realtime directions support
backpressure and cancellation; no complete audio response needs to be buffered.

```sh
curl --fail-with-body --no-buffer \
  'http://127.0.0.1:8787/v1/text-to-speech/morse/stream' \
  -H 'Content-Type: application/json' \
  -d '{"text":"SOS","model_id":"morse_v1"}' \
  -o sos.pcm
```

## Use the hosted API

Anyone can use **https://morsecode.vxpipe.com** instead of running a binary.
The same routes work on both origins. Read the
[API documentation](https://morsecode.vxpipe.com/) or give your agent the
[plain-text reference](https://morsecode.vxpipe.com/llms.txt).
The embedded examples use the public origin; when running locally, substitute
`http://127.0.0.1:8787` and `ws://127.0.0.1:8787`.

Vxpipe uses one `morse_code_api` adapter with its normal TTS/STT components.
Set `MORSE_CODE_API_BASE_URL` to your local or hosted origin and restart Vxpipe.
The endpoint is separate from saved service credentials and call specs.

## Development and verification

Use Go 1.26.1, pinned in `.tool-versions`:

```sh
go run ./cmd/morsecode-audio serve
# In another terminal:
go run ./cmd/smoke -base-url http://127.0.0.1:8787
```

The smoke command verifies discovery, HTTP synthesis, both realtime WebSockets
and batch transcription. It requires an explicit origin and follows no redirects.

```sh
test -z "$(gofmt -l cmd internal)"
go vet ./...
go test -race ./...
python3 scripts/test_release.py
docker build -t morsecode-audio:local .
```

See the [supported API](docs/elevenlabs-compatibility.md),
[Morse profile](docs/morse-audio.md), [realtime STT](docs/realtime-stt.md),
[realtime TTS](docs/realtime-tts.md), [load tests](docs/load-test.md),
[release workflow](docs/releases.md) and [Railway runbook](docs/railway-deployment.md).
The container uses the same server command and runs as a nonroot user.
