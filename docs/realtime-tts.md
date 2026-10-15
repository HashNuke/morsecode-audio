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
No MP3, SSML, pronunciation dictionaries, alignment output, or multi-context
protocol is implemented. `sync_alignment=true` is rejected explicitly.
`enable_logging` cannot enable retention. Unsupported parameters and fields
fail rather than silently invoking a different service.

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
