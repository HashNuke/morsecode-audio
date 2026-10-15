# Morse audio API

The existing Morse audio behavior, served over ElevenLabs-shaped HTTP and
WebSocket interfaces. Text becomes Morse PCM; Morse PCM becomes text. It does
not recognize human speech or call ElevenLabs. Any nonempty `xi-api-key` works.
No database, registration, retained content or external audio codec is needed.

Audio streams: HTTP TTS renders and flushes small PCM buffers as they are
requested; realtime STT consumes PCM chunks and emits partial/final transcripts.
Cancellation and socket backpressure bound work. The multipart STT endpoint is
a convenience for complete PCM/WAV files, not the live room path.

## Local use

Use Go 1.26.1 (also pinned in `.tool-versions`):

```sh
HOST=127.0.0.1 PORT=8787 go run ./cmd/morsecode-audio
```

In another terminal:

```sh
go run ./cmd/smoke -base-url http://127.0.0.1:8787
curl -H 'xi-api-key: any-example-key' -H 'Content-Type: application/json' \
  -d '{"text":"SOS","model_id":"morse_v1"}' \
  'http://127.0.0.1:8787/v1/text-to-speech/morse/stream?output_format=pcm_16000' \
  -o /tmp/morse.pcm
```

Choose another free port if necessary. Environment files are examples; the app
reads process variables and does not load `.env` itself. The smoke command
requires an explicit origin, uses a synthetic key by default, never follows
redirects and exits nonzero on failure. It checks the live HTTP and STT WS paths.

Vxpipe uses one `morse_code_api` adapter with its normal TTS/STT components.
Set `MORSE_CODE_API_BASE_URL=http://127.0.0.1:8787` in Vxpipe for local use;
after deployment, set it to `https://morse-audio.definerun.com` and restart.
The endpoint is not part of the saved service credential or call spec.

See the [supported API](docs/elevenlabs-compatibility.md),
[Morse profile](docs/morse-audio.md), [realtime messages](docs/realtime-stt.md)
and [Railway runbook](docs/railway-deployment.md).

## Verify

```sh
test -z "$(gofmt -l cmd internal)"
go vet ./...
go test ./...
go test -race ./...
docker build -t morsecode-audio:local .
```

The container runs one static Go binary as a nonroot user. SIGTERM stops
readiness, cancels active sockets and drains HTTP within five seconds.
