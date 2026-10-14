"""Multi-context lifecycle using pinned SDK payload types and AsyncAPI schemas."""
import argparse
import asyncio
import base64
import json
from pathlib import Path

from elevenlabs.client import ElevenLabs
from elevenlabs.types import (
    InitializeConnectionMulti, SendTextMulti, CloseContext, CloseSocket,
    AudioOutputMulti, FinalOutputMulti,
)
from jsonschema import Draft202012Validator
from websockets.asyncio.client import connect

from tts_websocket_smoke import websocket_origin


async def run(base_url):
    spec = json.loads(Path(__file__).with_name("elevenlabs-websockets.json").read_text())["multi"]
    schema = {"$ref": "#/components/schemas/V1TextToSpeechVoiceIdMultiStreamInputSubscribe",
              "components": spec["components"]}
    validator = Draft202012Validator(schema)
    token = ElevenLabs(base_url=base_url, api_key="synthetic-multi").tokens.single_use.create("tts_websocket").token
    uri = websocket_origin(base_url) + "/v1/text-to-speech/morse/multi-stream-input?auto_mode=true&output_format=pcm_16000&single_use_token=" + token
    async with connect(uri, origin="https://browser.example", max_size=1 << 20) as socket:
        async def send(payload):
            await socket.send(payload.model_dump_json(by_alias=True, exclude_none=True))

        async def receive():
            message = json.loads(await socket.recv())
            validator.validate(message)
            if message.get("isFinal") is True:
                assert FinalOutputMulti.model_validate(message).context_id
            else:
                assert AudioOutputMulti.model_validate(message).context_id
            return message

        async def until_final(context):
            total, text = 0, ""
            while True:
                message = await receive()
                assert message["contextId"] == context
                if message.get("isFinal") is True:
                    return total, text
                total += len(base64.b64decode(message["audio"], validate=True))
                text += "".join((message.get("normalizedAlignment") or {}).get("chars", []))

        await send(InitializeConnectionMulti(context_id="waiting"))
        await send(SendTextMulti(context_id="waiting", text="E"))
        first = await receive()
        assert first["contextId"] == "waiting" and len(base64.b64decode(first["audio"])) == 1920
        await send(SendTextMulti(context_id="waiting", text=""))  # keepalive, not EOS
        await send(SendTextMulti(context_id="complete", text="T", flush=True))
        await send(CloseContext(context_id="complete", close_context=True))
        assert await until_final("complete") == (32640, "T")
        await send(CloseContext(context_id="waiting", close_context=True))
        assert await until_final("waiting") == (0, "")
        await send(SendTextMulti(context_id="waiting", text="E T", flush=True))
        assert await until_final("waiting") == (48000, "E T")
        await send(CloseSocket(close_socket=True))
        # Completed flushes must not produce duplicate finals on socket close.
        assert [message async for message in socket] == []
        assert socket.close_code == 1000
    print("Multi TTS: pinned SDK message types/AsyncAPI, independent contexts, keepalive, flush-close, cancellation, ID reuse and normal socket completion passed.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    asyncio.run(asyncio.wait_for(run(parser.parse_args().base_url), 30))
