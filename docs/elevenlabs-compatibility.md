# ElevenLabs speech compatibility ledger

Baseline: official public HTTP OpenAPI retrieved 2026-10-14, content hashes in
[`contracts/pin.json`](../contracts/pin.json); Python SDK 2.72.0. The reduced
[schema snapshot](../contracts/elevenlabs-http.json) preserves speech/discovery
requests, responses, defaults and their transitive schemas. Upstream prose and
examples are omitted. The snapshot is a contract, not evidence of implementation.

Status: initial server/catalog checkpoint. Speech routes are not implemented yet.
Do not describe this revision as a complete ElevenLabs replacement.

| Operation | Status | Evidence / remaining work |
| --- | --- | --- |
| `/healthz`, `/readyz`, draining | implemented | `TestHealthAndDrain` |
| arbitrary nonempty `xi-api-key` HTTP auth | implemented | `TestCatalogAuthentication`; no account or upstream request |
| `GET /v1/models` | implemented for initial TTS model | `TestCatalogAuthentication`, SDK/schema smoke |
| `GET /v1/voices`, voice lookup/settings/default settings | implemented for one immutable Morse voice | `TestVoiceDiscoveryAndSettings`, SDK/schema smoke |
| `GET /v2/voices` | initial response only | query/filter/pagination semantics still pending H |
| Four HTTP TTS variants | pending B/C | binary, streaming, timing JSON, timing NDJSON; formats below |
| TTS single-context WebSocket | pending E | init, incremental text, flush, EOS, audio, final, alignment |
| TTS multi-context WebSocket | pending E | independent context lifecycle and fair bounded output |
| Batch multipart STT | pending B/H | formats, timestamps, multichannel, input limits |
| Realtime STT WebSocket | pending D | audio chunks, partials, manual/VAD commits, timed commits, errors |
| Bearer/WS initialization authentication | pending E | preserve documented auth locations and precedence |
| Speech single-use tokens | pending E | purpose, expiry, one-time consumption, bounded memory |
| Retained transcript GET/DELETE | decision pending | conflicts with no retained content |
| Webhook jobs and historical request-ID stitching | decision pending | no persistent jobs/history; proposed explicit error |

## Audio/model contract

The service's synthetic voice is `morse`, with TTS model `morse_v1`. Additional
Morse recognition models and documented ElevenLabs compatibility aliases will
arrive with speech handlers. Never call a real model provider. Catalog costs
describe this deterministic service, not reported ElevenLabs charges.

The pinned nonstreaming TTS schema advertises 28 formats: seven PCM and seven WAV
rates (8000, 16000, 22050, 24000, 32000, 44100, 48000), seven MP3 combinations,
five Opus bitrates at 48000 Hz, `ulaw_8000` and `alaw_8000`. Streaming omits the
seven WAV values in the upstream schema. Default is `mp3_44100_128`.
Exact format enums/defaults are in the snapshot. No format is implemented yet;
each needs real encoded-output and independent-decoder evidence before acceptance.

## Authentication and unsupported behavior

An HTTP request presently requires a nonempty `xi-api-key`; keys are not stored,
registered or checked externally. Missing keys return HTTP 401 with
`detail.status=needs_authorization`. Whitespace-only credentials are empty.
Unknown voice IDs return 404 `voice_not_found`. Missing routes return 404
`not_found`. Draining rejects new API work with 503 `not_ready`.

The final authentication profile must include all documented speech forms. Token
state will be bounded in-memory protocol state, invalidated on restart, with one
replica initially. Keys never appear in logs. No response claims key ownership.

No storage-dependent or natural-language semantic transformation is silently
accepted. Unsupported operations currently remain unimplemented; their exact
errors and owner-approved compatibility exceptions are still pending.

## Executable contracts

```sh
uv run --with-requirements contracts/requirements.txt \
  python contracts/catalog_smoke.py http://127.0.0.1:8787
```

Always supply the target origin explicitly. The contract runner never contacts
ElevenLabs or loads real credentials. Expand it as each speech surface arrives.
The public upstream API/schema fetch is separate from this local conformance run.

## WebSocket reference sources

The [field inventory](http-field-inventory.md) enumerates every captured HTTP request field.

HTTP OpenAPI does not include the WS protocols. Capture field/message fixtures
and source digests before implementing their handlers, using these primary pages:

- [TTS single context](https://elevenlabs.io/docs/api-reference/text-to-speech/v-1-text-to-speech-voice-id-stream-input).
- [TTS multi context](https://elevenlabs.io/docs/api-reference/text-to-speech/v-1-text-to-speech-voice-id-multi-stream-input).
- [Realtime STT](https://elevenlabs.io/docs/api-reference/speech-to-text/v-1-speech-to-text-realtime).

## Evidence

Initial red: `go test ./internal/api ./internal/config` failed because neither
implementation package existed. Initial green: focused tests, `go vet ./...`
and `go test -race ./...` pass. The binary ran on port 18787; the pinned official SDK discovery runner passed
and validated six response schemas. Missing-key rejection passed. SIGTERM
terminated the server cleanly. Subsequent behavior has its own red/green record.
