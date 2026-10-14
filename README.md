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

Implemented so far: health/readiness, arbitrary-key HTTP authentication, a
static catalog, explicit PCM synthesis and synchronous PCM16/WAV transcription.
Compressed audio, timed TTS and WebSockets remain subsequent checkpoints; see [the compatibility ledger](docs/elevenlabs-compatibility.md).
No production or hosted compatibility claim is made yet.

## PCM speech checkpoint

```sh
curl -H 'xi-api-key: synthetic-example' -H 'Content-Type: application/json' \
  -d '{"text":"SOS TEST 123","model_id":"morse_v1"}' \
  'http://127.0.0.1:8787/v1/text-to-speech/morse?output_format=pcm_16000' \
  -o /tmp/morse.pcm
curl -H 'xi-api-key: another-synthetic-key' \
  -F model_id=scribe_v2 -F file_format=pcm_s16le_16 -F file=@/tmp/morse.pcm \
  http://127.0.0.1:8787/v1/speech-to-text
uv run --with-requirements contracts/requirements.txt \
  python contracts/speech_smoke.py http://127.0.0.1:8787
```

Synthesis accepts explicit PCM at 8/16/22.05/24/32/44.1/48 kHz. The MP3 default
is not available in this checkpoint: request PCM explicitly. Batch recognition
accepts mono PCM16 RIFF/WAV or raw `pcm_s16le_16`. Uploads are bounded to 12 MiB
in process memory; no temporary upload files are written. Audio is limited to
300 seconds and text to 4096 bytes. Codec buffers remain bounded while reading
arbitrary chunks. Uppercase normalization and supported punctuation follow
[the Morse contract](docs/morse-audio.md).
