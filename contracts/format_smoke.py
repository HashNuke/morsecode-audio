"""Verify the pinned HTTP format enums against a running server, without files."""
import argparse
import json
import math
import struct
import subprocess
from pathlib import Path
from urllib.request import Request, urlopen


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    args = parser.parse_args()
    schema = json.loads(Path(__file__).with_name("elevenlabs-http.json").read_text())
    count = 0
    for suffix in ["", "/stream"]:
        path = "/v1/text-to-speech/{voice_id}" + suffix
        query = next(p for p in schema["paths"][path]["post"]["parameters"] if p["name"] == "output_format")
        for fmt in query["schema"]["enum"]:
            kind, rate, *_ = fmt.split("_")
            request = Request(args.base_url.rstrip("/") + path.replace("{voice_id}", "morse") + "?output_format=" + fmt,
                              data=b'{"text":"E T"}',
                              headers={"Content-Type": "application/json", "xi-api-key": "synthetic-format-check"})
            with urlopen(request, timeout=10) as response:
                data = response.read()
                expected = {"pcm": "audio/pcm", "wav": "audio/wav", "mp3": "audio/mpeg",
                            "opus": "audio/ogg", "ulaw": "audio/basic", "alaw": "audio/pcma"}[kind]
                assert response.headers["Content-Type"] == expected, fmt
            command = ["ffmpeg", "-v", "error", "-protocol_whitelist", "pipe"]
            if kind in ["pcm", "ulaw", "alaw"]:
                command += ["-f", {"pcm": "s16le", "ulaw": "mulaw", "alaw": "alaw"}[kind], "-ar", rate, "-ac", "1"]
            command += ["-i", "pipe:0", "-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1"]
            decoded = subprocess.run(command, input=data, capture_output=True, check=True, timeout=10).stdout
            duration = len(decoded) / 32000
            assert 1.49 <= duration <= 1.66, (fmt, duration)
            samples = [value for (value,) in struct.iter_unpack("<h", decoded)]
            rms = math.sqrt(sum(value * value for value in samples) / len(samples))
            assert rms > 300, (fmt, rms)
            count += 1
    print(f"HTTP format matrix: {count} binary cases independently decode with expected MIME/duration/signal.")


if __name__ == "__main__":
    main()
