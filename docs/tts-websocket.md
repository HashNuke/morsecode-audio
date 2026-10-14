# Single-context incremental synthesis

`GET /v1/text-to-speech/{voice_id}/stream-input` accepts the pinned ElevenLabs
single-context messages. Use voice `morse` and model `morse_v1`. See [multi-context TTS](multi-context-tts.md) for the independent lifecycle
of `multi-stream-input`.

## Sequence and output

Send `{"text":" "}` to initialize, then text fragments. By default synthesis
starts at character thresholds 120, 160, 250 and 290; an initial
`generation_config.chunk_length_schedule` can set 1–16 thresholds of 50–500.
`auto_mode=true` releases each fragment immediately. `try_trigger_generation`
reduces the current threshold to 50. These thresholds are deterministic Morse
buffering decisions, not a language model's quality heuristics.

`flush=true` finishes buffered text and its fourteen-unit utterance tail while
keeping the connection open. Later text begins a new generation with its own
codec stream. Send `{"text":""}` to finish all queued work: one `isFinal:true`
message follows all audio, then the server closes with code 1000. Flush by itself
does not emit a terminal socket marker. A single space is a keepalive when no
generation is active; within a phrase it preserves the pending word boundary.

Fragments share one continuous encoder until flush/EOS. This preserves letter
and word gaps instead of adding an utterance tail after every client message.
All 21 streaming formats are accepted; MP3 44.1 kHz/128 kbps is the default.
Responses carry base64 `audio`, optional `alignment` and `normalizedAlignment`.
Alignment uses `chars`, `charStartTimesMs`, `charDurationsMs`, with starts relative
to that audio chunk. PCM fixtures reconstruct exact source-clock timing. Codec
priming/padding and character boundaries spanning compressed packets still need
the complete semantic review in H; format support alone does not establish it.

## Authentication and bounds

Credential precedence is: `single_use_token` query, `xi-api-key` header,
`Authorization: Bearer ...` header, `authorization` query, initial message.
Initialization accepts `xi_api_key`, the pinned schema's `xi-api-key` alias, or
`authorization`, in that order. Query/initial authorization accepts a bare opaque
credential or a Bearer value. Explicit invalid higher-priority credentials never
fall back. Tokens must have purpose `tts_websocket` and consume once before
payload validation/upgrade. Header-authenticated initialization may omit a key.

Anonymous upgrades count toward the 64-socket limit and have five seconds to
initialize. Text messages are limited to 64 KiB; each generation permits 4096
input bytes and 300 seconds of audio. Each connection has four queued generations
and sixteen queued fragments per generation, holding lazy PCM readers rather
than complete audio. At most eight HTTP/WS encoders run globally. Output chunks
are bounded to 512 KiB before base64 encoding, with five-second writes. Idle
timeout defaults to twenty seconds, configurable from one to 180; absolute
socket lifetime is one hour. Limits fail explicitly rather than growing queues.

Disconnect, timeout and server drain cancel input and join output/codec work
before releasing socket admission. Protocol failures emit an `error`/`message`
object and close with 1008 without echoing text or credentials. Option failures
before upgrade use HTTP errors. Voice/generation settings cannot change after
initialization. Unsupported dictionaries, SSML and non-default speed fail
explicitly; optional-field equivalence remains part of H's compatibility ledger.

## Verification

Tests were added before the handler and initially failed on missing routes/auth.
Go race tests cover early PCM before future input, finite/fragment equivalence,
original/normalized alignment, repeated flush, normal final closure, each auth
location, token precedence/replay, malformed options/messages, initialization
timeout, bounded queues and global capacity. Disconnect/drain tests cover PCM
and MP3 codecs waiting for more text and verify released admission.

`contracts/tts_websocket_smoke.py` validates captured AsyncAPI branches and uses
unmodified ElevenLabs Python SDK 2.72.0 with websockets 17.2. The upstream final
schema has no required fields and overlaps audio in its top-level `oneOf`; the
runner selects the event branch and independently checks terminal ordering.

That SDK always derives WSS even from an HTTP base URL. For local HTTP only, the
runner terminates TLS with a temporary trusted certificate and forwards bytes
unchanged. The SDK runs in a bounded subprocess; trust changes affect only that
child. Hosted HTTPS runs directly, without the fixture or certificate override.
No SDK patch, disabled certificate validation or retry-on-failure is used.

The local fixture initially had intermittent pre-upgrade stalls. Moving the SDK
to a subprocess and changing Python versions did not resolve them. Disabling
session tickets in the short-lived fixture produced ten consecutive complete
runs across Python 3.12/3.13. This is consistent with the documented
[OpenSSL session-ticket handshake hazard](https://github.com/openssl/openssl/issues/7967),
not proof of a particular upstream root cause or hosted TLS compatibility.
The fixture never needs session resumption; production TLS is Railway's concern.

Chrome 154 exercised `contracts/browser_speech_smoke.js` from a separate local
origin: CORS token issuance, initial-key TTS, token TTS, and independent-signal
token STT passed against the read-only production image. Each TTS run returned
48,000 PCM bytes, one final and close code 1000; STT recognized E. This is native
browser API acceptance, not an audible playback or browser SDK claim.

The production image also passed HTTP/realtime/token regressions and SIGTERM
with active STT plus open-input PCM and MP3 TTS. It exited 0, without OOM or
filesystem changes. Multi-context synthesis has separate acceptance evidence; hosted acceptance remains open.
