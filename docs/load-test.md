# Load testing

Run the committed Go load client through `bin/loadtest`. It uses the existing
WebSocket dependency and needs only Bash and Go 1.26.1 from `.tool-versions`.
No server-only benchmark routes, database, Python packages or load-test service
are required. The test runs on your computer against an explicit target.

## Run

From the repository root:

```bash
bin/loadtest https://morsecode.vxpipe.com > /tmp/morse-loadtest.json
```

Defaults: four concurrent clients, a ten-second request launch window for each
of four sequential scenarios, and a ten-second timeout per operation. Each
scenario drains its in-flight work before the next starts, so total runtime can
exceed forty seconds. First invocation also compiles the client.

Flags go before the URL:

```bash
bin/loadtest -concurrency 8 -duration 20s \
  https://morsecode.vxpipe.com > /tmp/morse-loadtest-8.json

bin/loadtest -scenario realtime-stt -concurrency 4 -duration 15s \
  -text 'SOS TEST 123' https://morsecode.vxpipe.com

bin/loadtest -concurrency 2 -duration 2s http://127.0.0.1:8787
```

Alternatively build a standalone client, useful where Go is not installed:

```bash
go build -o /tmp/morse-loadtest ./cmd/loadtest
/tmp/morse-loadtest -concurrency 4 https://morsecode.vxpipe.com
```

Concurrency is bounded to 1–128, launch windows to 1–60 seconds per scenario,
timeouts to 1–30 seconds, and input text to thirty seconds of synthesized audio.
Ctrl-C cancels in-flight work and prevents new requests. Redirects are rejected;
the target must be an HTTP(S) origin without credentials, paths or query strings.
There are no automatic retries. A slower server naturally reduces request rate
because each worker waits for its current operation before starting another.

## Workload and correctness

Every request uses `SOS` by default: 2.46 seconds / 78,720 bytes of mono PCM16
at 16 kHz, including its silence tail. The client generates this fixture once
locally, outside measured requests, using the committed Morse encoder.

| Scenario | Operation and success check |
| --- | --- |
| `tts` | Stream `/v1/text-to-speech/morse/stream`; check content type, complete byte count and SHA-256 against the local fixture. Consume audio incrementally. |
| `batch-stt` | Upload the fixture to `/v1/speech-to-text`; require the exact expected transcript. |
| `realtime-tts` | Open a fresh WebSocket; initialize, send text character by character, then EOS while reading audio concurrently. Require exact PCM byte count/hash and `isFinal`. |
| `realtime-stt` | Open a fresh WebSocket, await `session_started`, send 640-byte PCM chunks, read events concurrently, and manually commit the final chunk. Require cumulative partials and the exact committed transcript. Close the socket after each turn. |

HTTP connections are reused. Each realtime operation includes a fresh WebSocket
handshake and one turn. Without `-pace`, input is sent as fast as the network
allows. With `-pace`, STT sends 20-ms chunks at audio speed; TTS sends one
character per 100 ms, as an incremental text source might. Audio is always read
as soon as it arrives. TTS measures generation, not speaker playback duration.
Neither mode simulates many-turn hour-long connections. Scenarios
run separately; this is not a mixed-workload benchmark. The fixture shares the
production encoder, so these checks detect transport/corruption/regression
failures, not independent conformance with the Morse standard.

## Report

Progress goes to stderr; stdout is one JSON report with UTC start time, target,
text, audio duration, concurrency, pacing, launch window, timeout and per-scenario data:

- `attempts`, `success`, `capacity_429`, `errors`, and categorized `error_kinds`.
- `elapsed_seconds` includes draining operations already in flight.
- `success_per_second` divides successful operations by that full elapsed time.
- `latency_ms` gives nearest-rank p50/p95/p99/max for successful operations only.
- `first_byte_or_partial_ms` measures first PCM byte for TTS, and first nonempty
  partial including connection setup for realtime STT; first decoded audio for
  realtime TTS. It is zero for batch STT.
- `peak_realtime_sessions` counts overlapping upgraded client connections;
  this is a client observation, not a server-side occupancy metric.

Exit status is zero only if every selected scenario has successes and no errors
or capacity rejections. Nonzero also covers cancellation and invalid options.
If piping through `tee`, use `set -o pipefail` to retain the failure status.
Rejected requests are not mixed into success latency percentiles. Always read
the error/rejection counts beside latency; low success latency alone is not a
healthy result. Empty percentiles are zero, not evidence of zero latency.

This is a bounded closed-loop test. It includes client/network/Cloudflare/Railway
latency, initial connections and client-side validation. It does not measure
origin-only CPU time or prove maximum service capacity. Compare runs from the
same location with the same workload. On the 0.5-vCPU / 125-MB instance, start
with four clients; correlate higher concurrency with Railway CPU, memory,
readiness and restarts before increasing further. A 429 can be deliberate
admission control, not a crash. Slow responses should be investigated before
raising timeouts or resources.

After each run, confirm the normal speech boundary remains healthy:

```bash
go run ./cmd/smoke -base-url https://morsecode.vxpipe.com
```

## Realtime capacity checks

The server admits 64 STT sockets and eight TTS sockets/HTTP requests combined.
A connected TTS socket holds a slot even when waiting for text. Test below those
limits first, then at the limit; a deliberate above-limit run should return 429s.
Do not raise the service limits or instance size just to hide failed checks.

```bash
bin/loadtest -scenario realtime-stt -pace -concurrency 8 -duration 10s \
  https://morsecode.vxpipe.com
bin/loadtest -scenario realtime-stt -pace -concurrency 32 -duration 10s \
  https://morsecode.vxpipe.com
bin/loadtest -scenario realtime-stt -pace -concurrency 64 -duration 10s \
  https://morsecode.vxpipe.com
bin/loadtest -scenario realtime-tts -pace -concurrency 4 -duration 10s \
  https://morsecode.vxpipe.com
bin/loadtest -scenario realtime-tts -pace -concurrency 8 -duration 10s \
  https://morsecode.vxpipe.com
```

For explicit rejection checks use 65 STT or nine TTS clients. Those runs exit
nonzero when 429s occur, by design. Small differences between client connection
lifetimes and server cleanup can also produce brief 429s during turnover at the
configured limit. Read successes, rejection counts and errors separately.
Paced STT total latency includes the 2.46-second SOS input; first-partial latency
includes the Morse letter gap. Neither is solely server processing time.

## Verify the load client

```bash
bash -n bin/loadtest
go test -race ./cmd/loadtest
go vet ./...
go test ./...
```

Local tests exercise the real server through loopback HTTP/WebSockets, reject
corrupt speech responses, distinguish 429s, verify summary arithmetic and ensure
cancellation stops new work. Hosted runs are explicit commands, never part of
the default test suite.
