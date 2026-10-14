"""TTS WS protocol/schema checks and unmodified SDK through a local trusted TLS fixture."""
import argparse
import asyncio
import base64
import json
import os
from pathlib import Path
import ssl
import subprocess
import sys
import tempfile
from urllib.parse import urlsplit, urlunsplit

from elevenlabs.client import ElevenLabs
from jsonschema import Draft202012Validator
from websockets.asyncio.client import connect


def websocket_origin(origin):
    parsed = urlsplit(origin)
    return urlunsplit(("wss" if parsed.scheme == "https" else "ws", parsed.netloc, "", "", ""))


async def protocol(base_url):
    spec = json.loads(Path(__file__).with_name("elevenlabs-websockets.json").read_text())["tts"]
    # Upstream FinalOutput has no required fields, making its top-level oneOf
    # overlap AudioOutput. Select the event branch explicitly before validating.
    validators = {name: Draft202012Validator({"$ref": "#/components/schemas/" + name,
                                             "components": spec["components"]})
                  for name in ["AudioOutput", "FinalOutput"]}
    client = ElevenLabs(base_url=base_url, api_key="synthetic-tts-token")
    token = client.tokens.single_use.create("tts_websocket").token
    endpoint = websocket_origin(base_url) + "/v1/text-to-speech/morse/stream-input"
    async with connect(endpoint + "?output_format=pcm_16000&single_use_token=" + token,
                       origin="https://browser.example", max_size=1 << 20) as socket:
        await socket.send(json.dumps({"text": " "}))
        for text in [" e", "t ", " e "]:
            await socket.send(json.dumps({"text": text}))
        await socket.send(json.dumps({"text": ""}))
        pcm = bytearray()
        original, normalized, finals = [], [], 0
        chunk_start_ms = 0
        reconstructed = []
        async for raw in socket:
            message = json.loads(raw)
            if message.get("isFinal") is True:
                validators["FinalOutput"].validate(message)
                finals += 1
                continue
            assert finals == 0, "audio followed final marker"
            validators["AudioOutput"].validate(message)
            chunk = base64.b64decode(message["audio"], validate=True)
            for field, target in [("alignment", original), ("normalizedAlignment", normalized)]:
                alignment = message.get(field)
                if alignment:
                    target.extend(alignment["chars"])
                    assert len(alignment["chars"]) == len(alignment["charStartTimesMs"]) == len(alignment["charDurationsMs"])
                    if field == "normalizedAlignment":
                        reconstructed.extend((char, chunk_start_ms + start, duration)
                                             for char, start, duration in zip(alignment["chars"], alignment["charStartTimesMs"], alignment["charDurationsMs"]))
            pcm.extend(chunk)
            chunk_start_ms += len(chunk) / 32
        assert socket.close_code == 1000 and finals == 1
        assert len(pcm) == 55680 and "".join(original) == " et  e " and "".join(normalized) == "ET E"
        assert reconstructed == [("E", 0, 60), ("T", 240, 180), (" ", 420, 420), ("E", 840, 60)]
    print("Single TTS WS: token-only browser origin, audio lengths, original/normalized alignment, chunk-relative timing and selected AsyncAPI branches passed.")


def consume_sdk(origin):
    client = ElevenLabs(base_url=origin, api_key="synthetic-sdk-tts")
    pcm = b"".join(client.text_to_speech.convert_realtime(
        "morse", text=iter(["E ", "T"]), model_id="morse_v1", output_format="pcm_16000"))
    assert len(pcm) == 48000
    mp3 = b"".join(client.text_to_speech.convert_realtime(
        "morse", text=iter(["E ", "T"]), model_id="morse_v1"))
    decoded = subprocess.run(["ffmpeg", "-v", "error", "-f", "mp3", "-i", "pipe:0",
                              "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1"],
                             input=mp3, capture_output=True, check=True, timeout=10).stdout
    assert 1.49 <= len(decoded) / 32000 <= 1.65


async def sdk_process(origin, cert=None):
    # Keep the synchronous SDK in a separate, killable process. It must not
    # share the TLS fixture's event loop or mutate its process-wide trust store.
    env = dict(os.environ)
    if cert is not None:
        env["SSL_CERT_FILE"] = str(cert)
    process = await asyncio.create_subprocess_exec(
        sys.executable, "-u", str(Path(__file__).resolve()), origin, "--sdk-client",
        env=env, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.PIPE)
    try:
        stdout, stderr = await asyncio.wait_for(process.communicate(), 30)
        assert process.returncode == 0, stderr.decode()
    finally:
        if process.returncode is None:
            process.kill()
            await process.wait()


async def sdk(base_url):
    # The SDK hardcodes wss even for an http base_url. Hosted HTTPS is tested
    # directly. Only local HTTP needs a real, trusted TLS termination fixture.
    target = urlsplit(base_url)
    if target.scheme == "https":
        await sdk_process(base_url)
        print("Unmodified SDK convert_realtime: PCM and default MP3 passed against the supplied HTTPS origin.")
        return
    assert target.scheme == "http" and target.hostname in {"127.0.0.1", "localhost", "::1"}, "TLS fixture is local only"
    bridges = set()
    async def bridge(reader, writer):
        # Forward bytes, including the upgrade and close handshake, unchanged.
        # This fixture terminates TLS only; it does not emulate a WS endpoint.
        task = asyncio.current_task()
        bridges.add(task)
        remote_writer = None
        pumps = []
        try:
            remote_reader, remote_writer = await asyncio.open_connection(
                target.hostname, target.port or 80)

            async def forward(source, destination):
                while data := await source.read(16384):
                    destination.write(data)
                    await destination.drain()

            pumps = [asyncio.create_task(forward(reader, remote_writer)),
                     asyncio.create_task(forward(remote_reader, writer))]
            await asyncio.wait(pumps, return_when=asyncio.FIRST_COMPLETED)
        finally:
            for pump in pumps:
                pump.cancel()
            await asyncio.gather(*pumps, return_exceptions=True)
            writer.close()
            if remote_writer:
                remote_writer.close()
                await remote_writer.wait_closed()
            await writer.wait_closed()
            bridges.discard(task)

    with tempfile.TemporaryDirectory(prefix="morse-tls-fixture-") as directory:
        cert, key = Path(directory) / "cert.pem", Path(directory) / "key.pem"
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                        "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1",
                        "-keyout", str(key), "-out", str(cert)], check=True, capture_output=True)
        tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        tls.load_cert_chain(cert, key)
        # This short-lived fixture never resumes sessions. Avoid OpenSSL's
        # TLS 1.3 session-ticket handshake hazard described here:
        # https://github.com/openssl/openssl/issues/7967
        tls.num_tickets = 0
        try:
            async with await asyncio.start_server(bridge, "127.0.0.1", 0, ssl=tls) as server:
                origin = "https://127.0.0.1:" + str(server.sockets[0].getsockname()[1])
                await sdk_process(origin, cert)
        finally:
            for task in list(bridges):
                task.cancel()
            await asyncio.gather(*list(bridges), return_exceptions=True)
    print("Unmodified SDK 2.72.0 convert_realtime: PCM and default MP3, verified local TLS, normal completion and independent decode passed.")


async def run(base_url):
    await protocol(base_url)
    await sdk(base_url)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    parser.add_argument("--sdk-client", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.sdk_client:
        consume_sdk(args.base_url)
    else:
        asyncio.run(asyncio.wait_for(run(args.base_url), 60))
