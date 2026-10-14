"""Pinned SDK realtime STT with independent signals and AsyncAPI validation."""
import argparse
import asyncio
import base64
import json
import struct
from pathlib import Path

from elevenlabs.client import ElevenLabs
from elevenlabs.realtime.scribe import AudioFormat, CommitStrategy
from jsonschema import Draft202012Validator


def signal(text, rate=16000):
    samples = bytearray()
    for index, char in enumerate(text):
        if index:
            samples.extend(bytes(rate * 60 // 1000 * 3 * 2))
        count = rate * 60 // 1000 * (3 if char == "T" else 1)
        for i in range(count):
            samples.extend(struct.pack("<h", 3000 if (i * 1400 // rate) % 2 == 0 else -3000))
    samples.extend(bytes(rate * 60 // 1000 * 14 * 2))
    return samples


async def run(base_url, use_token=False):
    spec = json.loads(Path(__file__).with_name("elevenlabs-websockets.json").read_text())["stt"]
    validator = Draft202012Validator({"$ref": "#/components/schemas/V1SpeechToTextRealtimeSubscribe",
                                     "components": spec["components"]})
    client = ElevenLabs(base_url=base_url, api_key="synthetic-sdk-realtime")
    options = {
        "model_id": "scribe_v2_realtime", "audio_format": AudioFormat.PCM_16000,
        "sample_rate": 16000, "commit_strategy": CommitStrategy.MANUAL,
        "include_timestamps": True,
    }
    if use_token:
        options["token"] = client.tokens.single_use.create("realtime_scribe").token
    connection = await client.speech_to_text.realtime.connect(options)
    messages = asyncio.Queue()
    for schema in spec["components"]["schemas"].values():
        for name in schema.get("properties", {}).get("message_type", {}).get("enum", []):
            connection.on(name, messages.put_nowait)

    async def receive():
        message = await asyncio.wait_for(messages.get(), 5)
        errors = list(validator.iter_errors(message))
        assert not errors, errors[0].message if errors else ""
        return message

    try:
        started = await receive()
        assert started["message_type"] == "session_started"
        assert started["config"]["sample_rate"] == 16000
        await connection.commit()  # Empty commit produces no transcript.
        audio_offset = 0
        for text in ["ET", "TE"]:
            pcm = signal(text)
            for offset in range(0, len(pcm), 317):
                await connection.send({"audio_base_64": base64.b64encode(pcm[offset:offset + 317]).decode()})
            await connection.commit()
            while True:
                message = await receive()
                if message["message_type"] == "partial_transcript":
                    assert text.startswith(message["text"])
                    continue
                assert message["message_type"] == "committed_transcript", message
                assert message["text"] == text
                break
            timed = await receive()
            assert timed["message_type"] == "committed_transcript_with_timestamps"
            assert timed["text"] == text
            assert abs(timed["words"][0]["start"] - audio_offset) <= 0.011
            audio_offset += len(pcm) / 32000
        print("Official SDK realtime STT (" + ("token" if use_token else "header") + "): two committed turns, split samples, timing order/origin and AsyncAPI schemas passed.")
    finally:
        await connection.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    asyncio.run(run(parser.parse_args().base_url))
