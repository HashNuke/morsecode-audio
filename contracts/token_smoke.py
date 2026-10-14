"""Official SDK issuance, batch/realtime token use and unauthenticated replay checks."""
import argparse
import asyncio
import json
from pathlib import Path

import httpx
from elevenlabs.client import ElevenLabs
from jsonschema import Draft202012Validator

from realtime_smoke import run as realtime, signal


def run(base_url):
    spec = json.loads(Path(__file__).with_name("elevenlabs-http.json").read_text())
    validator = Draft202012Validator({
        "$ref": "#/components/schemas/SingleUseTokenResponseModel",
        "components": spec["components"],
    })
    client = ElevenLabs(base_url=base_url, api_key="synthetic-token-issuer")
    issued = {}
    for purpose in ["batch_scribe", "realtime_scribe", "tts_websocket"]:
        result = client.tokens.single_use.create(purpose)
        validator.validate(result.model_dump())
        issued[purpose] = result.token
    assert len(set(issued.values())) == 3
    pcm = bytes(signal("ET"))
    result = client.speech_to_text.convert(
        model_id="scribe_v2", token=issued["batch_scribe"],
        file=("input.pcm", pcm), file_format="pcm_s16le_16",
    )
    assert result.text == "ET"
    # Raw browser-shaped requests establish that the SDK's API key is unnecessary.
    with httpx.Client(base_url=base_url, timeout=10, follow_redirects=False) as http:
        def batch(token):
            return http.post("/v1/speech-to-text", params={"token": token},
                             headers={"Origin": "https://browser.example"},
                             files={"file": ("input.pcm", pcm)},
                             data={"model_id": "scribe_v2", "file_format": "pcm_s16le_16"})

        assert batch(issued["batch_scribe"]).status_code == 401
        assert batch(issued["realtime_scribe"]).status_code == 401
        token = client.tokens.single_use.create("batch_scribe").token
        response = batch(token)
        assert response.status_code == 200
        assert response.json()["text"] == "ET"
        assert response.headers["access-control-allow-origin"] == "*"
        assert batch(token).status_code == 401
        preflight = http.options("/v1/speech-to-text", headers={
            "Origin": "https://browser.example", "Access-Control-Request-Method": "POST",
            "Access-Control-Request-Headers": "content-type,xi-api-key",
        })
        assert preflight.status_code == 204
        assert "access-control-allow-credentials" not in preflight.headers
    asyncio.run(realtime(base_url, use_token=True))
    print("Token SDK/schema, batch token-only use, replay/purpose rejection and CORS passed; TTS consumption is covered by tts_websocket_smoke.py.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    run(parser.parse_args().base_url)
