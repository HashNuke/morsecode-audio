# Load testing

The downloadable binary includes the load client. Pick one of four services and
an explicit HTTP(S) base URL:

```sh
morsecode-audio loadtest realtime-stt --concurrency 32 --duration 90s \
  https://morsecode.vxpipe.com
morsecode-audio loadtest realtime-tts --concurrency 8 --duration 90s \
  https://morsecode.vxpipe.com
morsecode-audio loadtest stt --concurrency 4 --duration 10s \
  https://morsecode.vxpipe.com
morsecode-audio loadtest tts --concurrency 4 --duration 10s \
  https://morsecode.vxpipe.com
```

Use `./morsecode-audio` if it is in the current directory rather than PATH.
On Windows use `morsecode-audio.exe`. No Go, Python or Bash is needed to run the
released load client. For development, `bin/loadtest` runs the same Go package:

```sh
bin/loadtest realtime-stt --concurrency 4 --duration 60s \
  http://127.0.0.1:8787
```

## Sustained realtime concurrency

By default, each realtime worker opens **one WebSocket**, keeps it open for at
least `--duration`, and performs repeated turns on that same socket. A failed or
rejected connection ends its worker; it is never replaced. This prevents rapid
reconnections from concealing an inability to hold the requested concurrency.
The duration starts after the session initializes. Workers may start a little
apart due to handshake latency; peak overlap and the shortest successful session
are reported separately. The final turn completes before closing, so elapsed
time can exceed the requested duration by a handshake and final turn/close.

STT sends 640-byte / 20-ms PCM chunks at audio speed, validates cumulative
partials and the exact committed transcript, then starts another turn. TTS sends
text one character per 100 ms, flushes each utterance, and validates its complete
PCM count/hash while reading concurrently. It sends EOS only when the session
ends and requires `isFinal`. TTS audio is read immediately; this measures
synthesis transport, not slow speaker playback. Both directions stay active
throughout the hold period rather than merely opening idle connections.

The default configuration admits 64 STT sockets and eight TTS sockets/HTTP requests
combined. A connected TTS socket holds its slot even while waiting for input.
Start below those limits, then at them. A 65-STT or nine-TTS test should show
capacity rejections. For a hardware-capacity experiment, the server owner can
explicitly raise `MAX_STT_CONNECTIONS` and `MAX_TTS_CONCURRENCY` (positive
integers). Otherwise a
1,000-client run measures admission rejection rather than machine capacity.
Keep resources fixed, record the temporary limits, and restore normal limits
afterwards. Do not silently raise limits during an ordinary test.
For 60/90-second acceptance, read `success`, `errors`, `capacity_429`,
`peak_realtime_sessions` and `shortest_successful_session_seconds` together.

HTTP `tts` and `stt` use a closed loop of requests for the duration, reusing HTTP
connections. Each worker waits for its request before starting another.
`stt` is the batch upload service (`batch-stt` remains an alias).

## Options

Flags go after the service and before the URL; both `-flag` and `--flag` work.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--concurrency` | 4 | Positive worker count. Realtime workers each own one connection. |
| `--duration` | 10s | Realtime session hold / HTTP request-launch window, 1s–10m. |
| `--timeout` | 10s | Each handshake or turn deadline, 1s–2m; independent of hold duration. |
| `--text` | SOS | Text per turn; limited to 30 seconds of synthesized audio. |
| `--pace` | true | STT at audio speed; TTS one character per 100 ms. Use `--pace=false` for bursts. |
| `--reconnect` | false | Opt into fresh realtime connections per turn during the launch window. |

The optional `all` service runs four scenarios sequentially. `--scenario` is
retained for existing scripts. `--help` prints the full CLI reference. If a longer
text takes longer than the default turn deadline, increase `--timeout` explicitly.
Ctrl-C cancels all workers and closes their sockets. Redirects and implicit
origins are rejected. The URL must have no credentials, path, query or fragment.
There are no automatic retries, and no server-only benchmark routes or storage.

## Searching for capacity

With admission explicitly configured to permit the experiment, start at 1,000
clients for 30 seconds. Test services separately. A passing stage must have all
requested original sessions succeed, no errors/429s, peak concurrency matching
the request, and a shortest successful session of at least 30 seconds. Each turn
must complete within `--timeout` (10 seconds by default). Report first-response
latency alongside failures; a stricter latency target can lower the usable limit.

If a stage passes, increase by 1,000 until the first failure. Then test midpoints
between the passing and failing counts until the bracket is useful (for example
within 32 sessions). Recheck the highest passing count before reporting it.
Raise the server's admission setting before testing above its current cap;
429s measure admission policy, not hardware capacity. Keep instance resources
fixed. Report observed passes/failures, not a precise universal maximum.
Network, proxy limits and load-generator limits also affect results.

```sh
morsecode-audio loadtest realtime-stt --concurrency 1000 --duration 30s \
  https://morsecode.vxpipe.com
