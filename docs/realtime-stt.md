# Realtime Morse transcription

Connect directly to `/v1/speech-to-text/realtime` from a browser or
server-side WebSocket client. The Vxpipe profile is:

```text
model_id=morse_stt_v1
audio_format=pcm_16000
commit_strategy=vad
vad_silence_threshold_secs=0.84
include_timestamps=false
enable_logging=false
```

The first event is `session_started` with the selected configuration and a
session ID. Send JSON chunks:

```json
{"message_type":"input_audio_chunk","audio_base_64":"...","sample_rate":16000,"commit":false}
```

The PCM sample rate must match `audio_format`. Supported PCM rates are
8/16/24/48 kHz. Odd sample boundaries carry across chunks. Malformed JSON, PCM,
base64 or rates produce an error and close the session. The socket library owns
WebSocket framing; the handler owns the bounded decoder.

The server emits `partial_transcript` and `committed_transcript`, each with
`text`. VAD mode here means Morse silence detection: a seven-unit word gap
does not end a turn, while a fourteen-unit utterance tail does. It is not a
human-speech detector. Empty commits and silence never create text.

Manual commit is also available with `commit_strategy=manual` and
`commit=true` in an input chunk. Optional `include_timestamps=true` adds a
`committed_transcript_with_timestamps` event with sample-derived word timing.
The default model alias `scribe_v2_realtime` performs the same Morse operation.
Unsupported query fields and duplicate parameters fail explicitly.

Each session has one decoder and no unbounded audio queue. At most 64 sockets
are admitted; messages are bounded to 512 KiB and decoded chunks to 256 KiB.
Sessions have a one-hour lifetime, a 30-second application-message idle deadline
and five-second writes. Empty control chunks can keep the connection alive
without inventing audio time. Disconnect frees the session; SIGTERM closes active
sockets without committing unfinished text. No content is retained.

```sh
go run ./cmd/smoke -base-url http://127.0.0.1:8787
```
