# Railway deployment

Deploy this standalone Go repository. The Dockerfile builds a static binary in
a pinned Go build image and copies it into a nonroot scratch image. No codec
packages, shell, database, volume or separate infrastructure SDK is required.

## Verify the image locally

```sh
docker build -t morsecode-audio:local .
docker run -d --name morsecode-audio-local --read-only \
  --memory=512m --cpus=2 -e PORT=9876 -e GOMEMLIMIT=256MiB \
  -p 127.0.0.1:8787:9876 morsecode-audio:local
curl --retry 10 --retry-connrefused --retry-delay 1 --fail http://127.0.0.1:8787/readyz
go run ./cmd/smoke -base-url http://127.0.0.1:8787
docker stop --timeout 10 morsecode-audio-local
docker inspect --format '{{.State.ExitCode}}' morsecode-audio-local
docker rm morsecode-audio-local
```

Use another free host port when needed. The different injected container port
verifies that the app reads `PORT`. Expect smoke success and exit code 0.
The exec-form entrypoint receives SIGTERM directly; the server becomes unready,
closes STT sockets and drains HTTP with a five-second deadline. Active work is
lost on restart, with no retained requests to recover.

## Deploy

1. Connect `HashNuke/morsecode-audio`, branch `main`, in Railway. Build from
   the repository root. Railway [detects the Dockerfile](https://docs.railway.com/builds/dockerfiles);
   keep its entrypoint without an extra start command.
2. In the service settings, use one replica, a 512-MiB memory limit initially,
   `/readyz` as the deployment healthcheck, a 30-second healthcheck timeout and
   an on-failure restart policy with three retries. These are suggested settings
   for this app, not automatic repository provisioning.
3. Set `HOST=0.0.0.0`, `GOMEMLIMIT=256MiB` and
   `RAILWAY_DEPLOYMENT_DRAINING_SECONDS=10`. Let Railway supply `PORT`.
   Its [drain setting](https://docs.railway.com/deployments/deployment-teardown)
   gives the binary time to shut down after SIGTERM.
4. Deploy and confirm readiness. Generate a public HTTPS domain and run the same
   smoke command with that origin. It also derives WSS and checks recognition.
5. Add `morsecode.vxpipe.com`. Copy the CNAME and TXT records Railway
   supplies and wait for certificate issuance. Railway's
   [public networking guide](https://docs.railway.com/networking/public-networking)
   describes this setup. Run:
   `go run ./cmd/smoke -base-url https://morsecode.vxpipe.com`.

The [healthcheck](https://docs.railway.com/deployments/healthchecks) gates a
deployment; it is not continuous uptime monitoring. All endpoints are public.

## Switch Vxpipe

Set `MORSE_CODE_API_BASE_URL` in **Vxpipe's runtime environment**, not just on
the Go service. Locally it is `http://127.0.0.1:8787`; after deployment use the
verified public HTTPS origin, without `/v1` or Railway's internal port.

Drain calls and restart the existing Vxpipe process using its normal process
manager. Keep the saved `morse_code_api` service, arbitrary credential and
published call spec. Probe the service and run a configured call to verify speech.
For containerized Vxpipe, localhost refers to that container; choose an address
reachable from it when using the local Go server.

On failure, restore the previous Vxpipe origin and restart. Redeploy the previous
verified Go revision in Railway if needed. Record the Go/Vxpipe revisions and
tested origin alongside the smoke result. Local testing does not establish hosted
availability; hosted checks stay pending until the user deploys.

Troubleshooting: a failed healthcheck usually requires checking HOST/PORT and
startup logs; wrong sample format/rate yields
422 or an STT input error; a failed WSS connection requires checking the selected
HTTPS domain and its certificate. The app does not log request bodies or keys.
