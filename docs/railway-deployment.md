# Railway deployment

The user deploys this separate repository. The Docker package currently serves
HTTP discovery, TTS, batch PCM/WAV STT and realtime STT. TTS WebSockets and final compatibility/load
acceptance are still in progress; use the milestone to decide when to publish the
complete service. Local container checks do not establish hosted acceptance.

## Package and configuration

The multistage Dockerfile pins Go/Debian image digests and FFmpeg, LAME, Opus and CA
package versions. It builds independently of Vxpipe and runs as UID/GID 65532.
The binary checks actual codec output before listening, binds `HOST=0.0.0.0`, and
reads Railway's `PORT` at runtime. HTTP and future WS routes share that listener.
There is no database, volume, writable upload directory or migration.

The image's exec-form entrypoint receives SIGTERM directly. Readiness becomes
false, speech sockets are explicitly canceled/joined, and HTTP shutdown has a
five-second deadline. Railway gets a ten-second drain allowance. The drain check
currently exercises realtime STT and must expand to both TTS WS modes in E.
A restart loses active work and future transient tokens.

Railway now uses [Infrastructure as Code](https://docs.railway.com/infrastructure-as-code).
Its [legacy config reference](https://docs.railway.com/config-as-code/reference)
deprecates `railway.toml`/`railway.json`, disallows them for new services, and gives
existing services a 2026-12-01 cutoff. Accordingly this repository supplies
`.railway/railway.go`, an isolated module using pinned Go SDK v0.3.0 (beta).
Its named partial manages this service only. This replaces the original plan's
`railway.toml` requirement; no legacy config file is installed.

## Verify locally

```sh
docker build -t morsecode-audio:local .
docker run --rm --name morsecode-audio-local --read-only \
  --memory=512m --cpus=2 -e PORT=9876 -e GOMEMLIMIT=256MiB \
  -p 127.0.0.1:8787:9876 morsecode-audio:local
```

Use another available host port if needed; the deliberately different container
port proves `PORT` is honored. In another terminal run the catalog, speech and TTS
scripts in `contracts/` against `http://127.0.0.1:8787`, then:

```sh
curl --fail http://127.0.0.1:8787/readyz
docker stop --timeout 10 morsecode-audio-local
```

The service must exit without SIGKILL. Repeat after restart with the same request
inputs; no previous request content should be retrievable. Maintain these checks
as WS/token handlers are introduced. Codec-pin updates require all format and SDK
checks again; if a pinned Debian package is retired, update and verify the pins
instead of silently dropping version constraints.

## Deploy the service

1. Create/select a Railway project and environment. Connect GitHub repository
   `HashNuke/morsecode-audio`, branch `main`, with repository root as the build
   context. Use the Dockerfile build and its entrypoint; no extra start command.
2. Apply the checked-in IaC using a Railway CLI version that supports Go IaC
   (the current reference documents 5.63.0), or configure the equivalent service
   settings below in the UI. For IaC, first review the project/service names in
   `.railway/railway.go`, install its pinned module dependencies, authenticate and
   link the intended project/environment. Then run `railway config plan` and
   review the changes before `railway config apply`. Do not use an unrelated
   project's resource configuration. The named partial remains stable after apply.
3. Set or verify the settings in the table. IaC is evaluated by the CLI; pushing
   this repository alone does not apply its service settings. Resource allocation,
   generated domain and custom-domain/DNS setup are separate user operations.
4. Deploy the selected revision and wait for `/readyz` to pass. Record the Git
   revision, image ID, Railway deployment ID and generated HTTPS origin.
5. Test discovery, all four HTTP TTS routes and batch STT at the generated origin.
   Run every WS/token test once those checkpoints are implemented. Use only
   synthetic compatibility keys. Inspect logs for startup/shutdown failures without
   enabling body, key or token logging.
6. Add custom domain `morse-audio.definerun.com` in Railway, copy its supplied DNS
   record to the domain provider, and wait for certificate readiness. Verify HTTPS
   and WSS at the custom origin before Vxpipe cutover. Do not guess DNS targets.

| Setting | Value |
| --- | --- |
| Builder/context | Dockerfile at repository root |
| `HOST` | `0.0.0.0` |
| `PORT` | Railway-injected value; domain target must reach this listener |
| `GOMEMLIMIT` | `256MiB` soft Go heap limit; codec processes need additional memory |
| Healthcheck | `/readyz`, 60-second startup timeout, no auth |
| Replicas | One, for transient token-consumption semantics |
| Restart | On failure, at most three retries |
| Teardown | Zero overlap; ten-second SIGTERM drain allowance |
| Initial allocation | 2 vCPU / 512 MiB for the milestone's load measurements |
| Persistent resources | None |

The memory/CPU allocation is a test target, not an accepted capacity claim.
Railway's [healthchecks](https://docs.railway.com/deployments/healthchecks) gate
deployment readiness; verify connection lifetimes and early chunks over the
actual hosted path rather than inferring them from a successful healthcheck.

## Vxpipe cutover and rollback

Provider integration is still pending. Once it is implemented, both
`MorseCodeApiTTS` and `MorseCodeApiSTT`, discovery and credential probes must use:

```sh
MORSE_CODE_API_BASE_URL=https://morse-audio.definerun.com
```

Drain Vxpipe calls, set that runtime variable and restart. Repeat configured-room
acceptance with unchanged services, synthetic keys and call specs. Switching the
origin must not require rebuilding Vxpipe or republishing call specs. Keep the
local override available through hosted verification. A failed endpoint must
report unavailability without falling back to another service.

Rollback redeploys the previous verified Go revision/image and repeats readiness
and protocol checks. If reverting Vxpipe's endpoint, restore its previous verified
origin and restart after draining calls. Active sessions do not migrate.

## Troubleshooting and pending hosted evidence

Local evidence (2026-10-14): the pinned image built successfully, ran as UID/GID
65532 with a read-only filesystem, two CPUs, 512 MiB and injected port 9876.
All 49 binary format cases passed independent MIME/duration/signal checks; the
pinned SDK passed discovery, PCM transcription and all timed TTS variants. SIGTERM
exited with status 0 without OOM, the container filesystem diff was empty, and
the PCM SDK round trip passed again after restart. The container was then removed.
The isolated IaC module compiles/vets and its rendered graph matches the table;
no authenticated Railway plan/apply or hosted verification has been performed.

Checkpoint D adds a passing SDK realtime STT check in the same read-only image.
`contracts/drain_smoke.py` opens an STT socket, stops the explicitly named local
test container and verifies socket closure plus exit status 0. HTTP TTS and batch
STT regressions also pass. The container was removed after verification. Future
TTS WebSockets still need equivalent active-connection drain checks.

- Failed startup: check codec availability/pins and listen address/port. No
  successful readiness response is served before codec checks complete.
- Healthy locally, unreachable publicly: check `HOST`, injected `PORT` and the
  domain's target port. Railway probes must reach the listener without a key.
- Missing streams or failed WS upgrade: verify the configured origin scheme and
  Railway path with the protocol fixtures; never accept a buffered curl response
  as proof of early delivery.
- Memory pressure/429: retain the bounded admission policy, record the workload
  and measure before raising limits or replicas.
- Hosted acceptance remains pending until the user deploys: HTTPS/WSS, all API
  families, configured rooms, disconnect, restart/drain and outage recovery must
  identify the exact deployed revisions and test outcomes.
