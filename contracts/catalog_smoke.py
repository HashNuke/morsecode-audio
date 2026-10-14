"""Check local discovery with the pinned official SDK and upstream schemas."""
import argparse
import json
from pathlib import Path

import httpx
from elevenlabs.client import ElevenLabs
from jsonschema import Draft202012Validator


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url", help="Explicit local or hosted service origin")
    args = parser.parse_args()
    schema = json.loads(Path(__file__).with_name("elevenlabs-http.json").read_text())
    client = ElevenLabs(base_url=args.base_url, api_key="synthetic-sdk-test")
    assert client.models.list()[0].model_id == "morse_v1"
    assert client.voices.get_all().voices[0].voice_id == "morse"
    assert client.voices.get("morse").name == "Morse 700 Hz"
    assert client.voices.settings.get("morse").speed == 1
    for route in ("/v1/models", "/v1/voices", "/v2/voices", "/v1/voices/{voice_id}", "/v1/voices/{voice_id}/settings", "/v1/voices/settings/default"):
        response = httpx.get(args.base_url + route.replace("{voice_id}", "morse"), headers={"xi-api-key": "another-synthetic-key"})
        response.raise_for_status()
        body_schema = schema["paths"][route]["get"]["responses"]["200"]["content"]["application/json"]["schema"]
        Draft202012Validator({**body_schema, "components": schema["components"]}).validate(response.json())
    assert httpx.get(args.base_url + "/v1/models").status_code == 401
    print("Official SDK discovery and six upstream response schemas passed; missing key rejected.")


if __name__ == "__main__":
    main()
