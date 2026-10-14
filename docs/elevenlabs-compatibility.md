# ElevenLabs speech compatibility ledger

Baseline: official public HTTP OpenAPI retrieved 2026-10-14, content hashes in
[`contracts/pin.json`](../contracts/pin.json); Python SDK 2.72.0. The reduced
[schema snapshot](../contracts/elevenlabs-http.json) preserves speech/discovery
requests, responses, defaults and their transitive schemas. Upstream prose and
examples are omitted. The snapshot is a contract, not evidence of implementation.

Status: HTTP speech, realtime STT, both TTS WebSockets and browser tokens;
remaining optional-field, input-format and semantic compatibility work is pending.
Do not describe this revision as a complete ElevenLabs replacement.

| Operation | Status | Evidence / remaining work |
| --- | --- | --- |
| `/healthz`, `/readyz`, draining | implemented | `TestHealthAndDrain` |
| arbitrary nonempty `xi-api-key` HTTP auth | implemented | `TestCatalogAuthentication`; no account or upstream request |
| `GET /v1/models` | implemented for initial TTS model | `TestCatalogAuthentication`, SDK/schema smoke |
| `GET /v1/voices`, voice lookup/settings/default settings | implemented for one immutable Morse voice | `TestVoiceDiscoveryAndSettings`, SDK/schema smoke |
| `GET /v2/voices` | initial response only | query/filter/pagination semantics still pending H |
| HTTP TTS convert | implemented for all 28 output formats and MP3 default | independent decode, PCM round trip, SDK default MP3 |
| HTTP TTS streaming/timing variants | implemented | binary stream, timing JSON/NDJSON; original/normalized alignment, disconnect/deadline cleanup, SDK schemas |
| TTS single-context WebSocket | implemented for the documented Morse profile | init, incremental text, flush, EOS, audio, final, alignment, bounded lifecycle; SDK and Chrome acceptance |
| TTS multi-context WebSocket | implemented for the documented Morse profile | independent context lifecycle, fair bounded output, flush-close preservation, idle expiry, cancellation/reuse; SDK types/schema and Chrome acceptance |
| Batch multipart STT | implemented for mono PCM16/WAV | independent signal and bidirectional Elixir fixtures, SDK/schema round trip; compressed/multichannel pending H |
| Realtime STT WebSocket | implemented for header/token auth and documented Morse options | independent signals at seven formats, repeated manual/VAD commits, timing, keepalive, bounded lifecycle, official SDK/AsyncAPI validation |
| Bearer/WS initialization authentication | implemented for both TTS WS routes | header/query/initial-message transports, precedence and initialization timeout; general HTTP/STT use xi-api-key or their documented tokens |
| Speech single-use tokens | issuance and consumption for all three purposes | expiry, atomic replay rejection, purpose/route scope, restart/drain invalidation and bounded memory; both TTS WS routes consume the same purpose |
| Browser CORS/origins | implemented for available speech routes | unauthenticated OPTIONS, explicit credentials only, wildcard origin without cookie credentials; cross-origin STT WS fixture |
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
Exact format enums/defaults are in the snapshot. All 28 nonstreaming formats and
21 streaming formats now have independent decode/packet-clock evidence. See
[HTTP output behavior and evidence](http-output.md), including source-clock
alignment and MP3 priming/padding. Optional request transformations and full
semantic parity remain H work; working formats alone do not establish them.

## Authentication and unsupported behavior

HTTP accepts a nonempty `xi-api-key`; keys are not stored,
registered or checked externally. Missing credentials return HTTP 401 with
`detail.status=needs_authorization`. Whitespace-only credentials are empty.
Unknown voice IDs return 404 `voice_not_found`. Missing routes return 404
`not_found`. Draining rejects new API work with 503 `not_ready`.

`POST /v1/single-use-token/{token_type}` issues `realtime_scribe`, `batch_scribe`
and `tts_websocket` tokens with the pinned response schema. Batch and realtime
STT accept their respective token in the `token` query parameter, with no header
required. Both TTS WS routes accept `single_use_token` or header/query/initial
credentials; see [TTS authentication and precedence](tts-websocket.md).

Tokens expire at 15 minutes and consume atomically on successful authentication,
before payload validation or socket upgrade. A failed admitted operation therefore
requires a fresh token. Wrong-purpose attempts do not consume a still-valid token.
An explicitly supplied token takes precedence over the header: empty, unknown,
expired, reused, duplicate or wrong-purpose tokens return 401 `invalid_token`
without falling back to a key. Tokens cannot authenticate discovery or issuance.
These precedence/error choices are the service's explicit compatibility profile;
the upstream reference does not specify all mixed-credential/error cases.

Each server owns at most 4096 outstanding token records. Issuance sweeps expired
records and returns 429 `too_many_tokens` at capacity. Consumption frees capacity;
drain clears all records and blocks issuance. Records contain only a random-token
SHA-256 digest, purpose and expiry, never an issuing key or user content. All
records disappear on restart; run one replica. No background timer or storage is
required. Tokens are removed from the internal query before speech options and
session acknowledgements are constructed; diagnostics never log raw request URLs.

