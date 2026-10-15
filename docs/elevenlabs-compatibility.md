# Supported speech API

This is a small ElevenLabs-shaped interface for Morse PCM, not full ElevenLabs
feature or SDK parity. The user's clarified scope is the existing Morse TTS/STT
behavior hosted online. Unsupported features are not pending parity requirements.

| Route | Supported behavior |
| --- | --- |
| `POST /v1/text-to-speech/morse` | JSON `text` and optional `model_id`; raw mono PCM |
| `POST /v1/text-to-speech/morse/stream` | Same input, PCM flushed incrementally with HTTP backpressure |
| `POST /v1/speech-to-text` | Multipart `file`, `model_id`, `file_format`; Morse transcript |
| `GET /v1/speech-to-text/realtime` | WebSocket chunk input and partial/committed transcript messages |
| `GET /v1/models`, `GET /v1/voices` | Static Morse discovery for service setup |
| `GET /healthz`, `GET /readyz` | Health and readiness without authentication |

All speech/discovery routes are public, including realtime WebSocket STT.
The optional `xi-api-key` header is accepted and ignored, even when blank.
There is no key registry or token issuance. Invalid supported-route input returns 422,
unknown routes return 404, overload returns 429 and draining returns 503.
Missing keys are accepted; compatibility keys are not an authorization boundary.

TTS uses `morse_v1` and voice `morse`. It defaults to `pcm_16000`, and
supports explicit `pcm_8000`, `pcm_16000`, `pcm_24000` and `pcm_48000`.
This PCM default intentionally differs from ElevenLabs' MP3 default. No FFmpeg,
MP3/Opus conversion, alignment API or TTS WebSocket is included.

Batch STT uses `morse_stt_v1`. For raw mono 16-kHz S16LE input, supply
`file_format=pcm_s16le_16`; for PCM16 WAV, use `file_format=other`.
The response includes `text` and sample-derived word timing. Existing Scribe
model aliases are accepted for the same Morse behavior, without speech-model
semantics. File data is bounded to 12 MiB in memory, never spilled to disk.

See [realtime STT](realtime-stt.md) for the WS message shape. HTTP synthesis is
limited to eight concurrent requests and speech sockets to 64 connections.
Text is bounded to 4096 bytes and individual synthesized audio to 300 seconds.
Tests cover invalid input, empty/silent audio, independent Morse fixtures,
chunk boundaries, cancellation, admission limits and shutdown.

No human speech, cloning, browser tokens, multi-context synthesis, compressed
format matrix, URL ingestion, diarization, retained jobs, webhooks or transcript
history is implemented. The prior broad compatibility implementation and schema
inventory were removed to keep this service focused on the remote PCM provider.
