# MorseCode Audio API

An independent, storage-free Go service for Morse audio behind ElevenLabs-shaped
TTS/STT protocols. It accepts arbitrary nonempty compatibility keys and never
contacts ElevenLabs. Recognition is for Morse tones, not ordinary speech.

## Development

Use Go 1.26.1 (`asdf install` with the included tool configuration) and FFmpeg
with `libmp3lame` and `libopus` encoders (verified with FFmpeg 6.1.1), then:

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
static catalog, all four HTTP TTS variants and all 28 nonstreaming output formats,
plus synchronous PCM16/WAV transcription. WebSockets, remaining optional API fields,
compressed STT and deployment packaging remain subsequent checkpoints; see
[the compatibility ledger](docs/elevenlabs-compatibility.md).
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

Synthesis defaults to MP3 at 44.1 kHz / 128 kbps, and also supports PCM, WAV,
Opus, mu-law and A-law. Batch recognition
accepts mono PCM16 RIFF/WAV or raw `pcm_s16le_16`. Uploads are bounded to 12 MiB
in process memory; no temporary upload files are written. Audio is limited to
300 seconds and text to 4096 bytes. Codec buffers remain bounded while reading
arbitrary chunks. Uppercase normalization and supported punctuation follow
[the Morse contract](docs/morse-audio.md).

## HTTP streaming and timestamps

Use `/v1/text-to-speech/morse/stream` for early binary chunks. Add
`/with-timestamps` to either convert or stream for base64 JSON with original and
normalized character alignment. Timed streaming uses newline-delimited JSON;
concatenate decoded audio in response order. Streaming supports the 21 formats
in the upstream schema (WAV is only available on nonstreaming routes).

```sh
uv run --with-requirements contracts/requirements.txt \
  python contracts/tts_smoke.py http://127.0.0.1:8787
```

Encoding uses bounded memory/pipes; no media is written to disk. At most eight
HTTP synthesis requests run concurrently; additional work receives 429. Requests
have a 30-second processing/write deadline, including slow readers. Cancellation
closes and joins any codec process. See [HTTP output behavior](docs/http-output.md)
for timing, framing and error semantics.
