# Realtime Morse transcription

`/v1/speech-to-text/realtime` upgrades an authenticated request to the pinned
Scribe-compatible protocol. A nonempty `xi-api-key` is required at this checkpoint;
browser token authentication belongs to E. `coder/websocket` v1.8.15 owns framing,
control messages, read limits and cancellation. The default origin check permits
same-origin browsers and clients without an Origin header; cross-origin policy
will be completed with browser authentication.

The first event is `session_started` with a random session ID and the actual
configuration. Send `input_audio_chunk` JSON with `audio_base_64`, `sample_rate`
and `commit`. The declared sample rate must match the selected input format.
Odd PCM byte boundaries carry across messages; committing an incomplete sample
fails explicitly. Binary WS messages and malformed fields/base64 fail with
`input_error` and close the session. SDK 2.72.0's null `previous_text` is treated
as absent; nonempty context remains unsupported at this checkpoint.

| Option | Supported behavior |
| --- | --- |
| `model_id` | `scribe_v2_realtime` default; `morse_stt_v1` alias |
| `audio_format` | `pcm_16000` default; PCM 8/16/22.05/24/44.1/48 kHz and `ulaw_8000` |
| `commit_strategy` | `manual` default or `vad`; turn prediction remains unsupported |
| `include_timestamps` | False default; true sends a separate timed event after the committed text |
| `language_code` | Optional two/three-letter label; otherwise `und`, with no language inference |
| `vad_silence_threshold_secs` | Morse default 0.84 seconds; configurable from 0.84 to 30 seconds |
| `keepalive_interval_ms` | Disabled by default; 500–10000 ms when requested |
| `enable_logging` | Boolean accepted; effective config always reports false, with no retention |

Other query fields fail explicitly until their Morse semantics are implemented
or resolved in H. Duplicate parameters fail validation. A 32-kHz PCM format is
available for HTTP TTS but is absent from the pinned realtime STT enum.

Partials contain the current segment's completed letters and grow by appending
characters. Manual commit finalizes the current complete signal once. Empty
commits and silence produce no final transcript. VAD mode recognizes Morse
silence rather than human speech. The seven-unit word gap stays within a segment;
the default fourteen-unit tail finalizes it. Detection resolution is one analysis
window (at most about 10 ms), including commit-threshold tolerance. This fixes
the exact-tail case at 22.05 kHz without treating word gaps as utterance endings.

Each final is one `committed_transcript`. When timestamps are enabled, a following
`committed_transcript_with_timestamps` carries the same text and word/spacing
timing. Timestamps use the session's cumulative source sample clock, including
silence across earlier commits. No decoder reset moves that clock backwards.
Deterministic codebook `logprob=0` is not calibrated acoustic confidence.

Keepalive partials are generated only while supplied silent samples advance the
audio clock. They contain empty text after commit or repeat the current partial;
they are not invented transcript content. Empty application control messages can
keep the connection active but do not manufacture sample time or transcripts.

## Bounds and lifecycle

There are 64 admitted speech sockets, including upgrades in progress. Excess
requests receive HTTP 429 before upgrade. Each message is bounded to 512 KiB and
each decoded input chunk to 256 KiB; mu-law expands to at most twice that size.
Oversized encoded audio yields `chunk_size_exceeded`; the transport ceiling closes
with WS status 1009. Signal/text duration limits remain enforced by the decoder.
The current session has at most one hour of audio and one hour of wall time,
30 seconds between application messages and five seconds for an output write.
No input or output queue grows independently of socket backpressure.

Each handler owns its decoder and emits messages serially. Malformed audio fails
the session; reconnect to start cleanly. Disconnect cancels owned work and returns
the admission slot. Server drain rejects new API requests, cancels all speech
sockets and waits for their handlers separately from HTTP shutdown. Drain is a
bounded transport close; it does not commit or recover unfinished segments.

## Verification

Initial protocol tests failed with HTTP 404 before the handler existed. Go
tests use the `input_audio_chunk` and acknowledgement fields consumed by Vxpipe's
existing `ElevenLabs.Scribe` wire module; no Vxpipe provider integration is claimed.
Independent E/T square waves validate all seven input formats, repeated manual
and automatic commits, odd byte boundaries, word gaps and timestamp origin.
The 22.05-kHz auto-commit test initially timed out at the complete fourteen-unit
tail and passed after accounting for the decoder window resolution.

A 500-character, roughly two-minute segment crosses many input chunks; concurrent
reads drain its partials while sending. Separate sockets preserve distinct text.
Malformed input, incomplete samples, size limits, empty commits, keepalives,
custom silence thresholds, admission/release, idle/lifetime expiry and server
drain pass under the race detector. The pinned official SDK recognizes two turns
from independently generated signals and every server message validates against
the captured AsyncAPI schema. Its timing checks include exact cumulative offsets.

```sh
uv run --with-requirements contracts/requirements.txt \
  python contracts/realtime_smoke.py http://127.0.0.1:8787
```

The upstream [realtime reference](https://elevenlabs.io/docs/api-reference/speech-to-text/v-1-speech-to-text-realtime)
defines the wire events; the Morse defaults, explicit limits and unsupported
semantic options above remain visible in the compatibility ledger. This is
checkpoint D, not final API or hosted acceptance.