morsecode-audio loadtest realtime-tts --concurrency 1000 --duration 30s \
  https://morsecode.vxpipe.com
```

Watch readiness, restarts, CPU and memory between stages. If the service does
not recover, stop the search and restore its normal admission settings.

## Correctness and reports

The default SOS fixture is 2.46 seconds / 78,720 bytes at mono PCM16, 16 kHz,
including final silence. Generate it once locally outside measured work.
TTS checks content type where applicable and exact byte count/SHA-256; STT
requires the expected text. The fixture uses the production encoder, so this
checks transport correctness rather than independent Morse conformance.

Progress goes to stderr; stdout is a JSON report:

```sh
morsecode-audio loadtest realtime-stt --concurrency 32 --duration 90s \
  https://morsecode.vxpipe.com > loadtest.json
```

- `attempts`, `success`, `capacity_429`, `errors` count sessions in persistent
  realtime mode, or requests/turns in HTTP/reconnect mode.
- `completed_turns` counts validated realtime turns, including those completed
  before a session later fails. It is not a successful-session count.
- `peak_realtime_sessions` counts overlapping initialized client connections.
  It is not a server-side occupancy metric.
- `shortest_successful_session_seconds` measures the shortest completed held
  session after initialization. It must meet the requested duration.
- `elapsed_seconds` includes draining; `success_per_second` is session completion
  rate in persistent mode, not turn throughput.
- `latency_ms` is total successful session/request duration (including the hold
  period), with nearest-rank p50/p95/p99/max. Rejections/errors are excluded.
- `first_byte_or_partial_ms` measures initial audio/partial arrival including
  handshake; it is zero for batch STT. It is not origin-only processing time.

Exit zero requires successes and no errors/rejections in every selected phase.
Deliberate above-limit tests exit nonzero for 429s. Cancellation and invalid
options are also nonzero. Empty percentiles are zero, not zero-latency evidence.
The report includes UTC start, target, duration, timeout, pacing and reconnect
mode. Tests include network/Cloudflare/Railway latency. Isolated STT and TTS runs
do not establish performance under simultaneous mixed load. Slow readers,
longer texts and noisy inputs need their own runs. Correlate with Railway CPU,
memory, readiness and restarts without changing resource limits automatically.

## Verification

```sh
go test -race ./internal/loadtest
go vet ./...
go test ./...
go run ./cmd/smoke -base-url https://morsecode.vxpipe.com
```

Tests exercise loopback speech, corruption, rejection classification, repeated
turns on the original sockets, hold duration and cancellation. Hosted loads are
explicit commands, never part of the default test suite.

## Hosted results — 2026-10-15

These initial short runs used fresh connections per turn (equivalent to
`--reconnect` in the current client); they are not sustained-session evidence.

Tested API revision `49c901f` at `https://morsecode.vxpipe.com`, one Railway
Singapore replica limited to 0.5 vCPU / 125 MB. Each stage used `-pace`, `SOS`,
a ten-second launch window and ten-second operation timeout. The STT fixture is
2.46 seconds; TTS input arrives one character per 100 ms. Each operation opens
a new WebSocket. All stages ran sequentially, not as a mixed workload.

