# Realtime latency review — 2026-10-15

The earlier 5.540-second TTS and 1.814-second STT p95 values were measured
while opening 1,812 and 531 concurrent sessions, respectively. They include
connection setup and the first turn. They are not single-request processing
times, and they do not measure the latency of subsequent turns.

## Single-session measurements

Reviewed API revision `4b481faa7b2e52661e57d843debb7fbd964595f2` and Vxpipe
revision `0f1b9831`. The hosted service now has 0.5 vCPU and 256 MB; the earlier
capacity measurements used 0.5 vCPU and 125 MB. This is a small baseline probe,
not a controlled before/after comparison of the resource change.

For each service and origin, opened three sequential WebSockets and performed
three `SOS` turns per socket. Only one test connection was active per origin.
The probe reused the load client's `newTarget`, `openRealtime`, `ttsTurn` and
`sttTurn`, with separate timers around connection initialization and each turn.
TTS verified the exact PCM length/hash; STT verified cumulative partials and the
exact committed transcript. All 36 turns passed across local and hosted probes.

Input used the existing pacing: one TTS character every 100 ms, starting
immediately, and 640-byte STT chunks every 20 ms at 16 kHz. A turn contains
2.46 seconds of Morse audio, including its trailing silence. Connection time
includes the STT `session_started` acknowledgement; TTS has no corresponding
server acknowledgement and setup ends after writing initialization.

| Measurement | Local median (range), ms | Hosted median (range), ms |
| --- | --- | --- |
| TTS connection setup, 3 samples | 1.232 (0.942–1.891) | 289.592 (262.749–311.205) |
| TTS text to first audio, 9 turns | 0.595 (0.327–2.004) | 184.229 (174.834–201.574) |
| TTS cold connection to first audio, 3 samples | 1.763 (1.537–2.642) | 491.166 (446.977–495.818) |
| STT connection setup, 3 samples | 0.830 (0.776–2.166) | 255.945 (255.356–554.069) |
| STT audio start to first partial, 9 turns | 481.104 (480.529–481.455) | 650.684 (647.639–653.474) |
| STT cold connection to first partial, 3 samples | 481.917 (481.627–482.696) | 908.147 (903.709–1205.212) |

The established-connection rows include the first turn and two subsequent
turns on each socket, timed after setup. These small samples do not establish
a production p95 or latency guarantee. Network results are specific to this
test client's route to `https://morsecode.vxpipe.com`. Local testing used the
same source on an AMD EPYC host without Railway's resource quota; a short codec
benchmark overlapped the beginning of the probe. No artificial saturation load
was running against the hosted service.

The approximately 170–185 ms difference between local and hosted first-event
times is consistent with network/proxy round-trip overhead. Server-side traces
were not collected, so this does not isolate Cloudflare, Railway, routing or
server scheduling individually. A nearer deployment or running the binary
alongside its caller should be evaluated using the caller's actual location.

## Review findings

### 1. The capacity report cannot establish steady-state response latency

[`holdSession`](../internal/loadtest/session.go) starts its timer before dialing
and stores only the first turn's first event. Later turns contribute to the
success count but have no recorded first-event distribution. `latency_ms` is
the whole session duration, including the deliberate hold period.

Consequently, a stage can pass the current 10-second deadline while providing
poor interactive latency. The existing figures establish successful sustained
work under that acceptance rule; they do not establish a low-latency operating
limit. Failed sessions also do not contribute to successful latency percentiles.

Recommended change: retain the existing end-to-end metric, add connection setup
and per-turn first-event/completion distributions, and report failures alongside
them. Choose an interactive latency target before repeating capacity search.
Avoid retaining every turn sample indefinitely; use a bounded histogram or
bounded sampling if these measurements are added to the load client.

### 2. Vxpipe pays TTS connection setup for every utterance

In Vxpipe, `apps/vxpipe_call_engine/lib/vxpipe/providers/morse_code_api/tts_request.ex`
opens a socket in `perform/3`, sends initialization, text and EOS in
`send_input/2`, then closes the socket. Thus a sequence of utterances repeatedly
pays TCP/TLS/WebSocket setup, even though the API supports multiple flushed
utterances on one connection. This does not cause the held-session load test's
later-turn latency; it affects actual Vxpipe use.

