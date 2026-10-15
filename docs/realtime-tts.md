# Realtime Morse synthesis

Connect to `wss://morsecode.vxpipe.com/v1/text-to-speech/morse/stream-input`
with `model_id=morse_v1&output_format=pcm_16000&auto_mode=true`.
This follows the ElevenLabs single-context input-streaming WebSocket protocol:

```json
{"text":" "}
{"text":"S"}
{"text":"OS TEST","flush":true}
{"text":""}
```

Each line is a separate client message. Initialize with exactly one space.
The server starts sending audio before end-of-input; read continuously while
sending. Received messages contain base64 PCM and use ElevenLabs' camel-case
completion field:

```json
{"audio":"BASE64_PCM","isFinal":false}
{"audio":null,"isFinal":true}
```

The empty text message flushes any remaining tail, sends `isFinal: true`, and
closes normally. `flush: true` finishes the current Morse utterance, including
its 840-ms tail, but keeps the socket open. It does not send `isFinal`; that
event ends the connection. Text after a flush starts a new utterance. A space
can keep the socket alive; it also separates words when an utterance is active.
Closing the transport without the empty-text message cancels unfinished work.

## Morse behavior and deliberate differences

Text fragments concatenate literally: `S` then `OS` produces `SOS`; `SOS `
then `TEST` produces two words. Fragment boundaries never introduce a word
gap or final tail on their own. Each nonempty fragment is synthesized
immediately. `auto_mode`, `try_trigger_generation` and valid
`generation_config.chunk_length_schedule` values are accepted, but Morse has
no contextual model buffer or minimum generation threshold. A schedule does
not delay output. The configured 20-WPM timing and 700-Hz tone remain fixed.
Voice styling fields do not alter Morse; `speed` must be absent or 1.

The PCM default and supported rates are identical to HTTP TTS: 8/16/24/48 kHz,
mono signed 16-bit little-endian. Audio events hold at most 4,096 decoded bytes.
No MP3, SSML, pronunciation dictionaries, alignment output, or compressed
formats are implemented. `sync_alignment=true` is rejected explicitly.
`enable_logging` cannot enable retention. Unsupported parameters and fields
fail rather than silently invoking a different service. Multi-context
synthesis lives on the `multi-stream-input` route below.

Up to eight TTS sockets/HTTP requests share one synthesis capacity pool. A TTS
socket reserves its slot for its lifetime, including time waiting for text.
STT has a separate 64-socket pool. Idle timeout defaults to 20 seconds;
`inactivity_timeout` accepts 1–180 seconds. Sessions have a one-hour lifetime,
five-second writes and 64-KiB incoming message limits. Each utterance is limited
to 4,096 input bytes and 300 seconds of audio. Flush resets utterance limits.
HTTP admission errors precede upgrade; protocol errors after upgrade contain
`error` and `message` and close the connection. No data is persisted.

## Implementation and verification

Reuse the existing encoder for each fragment, omitting its final tail until
flush/EOS and carrying only spacing/count state across fragments. This avoids
retaining or regenerating the complete utterance. Socket writes provide
backpressure; disconnect and shutdown release both socket and synthesis slots.

Vxpipe's `MorseCodeApi.TTSRequest` uses this endpoint under its existing
supervised request session. Its `speak` contract still accepts a complete
utterance; the transport initializes, submits text and sends EOS, then consumes
incremental PCM with output credit. Only `isFinal` plus valid complete PCM can
complete the request. Cancellation kills the socket-owning task.

Focused tests cover audio before EOS, fragmented-word waveform equivalence at
all four rates, repeated flushes without extra tails, invalid initialization,
shared TTS admission, independent STT capacity, disconnect cleanup and draining.
The smoke command checks HTTP and WebSocket TTS produce identical audio. The
load client can send paced text through `-scenario realtime-tts -pace`.

Protocol sources: [ElevenLabs WebSocket reference](https://elevenlabs.io/docs/api-reference/text-to-speech/v-1-text-to-speech-voice-id-stream-input)
and [realtime TTS guide](https://elevenlabs.io/docs/eleven-api/guides/how-to/websockets/realtime-tts).

## Multi-context synthesis on one socket

Connect to `wss://morsecode.vxpipe.com/v1/text-to-speech/morse/multi-stream-input`
with the same query parameters. This follows the ElevenLabs multi-stream-input
subset: every message carries a `context_id` (1–128 characters, `default` when
absent) and every response carries the camel-case `contextId`:

```json
{"text":" ","context_id":"utterance-1"}
{"text":"SOS","context_id":"utterance-1"}
{"context_id":"utterance-1","flush":true}
{"context_id":"utterance-1","close_context":true}
```

A new context also accepts its first text immediately, without the
single-space prologue (`InitialiseContext`):

```json
{"text":"SOS ","context_id":"utterance-2"}
```

Received messages contain base64 PCM correlated to the context:

```json
{"audio":"BASE64_PCM","isFinal":false,"contextId":"utterance-1"}
{"audio":null,"isFinal":true,"contextId":"utterance-1"}
```

`flush` finishes exactly that sentence, including its 840-ms tail, but leaves
the context open: no `isFinal` is sent, and later text with the same ID
continues the same context with fresh utterance timing. Each flush also seals
its generation with a fresh cancellation token, so a later close suppresses
only post-flush text while exactly the promised flushed PCM drains, across
any number of flushes in waveform order. Only `close_context`
ends a context, with exactly one audio-free `isFinal`. Closing an unflushed
context cancels it: frames already written stand, outstanding audio is
suppressed, and the terminal still arrives. Closing an already-flushed context
sends the terminal with no duplicate tail. Closing an unknown context is an
idempotent `isFinal`, never an error, and a closed ID may initialize again as
a new, unlinked context. `{"text":"","context_id":...}` is a keepalive: it
resets that context's inactivity clock without audio or completion.
`{"close_socket":true}` lets already-flushed contexts finish, cancels
never-flushed contexts without synthesizing them, sends every terminal in
sorted ID order, then closes normally.

One socket carries at most five contexts; the sixth is rejected with
`too_many_contexts` and the socket closes. Text on an unknown context
initializes it immediately with that text (`InitialiseContext`); only a bare
flush with no text for an unknown context is a protocol error
(`unknown_context`) that closes the socket. Idle contexts
retire separately with terminals and free their slots while busier contexts on
the same socket keep working; the socket itself only closes after a full
inactivity window with no messages at all. Each context keeps its own
encoder, 4,096-byte input budget and completion; frames never cross contexts,
so late audio cannot leak into the next utterance. The handler is one reader
plus one writer with a 64-job bounded queue and 4,096-byte frames, and the
reader never writes while the writer runs: every terminal error stops and
joins the writer first, so the error is strictly the last write and audio
never arrives after it. Fragments
that synthesize to zero PCM (word-gap bookkeeping such as lone spaces, and
empty keepalives) never occupy the queue, so benign bursts keep the reader
responsive to `close_context` and cancelling a long generation never
requires draining it first. Every other enqueue (audio, direct-initialization
audio, and terminals including unknown-context terminals) is bounded
nonblocking under the same queue bound: a full queue is explicit
backpressure, so the socket receives a deterministic
`too_many_queued_messages`
error and closes instead of stalling control behind queued audio. Explicit
overload may truncate accepted flushed audio, but the truncation is always
followed by the error, never by completion. Only the graceful
`close_socket` drain intentionally waits for already-accepted flushed work.
Socket
capacity, idle/lifetime limits and error shape match the single-context
endpoint.
