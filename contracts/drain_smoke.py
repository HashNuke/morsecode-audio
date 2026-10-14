"""Stop the explicitly named local test container while an STT socket is active."""
import argparse
import asyncio
import json
from urllib.parse import urlsplit, urlunsplit

from websockets.asyncio.client import connect
from websockets.exceptions import ConnectionClosed


async def run(base_url, container):
    origin = urlsplit(base_url)
    assert origin.hostname in {"127.0.0.1", "localhost", "::1"}, "Local container test only"
    uri = urlunsplit(("ws", origin.netloc, "/v1/speech-to-text/realtime", "", ""))
    async with connect(uri, additional_headers={"xi-api-key": "synthetic-drain"}) as ws:
        assert json.loads(await asyncio.wait_for(ws.recv(), 5))["message_type"] == "session_started"
        process = await asyncio.create_subprocess_exec("docker", "stop", "--timeout", "10", container,
                                                       stdout=asyncio.subprocess.PIPE)
        try:
            await asyncio.wait_for(ws.recv(), 7)
            raise AssertionError("Drain left the idle speech connection active")
        except ConnectionClosed:
            pass
        await asyncio.wait_for(process.communicate(), 12)
        assert process.returncode == 0
    check = await asyncio.create_subprocess_exec("docker", "inspect", "--format", "{{.State.ExitCode}}", container,
                                                 stdout=asyncio.subprocess.PIPE)
    stdout, _ = await check.communicate()
    assert check.returncode == 0 and stdout.strip() == b"0"
    print("SIGTERM closed the active STT socket and the test container exited cleanly.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("base_url")
    parser.add_argument("container", help="The specific local test container to stop")
    args = parser.parse_args()
    asyncio.run(run(args.base_url, args.container))