Keeping a socket per call/session could avoid repeated setup, which cost
263–311 ms for TTS in this probe. That is an opportunity, not a measured Vxpipe
speedup. Preserve cancellation, stale-audio rejection, backpressure and correct
utterance completion. The existing request-owned socket deliberately makes
cancellation close the connection; sharing it requires lifecycle changes.
There is no need for a global connection pool.

### 3. STT copies unused timestamp history on every partial

[`Decoder.letter`](../internal/morse/decoder.go) copies the complete character
history into each partial event. [`realtimeSTT`](../internal/api/realtime_stt.go)
uses only the partial text and uses character timestamps only for final events.
The batch consumer also uses final events. History copying therefore creates
quadratic allocation growth as a transcript becomes longer.

A local diagnostic benchmark created a decoder for each utterance and fed it
640-byte chunks without wall-clock pacing. Input was repeated `E` at the default
profile, with input encoding outside the timed section:

| Letters | Decode time | Allocated bytes per utterance |
| --- | --- | --- |
| 3 | 0.051 ms | 1,318 |
| 100 | 1.483 ms | 198,307 |
| 1,000 | 23.370 ms | 17,617,018 |

These are cumulative allocations, not retained memory or per-session RSS. The
1,000-letter signal lasts about 241 seconds. Allocation profiling attributed
about 876 MB of 1,040 MB allocated across the mixed benchmark suite to the
partial-event construction line, including its history copy. This confirms an
avoidable allocation hotspot; it does not explain the earlier short-`SOS`
capacity failures by itself.

Recommended change: keep timestamp history for finals, omit its unused copy
from partial events, and preserve timestamp/partial protocol tests. Measure
allocation reduction before considering broader decoder changes.

### 4. TTS does repeated waveform computation and frame allocation

[`Encoder.Read`](../internal/morse/encoder.go) calculates sine and rounding for
each tone sample. [`writeTTSAudio`](../internal/api/realtime_tts.go) allocates a
buffer per fragment and encodes a base64 string plus a map for each audio frame.
These are throughput optimization candidates when many streams are active.

Local codec benchmarks measured encoder construction plus the first 4,096-byte
frame at **0.024 ms**, and complete `SOS` rendering at **0.290 ms**, each with
about 1.4 KB of allocations. CPU profiling confirmed work in the encoder loop,
sine and rounding. These costs are far below the hosted single-turn network
time; optimizing them will not remove hundreds of milliseconds of routing or
connection overhead.

If server profiling under load identifies this path as a bottleneck, consider
reusing per-connection frame buffers, typed response structs, and bounded
precomputed dot/dash PCM for supported profiles. Preserve exact samples,
phase resets, sample rates and cancellation behavior. Avoid whole-utterance
caching or persistence: neither is needed for this optimization.

## STT timing that should remain intentional

At 20 WPM, `S` is three 60-ms dots plus two 60-ms internal gaps: 300 ms.
The decoder then waits for the 180-ms letter gap before publishing a stable
character, giving a **480-ms signal-time floor** for this particular first
letter. Other characters have different durations. Partials are cumulative
text emitted as characters are recognized, not word-only updates.

The 840-ms tail affects automatic finalization/full utterance duration, not
the first partial. Manual commit can finalize when the sender knows the input
has ended. Sending already-recorded PCM without pacing also runs faster than
real time, but neither changes how long live audio takes to arrive. Publishing
unfinished-letter guesses or changing Morse speed would change behavior and
should not be disguised as an implementation optimization.

## Scope and next steps

This review changed documentation only. Temporary diagnostic test/benchmark
sources were removed after use; no new CI tests, server instrumentation or
production behavior were added. Codec profiles cover isolated codec work,
not WebSocket/JSON/GC costs of the whole server under saturation.

Prioritize measurement clarity and avoiding repeated connection setup for
interactive latency, then the unnecessary STT copies for allocation efficiency.
Profile the server before attempting broader waveform/transport changes.
When reviewing memory settings after a resource resize, remember that
[`GOMEMLIMIT` is a soft Go runtime memory limit](https://go.dev/doc/gc-guide#Memory_limit)
and does not replace the container limit; leave headroom for memory outside
Go's accounting. No heap-limit tuning or renewed capacity claim was made here.
