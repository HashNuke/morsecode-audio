# HTTP audio output

The four HTTP TTS routes use the same bounded Morse source. Nonstreaming convert
returns binary audio; `/stream` flushes each audio chunk. The two
`/with-timestamps` variants return base64 with original and normalized alignment.
Nonstreaming timing is one JSON object written incrementally without buffering
the complete audio; timing streaming is newline-delimited JSON, not SSE.

All 28 captured nonstreaming output formats work. The default is
`mp3_44100_128`. The streaming enum has 21 formats and excludes WAV. PCM/WAV are
rendered directly; compressed/G.711 encodings use FFmpeg through input/output
pipes, with only the pipe protocol enabled. No filenames, network sources or
caller-controlled command arguments reach the codec process. CI installs FFmpeg;
the Docker image pins FFmpeg 5.1.9, LAME 3.100 and Opus 1.3.1 on a digest-pinned
Debian base. Its actual HTTP output passes all 49 binary-format cases with an
independent host decoder, alongside the pinned SDK checks.

| Format | Wire representation | Content type |
| --- | --- | --- |
| PCM | Mono signed 16-bit little-endian, no header | `audio/pcm` |
| WAV | RIFF/WAVE mono PCM16 with known sizes | `audio/wav` |
| MP3 | CBR MPEG layer III, no ID3/Xing seek header | `audio/mpeg` |
| Opus | Ogg Opus, 20-ms frames/pages | `audio/ogg` |
| mu-law | Raw 8-kHz G.711 bytes | `audio/basic` |
| A-law | Raw 8-kHz G.711 bytes | `audio/pcma` |

Alignment uses absolute seconds on the synthesized PCM clock for the request.
Each supported character covers its Morse marks; a normalized space covers the
seven-unit word gap. Repeated original whitespace divides that gap evenly.
Trimmed leading/trailing whitespace gets zero duration at the adjacent boundary.
Original case/characters are preserved in `alignment`; uppercase/collapsed text
is reported in `normalized_alignment`.

Each streaming character is emitted once, when a delivered packet reaches its
end time. Chunks with no newly completed character have null alignment. Audio
bytes concatenate into a single encoded stream. Codec headers may arrive before
alignment. PCM/G.711 packet timing comes from sample counts; MP3 framing uses the
fixed encoder format; Ogg Opus timing uses granule positions and pre-skip. These
readers frame only trusted output from this server's codec, not uploaded media.
MP3 decoding without seek metadata can expose priming/padding samples; alignment
remains on the source signal clock and does not promise identical physical sample
offsets across MP3 decoders. Full behavioral compatibility review remains in H.

References: [HTTP timed-stream schema](https://elevenlabs.io/docs/api-reference/text-to-speech/stream-with-timestamps),
[FFmpeg MP3 framing](https://github.com/FFmpeg/FFmpeg/blob/n6.1/libavcodec/mpegaudiodecheader.c),
[Ogg framing](https://www.rfc-editor.org/rfc/rfc3533#section-6), and
[Opus granule/pre-skip timing](https://www.rfc-editor.org/rfc/rfc7845#section-4).

Eight HTTP synthesis slots bound active encoders. There is no waiting queue.
Requests beyond capacity receive 429 `too_many_concurrent_requests`; a 30-second
processing/write deadline bounds admitted work. Text and duration are checked
before starting the codec. Audio buffers stay fixed-size; Ogg pages have the
format's bounded page size. Every process is closed and joined on completion,
write error, deadline or cancellation. Codec stderr is discarded rather than
retaining content-derived diagnostics.

Invalid input/format produces JSON before audio headers. Missing codecs produce
503 and initial encoding failures 502. An error after streaming begins aborts
the HTTP stream; it never appends a JSON error to binary audio or a partial JSON
document. Clients must treat transport failure as incomplete synthesis.

## Verification

- Initial output tests failed on missing format/encoding APIs; HTTP tests failed
  on the explicit-PCM-only response and missing streaming route.
- Timestamp tests initially returned 404 for both routes. All timing variants now
  preserve original ` e  t ` and normalized `E T`, with independent expected
  intervals 0–0.06, 0.06–0.48 and 0.48–0.66 seconds.
- All 28 formats (and the default) independently decode with FFmpeg to non-silent
  700-Hz audio of the expected duration. All streaming packet readers preserve
  every encoded byte and monotonic packet time across the supported formats.
- An early-output test closes MP3 encoding after its first bytes and verifies
  that over 290 seconds of a 300-second input was never rendered.
- A real HTTP client disconnect releases its handler; repeated deadline write
  failures release synthesis capacity. Go's HTTP write deadlines own blocked
  socket timing; the test exercises this application's failure cleanup.
- Timed streaming found a repeated-EOF defect in process output: waiting for a
  finished process closes its pipe. A failing regression now verifies subsequent
  reads remain EOF rather than returning a closed-file error.
- Pinned official SDK 2.72.0 consumes all four routes, default MP3, PCM/Opus timed
  variants and original/normalized alignment; both timestamp schemas validate.
- Race and vet checks pass. These are local checks, not Railway or load results.

## Continuous encoding for text WebSockets

`audio.EncodeLive` accepts a cancellable, unknown-length PCM source and owns its
lifecycle. It preserves one codec stream across successive text fragments. The
source must implement `Close` that unblocks `Read`; WS producers must bound their
queues and total duration. Unlike the finite HTTP encoder, live output cannot
advertise a byte length or write a WAV header. All 21 streaming formats are covered.

The first live tests timed out for MP3/Opus/G.711: default FFmpeg input probing
waited for more audio while the producer deliberately kept its input open. For
live PCM, the format/rate/channel are already known, so a 32-byte probe and zero
analysis duration avoid that buffering. Every format now emits an actual audio
packet after at most the supplied 600 ms of PCM, before producer EOF. Independent
decoding verifies tone energy and duration; this is a pipeline test, not a latency
benchmark or evidence of completed WebSocket routes.

Closing output or cancelling its context closes the source before joining the
codec's input copier. Child failure also closes the source before waiting. Tests
cover starved input, missing codecs, rejected formats, pre-cancelled startup and
normal PCM EOF. Final cleanup initially turned repeated PCM EOF into a closed-pipe
error; retaining the terminal read result fixes it. Full race/vet and existing
finite HTTP format regressions pass. No temporary files or extra storage are used.
