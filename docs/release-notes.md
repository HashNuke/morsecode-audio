Run the Morse audio API locally with one binary:

```sh
./morsecode-audio serve
```

Open http://127.0.0.1:8787 for documentation. Use `--host` and `--port` to choose
another listener, and `version` to inspect the installed release.

This release provides Text to Audio, Audio to Text, Realtime Text to Audio and
Realtime Audio to Text. Both realtime interfaces use ElevenLabs-shaped
WebSocket messages with the fixed Morse audio profile. No account, database or
external codec is required. The public API remains at https://morsecode.vxpipe.com.

Choose the archive matching your OS and CPU. SHA-256 hashes are in
`checksums.txt`. macOS and Windows binaries are unsigned; all targets are
cross-compiled and the packaged Linux AMD64 binary is smoke-tested before
publication.
