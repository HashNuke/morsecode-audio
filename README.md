# MorseCode Audio API

An independent, storage-free Go service for Morse audio behind ElevenLabs-shaped
TTS/STT protocols. It accepts arbitrary nonempty compatibility keys and never
contacts ElevenLabs. Recognition is for Morse tones, not ordinary speech.

## Development

Use Go 1.26.1 (`asdf install` with the included tool configuration), then:

```sh
HOST=127.0.0.1 PORT=8787 go run ./cmd/morsecode-audio
curl http://127.0.0.1:8787/readyz
curl -H 'xi-api-key: synthetic-example' http://127.0.0.1:8787/v1/models
go test -race ./...
go vet ./...
```

Environment files are examples only; the application reads process variables and
does not load `.env` automatically. Select another free port when needed. The
listener defaults to loopback, requires a valid numeric port and reports bind
conflicts. SIGTERM stops readiness and drains HTTP requests within five seconds.

Currently implemented: health/readiness, arbitrary-key HTTP authentication and a
static Morse voice/model catalog. Speech generation, recognition and WebSockets
are subsequent checkpoints; see [the compatibility ledger](docs/elevenlabs-compatibility.md).
No production or hosted compatibility claim is made yet.