| Scenario | Clients | Peak sockets | Successes | HTTP 429 | Other errors | Total p95 ms | First event p95 ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| realtime-stt | 8 | 8 | 32 | 0 | 0 | 3227.9 | 1247.2 |
| realtime-stt | 32 | 32 | 128 | 0 | 0 | 3261.5 | 1281.2 |
| realtime-stt | 64 | 64 | 256 | 0 | 0 | 3307.3 | 1316.2 |
| realtime-tts | 4 | 4 | 48 | 0 | 0 | 1158.5 | 733.4 |
| realtime-tts | 8 | 8 | 92 | 0 | 0 | 1206.7 | 763.1 |
| realtime-stt | 65 | 64 | 256 | 27 | 0 | 3310.2 | 1330.4 |
| realtime-tts | 9 | 8 | 93 | 18 | 0 | 1242.8 | 778.3 |

The short runs demonstrate 64 overlapping STT sessions and eight overlapping
TTS sessions for this paced workload. The above-limit stages reached the same
admission caps and rejected excess connections with 429, without protocol or
transcript/PCM failures. Every post-stage HTTP/realtime smoke check passed.
No resource settings or admission limits were raised.

Latency includes client/network/TLS setup and deliberate input pacing. These
are not origin-only processing times, an hour-long soak, a noisy-audio test,
or evidence that 64 STT plus eight TTS sessions can run simultaneously with the
same latency. Longer texts, slower readers and mixed traffic need separate runs.

Railway's one-hour window sampled every 30 seconds after these runs reported
maximum CPU usage 0.0511462 and maximum memory 0.013860864 GB. That broad window
includes idle time and deployment activity, so these are context, not an isolated
benchmark peak or a sizing guarantee. The service remained ready afterwards.

## Sustained baseline — 2026-10-15

Before raising admission limits, the v0.1.1 client held original sockets against
API revision `8cd8d57`, separately for each service, with the same 0.5-vCPU /
125-MB instance and paced SOS workload. No connections were replaced.

| Service | Original sessions | Completed turns | Shortest successful hold | Errors / 429s |
| --- | ---: | ---: | ---: | ---: |
| realtime-stt | 64 | 2238 | 90.045 s | 0 / 0 |
| realtime-tts | 8 | 1739 | 90.251 s | 0 / 0 |

These demonstrate sustained operation at the original configured limits, not
hardware saturation. Both post-run smoke checks passed. The subsequent
1,000-client search temporarily raises admission to distinguish physical /
end-to-end capacity from those default caps.

## Expanded 90-second exploration — 2026-10-15

With API/client revision `8533195` and temporary admission of 1,000 per
direction, the earlier 90-second search produced the following. Instance
resources stayed at 0.5 vCPU / 125 MB and services were tested separately.

| Service | Clients | Successful sessions | Errors | Completed turns | Minimum successful hold |
| --- | ---: | ---: | ---: | ---: | ---: |
| realtime-stt | 1000 | 0 | 1000 | 0 | — |
| realtime-stt | 532 | 0 | 532 | 567 | — |
| realtime-stt | 298 | 298 | 0 | 10277 | 90.003 s |
| realtime-stt | 415 | 415 | 0 | 13695 | 90.816 s |
| realtime-stt | 473 | 473 | 0 | 14576 | 90.000 s |
| realtime-stt | 502 | 502 | 0 | 14618 | 90.007 s |
| realtime-tts | 1000 | 1000 | 0 | 40233 | 90.296 s |

No stage returned HTTP 429. The two failed STT stages lost their connections
and process startup logs confirmed restarts; readiness recovered afterwards.
The user subsequently requested the 30-second stepped/binary search below.
A 30-second pass is not interchangeable with a 90-second endurance result.

