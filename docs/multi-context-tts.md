# Multi-context TTS

`GET /v1/text-to-speech/{voice_id}/multi-stream-input` uses the same format,
Morse model, token purpose, handshake authentication and limits as
[single-context TTS](tts-websocket.md), with independent context lifecycles.

The wire contract is the pinned AsyncAPI plus ElevenLabs Python SDK 2.72.0's
`InitializeConnectionMulti`, `SendTextMulti`, `CloseContext`, `CloseSocket`,
`AudioOutputMulti` and `FinalOutputMulti` types. The SDK's dedicated close-context
description specifies that an already-flushed context continues flushing after
close. Its combined message type has less precise wording; this implementation
preserves the explicit flush contract. The
[upstream guide](https://elevenlabs.io/docs/eleven-api/guides/how-to/websockets/multi-context-web-socket)
documents per-context keepalive and the five-context limit. Output follows the
pinned schema's `contextId` / `isFinal` casing despite snake-case examples in prose.

## Lifecycle

- An initial space creates a context; a first text message can also create one,
  matching the guide's conversational example. Omitted `context_id` selects
  `default`. IDs are nonblank strings of at most 128 bytes. At most five contexts
  exist per connection, including closed contexts whose output is still draining.
- Text without `flush` appends to that context's current generation. Empty text
  refreshes its idle deadline without ending input or producing silence/audio.
  Traffic in another context does not refresh this deadline.
- `flush:true`, with optional text, finishes the current generation. It receives
  its own audio and final marker while the context remains available for another
  generation. Repeated empty flushes do not fabricate generations or final markers.
- `close_context:true` cancels unflushed work and discards any of that generation's
  queued audio. Earlier flushed generations finish. Each started generation emits
  one final, including an aborted generation; a never-started context also gets a
  terminal marker when closed. `close_context:false` keeps the context and ignores
  any text on that control message, matching the pinned SDK description.
- The same ID can be reused immediately, subject to the five-context bound.
  Its new worker has fresh state; its output waits behind the retiring instance
  so identical IDs cannot interleave across lifetimes.
- `close_socket:true` flushes open input, drains all contexts and closes normally
  with code 1000. It does not duplicate finals for generations already flushed.
  A transport disconnect or server drain cancels all contexts immediately.
- Idle expiry closes only the expired context using close-context semantics.
  When no contexts remain, an idle connection closes normally. Initialization
  has the same five-second deadline as single-context TTS.

The authenticated connection authorizes subsequent contexts. Initialization keys
may be repeated on the first message of a new context, then are discarded.
Credentials and voice/generation settings cannot be changed on an active context.
All explicit credential precedence and token replay rules remain unchanged.

## Ownership and output

One coordinator owns context identity, input state, deadlines and socket writes.
Each context has a serial generation worker and a single buffered output event;
the shared encoder boundary is also used by single-context TTS. Ready contexts
are visited round-robin. A producer waiting for text cannot block another ready
context. Input control is checked before publishing another audio event.

Each context retains the bounded four-generation/sixteen-fragment queues; all
HTTP and WS work shares the eight-encoder admission limit. Closing an unflushed
generation marks its output invalid before canceling its input. The coordinator
checks that marker again when dequeuing, preventing already-buffered stale output
from being published after the close is processed. Bytes already sent to a client
cannot be withdrawn. Retiring workers are joined before their admission is freed.

Worker failures produce a bounded terminal protocol error, then all connection
work is canceled and joined. Unknown fields and malformed messages fail explicitly.
The remaining optional-field and compressed-alignment semantic matrix belongs to
checkpoint H; this lifecycle implementation does not establish full API parity.

## Evidence

Initial multi-context tests failed 401/404 before the route existed. The first
implementation lost utterance tails because source EOF was confused with explicit
cancellation. A separate abort marker fixed that failure. A dedicated credential
test then exposed rejection of credentials on a new context; that documented
initialization case now passes without allowing mutation of an existing context.

Go race tests prove independent early output, flush-close preservation, immediate
ID reuse, empty keepalive, all-context socket flush, five-context rejection, token
replay, idle closure, independent deadline refresh, fair ready-output rotation,
capacity errors and disconnect/drain of two live MP3 workers. Both TTS routes have
initialization deadline and synthesis-capacity checks. Full race suite and vet pass.

`contracts/multi_tts_smoke.py` sends SDK payload types and validates each response
against both SDK types and the pinned AsyncAPI. The pinned Python package has no
high-level multi-context transport client; the runner uses an ordinary WebSocket
connection and does not claim one. It covers token-only cross-origin connection,
early E audio, independent T flush, cancellation, reuse and normal socket close.

Chrome 154 passes native cross-origin interruption: E emits 1,920 PCM bytes before
cancellation; the new T context emits 32,640 bytes. Each receives one final, with
no audio after its final. The read-only production image passes this browser test,
single/multi WS SDK/schema checks and SIGTERM with STT, single-context PCM/MP3 and
two multi-context MP3 encoders active. It exits 0 without OOM or filesystem changes.
Hosted Railway behavior remains unverified.
