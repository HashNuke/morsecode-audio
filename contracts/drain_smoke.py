"""Stop the explicitly named local test container while STT and streaming TTS sockets are active."""
import argparse
import asyncio
import json
from contextlib import AsyncExitStack
from urllib.parse import urlsplit, urlunsplit

from websockets.asyncio.client import connect
from websockets.exceptions import ConnectionClosed


async def run(base_url, container):
    origin = urlsplit(base_url)
    assert origin.hostname in {"127.0.0.1", "localhost", "::1"}, "Local container test only"
    async with AsyncExitStack() as stack:
        uri = urlunsplit(("ws", origin.netloc, "/v1/speech-to-text/realtime", "", ""))
        stt = await stack.enter_async_context(connect(uri, additional_headers={"xi-api-key": "synthetic-drain"}))
        assert json.loads(await asyncio.wait_for(stt.recv(), 5))["message_type"] == "session_started"
        sockets = [stt]
        for output_format in ["pcm_16000", "mp3_44100_128"]:
            uri = urlunsplit(("ws", origin.netloc, "/v1/text-to-speech/morse/stream-input",
                              "auto_mode=true&output_format=" + output_format, ""))
            tts = await stack.enter_async_context(connect(uri))
            await tts.send(json.dumps({"text": " ", "xi_api_key": "synthetic-drain"}))
            await tts.send(json.dumps({"text": "ETETETETETETETET"}))
            assert json.loads(await asyncio.wait_for(tts.recv(), 5)).get("audio")
            # No EOS: codecs must cancel even while awaiting more text.
            sockets.append(tts)
        uri = urlunsplit(("ws", origin.netloc, "/v1/text-to-speech/morse/multi-stream-input",
                          "auto_mode=true", ""))
        multi = await stack.enter_async_context(connect(uri, additional_headers={"xi-api-key": "synthetic-drain"}))
        for context_id in ["a", "b"]:
            await multi.send(json.dumps({"context_id": context_id, "text": "ETETETETETETETET"}))
        seen = set()
        while len(seen) < 2:
            message = json.loads(await asyncio.wait_for(multi.recv(), 5))
            assert message.get("audio") and message["contextId"] in {"a", "b"}
            seen.add(message["contextId"])
        sockets.append(multi)
        process = await asyncio.create_subprocess_exec("docker", "stop", "--timeout", "10", container,
                                                       stdout=asyncio.subprocess.PIPE)

        async def closed(ws):
            try:
                while True:
                    await ws.recv()  # Drain any audio already in flight.
            except ConnectionClosed:
                pass

        await asyncio.wait_for(asyncio.gather(*(closed(ws) for ws in sockets)), 7)
        await asyncio.wait_for(process.communicate(), 12)
        assert process.returncode == 0
    check = await asyncio.create_subprocess_exec("docker", "inspect", "--format", "{{.State.ExitCode}}", container,
                                                 stdout=asyncio.subprocess.PIPE)
    stdout, _ = await check.communicate()
    assert check.returncode == 0 and stdout.strip() == b"0"
    print("SIGTERM closed active STT, PCM TTS, MP3 TTS and two multi-context encoders; the test container exited cleanly.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    parser.add_argument("container", help="The specific local test container to stop")
    args = parser.parse_args()
    asyncio.run(run(args.base_url, args.container))
