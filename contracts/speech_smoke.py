"""Exercise real Morse synthesis/recognition through the pinned official SDK."""
import argparse
import io
import json
from pathlib import Path

from elevenlabs.client import ElevenLabs
from jsonschema import Draft202012Validator


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    args = parser.parse_args()
    client = ElevenLabs(base_url=args.base_url, api_key="synthetic-speech-contract")
    pcm = b"".join(client.text_to_speech.convert(
        voice_id="morse", text="SOS TEST 123", model_id="morse_v1", output_format="pcm_16000"
    ))
    result = client.speech_to_text.convert(
        model_id="scribe_v2", file=io.BytesIO(pcm), file_format="pcm_s16le_16"
    )
    assert result.text == "SOS TEST 123"
    assert [word.text for word in result.words] == ["SOS", " ", "TEST", " ", "123"]
    schema = json.loads(Path(__file__).with_name("elevenlabs-http.json").read_text())
    body_schema = schema["paths"]["/v1/speech-to-text"]["post"]["responses"]["200"]["content"]["application/json"]["schema"]
    # Validate the SDK representation as well as its ability to parse the result.
    validator = Draft202012Validator({**body_schema, "components": schema["components"]})
    errors = list(validator.iter_errors(result.model_dump(mode="json", exclude_unset=True)))
    if errors:
        raise AssertionError("Transcription response does not match the pinned schema: " + errors[0].message)
    print(f"SDK PCM round trip and transcription schema passed: {len(pcm)} bytes.")


if __name__ == "__main__":
    main()
