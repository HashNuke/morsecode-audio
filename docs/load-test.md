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
capacity rejections. For a hardware-capacity experiment, the server owner can explicitly raise
`MAX_STT_CONNECTIONS` and `MAX_TTS_CONCURRENCY` (each 1–1000). Otherwise a
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
| `--concurrency` | 4 | Workers, 1–1000. Realtime workers each own one connection. |
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
clients for 90 seconds. Test services separately. A passing stage must have all
requested original sessions succeed, no errors/429s, peak concurrency matching
the request, and a shortest successful session of at least 90 seconds.
If 1,000 fails, test the midpoint between it and a known passing count. Repeat
until the bracket is useful (for example within 32 sessions). Report the highest
observed pass and the failing bracket, not a precise universal maximum.
Network, proxy limits and load-generator limits also affect results.

```sh
morsecode-audio loadtest realtime-stt --concurrency 1000 --duration 90s \
  https://morsecode.vxpipe.com
morsecode-audio loadtest realtime-tts --concurrency 1000 --duration 90s \
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