All browser origins may use this deliberately public compatibility API. HTTP API
responses expose `Access-Control-Allow-Origin: *`; OPTIONS allows GET/POST/DELETE
and explicit content-type/key/authorization headers without authentication or
token consumption. Cookie credentials are never accepted or enabled by CORS.
WebSocket origin checks permit cross-origin clients; authentication and bounded
socket admission remain mandatory. Chrome 154 passes cross-origin token issuance,
first-message/token TTS and token STT using the browser fixture. This establishes
native browser API behavior, not acceptance of every browser SDK.

Evidence: token tests first failed 404 and preflight failed 401. Focused and full
Go race checks/vet pass. Clock-controlled fixtures prove the exact expiry boundary,
capacity recovery and drain; 16 competing requests admit exactly one transcript.
Official Python SDK 2.72.0 issues all three types, consumes batch/realtime tokens,
and validates token/WS schemas. `contracts/token_smoke.py` also tests token-only
batch recognition, replay/purpose errors and CORS without any real credentials.
No response claims key ownership. Error/precedence choices remain the explicit
Morse compatibility profile; complete API semantics still require H review.

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

HTTP OpenAPI does not include WS protocols. The separately captured
[AsyncAPI snapshot](../contracts/elevenlabs-websockets.json) contains the three
WS channels and 61 transitive message schemas, including query/header bindings,
all fields, defaults, enums and error messages. Source/specification digests are
in the pin manifest. These schema fixtures establish the target before handler
implementation; they are not passing WS behavior tests. Primary sources:

- [TTS single context](https://elevenlabs.io/docs/api-reference/text-to-speech/v-1-text-to-speech-voice-id-stream-input).
- [TTS multi context](https://elevenlabs.io/docs/api-reference/text-to-speech/v-1-text-to-speech-voice-id-multi-stream-input).
- [Realtime STT](https://elevenlabs.io/docs/api-reference/speech-to-text/v-1-speech-to-text-realtime).

## Evidence

Initial red: `go test ./internal/api ./internal/config` failed because neither
implementation package existed. Initial green: focused tests, `go vet ./...`
and `go test -race ./...` pass. The binary ran on port 18787; the pinned official SDK discovery runner passed
and validated six response schemas. Missing-key rejection passed. SIGTERM
terminated the server cleanly. Subsequent behavior has its own red/green record.

### PCM speech evidence

- Initial API red: both new HTTP speech tests returned 404 before handlers existed.
- Codec red: the implementation package did not exist. Independent square-wave
  SOS 2 fixtures initially exposed partial-window timing/frequency errors at
  22.05 kHz; fixed by classifying transition windows by majority active samples.
  All seven rates now pass with arbitrary byte chunk boundaries.
- Timestamp red: decoder events had no character timestamps; sample-derived
  character/word timings now pass independent E/space/T interval checks.
- SDK regression red: official Python 2.72.0 sends literal JSON `null` for three
  omitted nullable form fields. A focused test initially returned 422; explicit
  null now succeeds while unsupported non-null transformations remain rejected.
- Local Vxpipe integration passes Go → Elixir recognition at four shared rates
  and Elixir → Go raw PCM transcription. These are opt-in network tests in the
  owning CallEngine application, not default-suite external dependencies.
- No language/confidence inference: language defaults to `und`, probability is
  zero. Required numeric `logprob` is zero for the deterministic accepted
  codebook lookup (log(1)), not an acoustic confidence estimate. An explicit
  language label is preserved. Null logprob failed the pinned upstream schema
  even though the SDK parsed it; the numeric deterministic score passes that
  contract. This does not claim probabilistic human-speech recognition.

Final PCM checkpoint checks: `go test -race ./...`, `go vet ./...`, official SDK
speech round trip and the pinned transcription response schema pass. The
CallEngine local-network interop lane passed two tests (four Go→Elixir rates
and one Elixir→Go phrase). This does not establish compressed formats, WS,
configured provider sessions or hosted deployment.

### Proposed no-retention responses (owner decision pending)

| Request | Proposed response | Rationale |
| --- | --- | --- |
| Transcript GET/DELETE | HTTP 501, `detail.status=unsupported_feature` | no retained transcript objects exist |
| Batch `webhook=true` | HTTP 501, `detail.status=unsupported_feature` | no durable job or configured webhook registry |
| Nonempty TTS previous/next request IDs | HTTP 422, `detail.status=unsupported_feature` | no request-history lookup |
| `enable_logging` true/false | accepted without content retention | neither value enables storage |

Implement those errors only as explicit compatibility exceptions. The owner was
asked to resolve the no-storage conflict; this proposal is not an approval or a
full-parity claim. Semantic features unrelated to deterministic Morse (such as
entity extraction/transcript rewriting) similarly need an explicit feature
mapping or unsupported response in H.

WebSocket framing/library decision: use the maintained
[coder/websocket](https://github.com/coder/websocket) implementation with
context-based cancellation, origin checks and read limits. The first STT handler
pins v1.8.15. See [realtime STT behavior and evidence](realtime-stt.md), including
the Morse commit profile and pending optional semantic fields.


Multi-context lifecycle and browser/container evidence is recorded in
[multi-context TTS](multi-context-tts.md). The upstream
[authentication guide](https://elevenlabs.io/docs/api-reference/authentication)
specifies `xi-api-key` for general HTTP requests. Bearer query/initial fields are
part of the TTS WS bindings; the STT binding specifies a key header or `token`.
No additional general HTTP bearer transport is inferred from the TTS bindings.
