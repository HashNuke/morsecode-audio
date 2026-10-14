"""Exercise all four HTTP TTS shapes using the pinned official SDK."""
import argparse
import base64
import json
import subprocess
from pathlib import Path

from elevenlabs.client import ElevenLabs
from jsonschema import Draft202012Validator


def validate(schema, name, response):
    validator = Draft202012Validator({"$ref": "#/components/schemas/" + name,
                                     "components": schema["components"]})
    errors = list(validator.iter_errors(response.model_dump(mode="json", exclude_unset=True)))
    assert not errors, errors[0].message if errors else ""


def decode_mp3(data):
    output = subprocess.run(["ffmpeg", "-v", "error", "-protocol_whitelist", "pipe",
                             "-i", "pipe:0", "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1"],
                            input=data, capture_output=True, check=True, timeout=10).stdout
    assert len(output) > 16000


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    args = parser.parse_args()
    client = ElevenLabs(base_url=args.base_url, api_key="synthetic-http-tts")
    schema = json.loads(Path(__file__).with_name("elevenlabs-http.json").read_text())
    for method in [client.text_to_speech.convert, client.text_to_speech.stream]:
        decode_mp3(b"".join(method(voice_id="morse", text=" e  t ")))
    for streaming in [False, True]:
        for fmt in [None, "pcm_16000", "opus_48000_64"]:
            params = {"voice_id": "morse", "text": " e  t "}
            if fmt:
                params["output_format"] = fmt
            if streaming:
                responses = client.text_to_speech.stream_with_timestamps(**params)
                schema_name = "StreamingAudioChunkWithTimestampsResponseModel"
            else:
                responses = [client.text_to_speech.convert_with_timestamps(**params)]
                schema_name = "AudioWithTimestampsResponseModel"
            audio = bytearray()
            original, normalized = [], []
            count = 0
            for response in responses:
                validate(schema, schema_name, response)
                audio.extend(base64.b64decode(response.audio_base_64, validate=True))
                if response.alignment:
                    original.extend(response.alignment.characters)
                if response.normalized_alignment:
                    normalized.extend(response.normalized_alignment.characters)
                count += 1
            assert "".join(original) == " e  t "
            assert "".join(normalized) == "E T"
            assert count > 1 if streaming else count == 1
            if fmt is None:
                decode_mp3(bytes(audio))
            elif fmt == "pcm_16000":
                assert len(audio) == 48000
            else:
                assert audio[:4] == b"OggS"
    print("Official SDK: four TTS routes, MP3 default, PCM/Opus timed variants and response schemas passed.")


if __name__ == "__main__":
    main()
