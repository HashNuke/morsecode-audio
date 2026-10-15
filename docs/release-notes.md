The binary now includes sustained realtime load testing:

```sh
./morsecode-audio loadtest realtime-stt --concurrency 32 --duration 90s \
  https://morsecode.vxpipe.com
```

Services: `tts`, `stt`, `realtime-tts`, `realtime-stt`. Each realtime worker keeps
one WebSocket open for the requested duration, performs repeated validated turns,
and never replaces a failed session. The report includes peak concurrency,
completed turns, shortest successful session duration, 429s and errors.
Use `loadtest --help` for pacing, timeout, text and reconnect options.

Run your own API with `./morsecode-audio serve`; the public API remains available
at https://morsecode.vxpipe.com. Choose the OS/CPU archive and verify its SHA-256
hash using `checksums.txt`. macOS and Windows binaries are unsigned. The packaged
Linux AMD64 server and load-test command are smoke-tested before publication.