## Stepped 30-second search — 2026-10-15

API and client revision `7bfdace` (v0.1.3), public target
`https://morsecode.vxpipe.com`, one Railway Singapore replica, unchanged
0.5-vCPU / 125-MB limits. Admission was temporarily 10,000 per direction so it
would not constrain the search. Services ran separately with paced SOS, original
sockets, 30-second minimum individual holds and a 10-second handshake/turn
deadline. No retries or replacement connections.

The search started at 1,000 clients for each service. STT failed there, so its
next count was 500; TTS passed and advanced to 2,000 before binary narrowing.
We stopped within 32 clients and repeated the highest passing count.

| Service | Clients | Result | Successful sessions | Errors | Completed turns | First event p95 | Minimum successful hold |
| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: |
| realtime-stt | 1000 | Fail | 0 | 1000 | 0 | — | — |
| realtime-stt | 500 | Pass | 500 | 0 | 5500 | 1.920 s | 30.132 s |
| realtime-stt | 750 | Fail | 0 | 750 | 0 | — | — |
| realtime-stt | 625 | Fail | 0 | 625 | 2 | — | — |
| realtime-stt | 562 | Fail | 0 | 562 | 566 | — | — |
| realtime-stt | 531 | Pass | 531 | 0 | 6372 | 1.841 s | 31.562 s |
| realtime-stt | 531 | Pass (repeat) | 531 | 0 | 6372 | 1.814 s | 31.564 s |
| realtime-tts | 1000 | Pass | 1000 | 0 | 22187 | 2.540 s | 30.213 s |
| realtime-tts | 2000 | Fail | 1999 | 1 | 17589 | 6.555 s | 30.266 s |
| realtime-tts | 1500 | Pass | 1500 | 0 | 20637 | 3.942 s | 30.215 s |
| realtime-tts | 1750 | Pass | 1750 | 0 | 19325 | 5.999 s | 30.292 s |
| realtime-tts | 1875 | Fail | 1810 | 65 | 17252 | 5.497 s | 30.172 s |
| realtime-tts | 1812 | Pass | 1812 | 0 | 19048 | 5.832 s | 30.197 s |
| realtime-tts | 1843 | Fail | 1823 | 20 | 18175 | 5.815 s | 30.212 s |
| realtime-tts | 1812 | Pass (repeat) | 1812 | 0 | 19729 | 5.540 s | 30.285 s |

**Repeated passing counts: 531 STT and 1,812 TTS.** Nearest observed failures:
562 STT and 1,843 TTS. Every stage had zero HTTP 429s. These are observed
zero-error counts under the stated ten-second deadline, not universal maximums
or a promise of low latency. TTS first audio reached 5.540 seconds p95 on the
1,812-client repeat; STT first partial was 1.814 seconds p95 at 531. A stricter
latency requirement would need a lower count and its own search.

STT overload lost all sessions and caused process restarts. After four failed
stages Railway marked the deployment CRASHED; the search stopped while the
same revision was redeployed, then resumed at 531. Both 531-client stages
passed after recovery. TTS failures were read failures/timeouts affecting a
subset of sessions; no additional process restart appeared in its runtime logs.
Failure counts were not monotonic (2,000 had fewer failures than 1,875), so the
31-client brackets describe these observations rather than a deterministic edge.

The four-core load generator reached about 2.5 CPU cores during TTS startup
and about 238 MB resident memory in a later stage. Railway CPU/memory metrics
were inspected throughout; their one-hour aggregates span prior tests and
deployments and cannot isolate an individual stage or prove a crash cause.
Configured limits remained 0.5 vCPU / 125 MB. First-event latency includes
connection setup, Cloudflare/Railway/network time and deliberate text/audio
pacing. The minimum hold applies to each original socket; initialization is
staggered, so the whole peak population does not necessarily overlap for the
entire 30 seconds. These are active clean-Morse tests, not idle connections,
mixed STT+TTS traffic, slow consumers, or a long endurance guarantee.
