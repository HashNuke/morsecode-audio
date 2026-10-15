Persistent realtime TTS now supports multiple utterances on one WebSocket.
Use `/v1/text-to-speech/morse/multi-stream-input` with `context_id`, flush,
and explicit context completion. Closing an utterance keeps its socket open;
up to five contexts can coexist. The existing HTTP and single-context TTS
endpoints remain available.

The endpoint streams bounded PCM frames, preserves already-flushed audio,
cancels unflushed work, and rejects overload explicitly. The homepage and
`llms.txt` document initialization, completion and keepalive messages.

Download a binary below and run `morsecode-audio serve`, or use the public API
at https://morsecode.vxpipe.com. Verify archives with `checksums.txt`.
macOS and Windows binaries are unsigned.
