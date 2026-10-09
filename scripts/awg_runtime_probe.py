#!/usr/bin/env python3
"""Synthetic, local-only AWG transfer gate. Run in a fresh network namespace.

Requires Python cryptography, curl and the explicit sing-box binary argument.
Never prints configurations, keys or subprocess diagnostics. Self-to-self
success is NOT official-client interoperability evidence.
"""
import argparse
import base64
import contextlib
import hashlib
import http.server
import json
import pathlib
import socket
import subprocess
import tempfile
import threading
import time

from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, NoEncryption, PrivateFormat, PublicFormat

PAYLOAD = b"AWG_DATA_TRANSFER_OK"


def keypair():
    key = X25519PrivateKey.generate()
    return (base64.b64encode(key.private_bytes(Encoding.Raw, PrivateFormat.Raw, NoEncryption())).decode(),
            base64.b64encode(key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)).decode())


def port(kind):
    with socket.socket(socket.AF_INET, kind) as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(PAYLOAD)

    def log_message(self, *args):
        pass


def stop(process):
    if process.poll() is None:
        process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def probe(binary, name, feature, http_port, wrong=None):
    private, public = keypair()
    client_private, client_public = keypair()
    udp_port, socks_port = port(socket.SOCK_DGRAM), port(socket.SOCK_STREAM)
    params = {} if name == "wireguard" else dict(
        jc=3, jmin=40, jmax=80, s1=16, s2=24, s3=16, s4=16,
        h1="100000-100010", h2="200000-200010", h3="300000-300010", h4="400000-400010")
    params.update(feature)
    server_peer = dict(public_key=client_public, allowed_ips=["10.77.0.2/32"])
    client_peer = dict(public_key=public, address="127.0.0.1", port=udp_port, allowed_ips=["0.0.0.0/0"])
    client_params = dict(params)
    if wrong == "hp":
        client_params["header_protection_key"] = "34" * 32
    elif wrong == "identity":
        client_peer["public_key"] = keypair()[1]
    elif wrong == "psk":
        server_peer["pre_shared_key"] = base64.b64encode(bytes([71]) * 32).decode()
        client_peer["pre_shared_key"] = base64.b64encode(bytes([72]) * 32).decode()
    server = {"log": {"disabled": True}, "endpoints": [dict(
        type="wireguard", tag="awg", system=False, address=["10.77.0.1/32"],
        private_key=private, listen_port=udp_port, mtu=1280, peers=[server_peer], **params)],
        "outbounds": [{"type": "direct", "tag": "direct"}], "route": {"final": "direct"}}
    client = {"log": {"disabled": True}, "inbounds": [dict(
        type="mixed", tag="test", listen="127.0.0.1", listen_port=socks_port)],
        "endpoints": [dict(type="wireguard", tag="awg", system=False,
        address=["10.77.0.2/32"], private_key=client_private, mtu=1280,
        peers=[client_peer], **client_params)], "route": {"final": "awg"}}
    result = {"case": name, "expected_transfer": wrong is None, "pass": False}
    with tempfile.TemporaryDirectory(prefix="awg-gate-") as directory, contextlib.ExitStack() as cleanup:
        paths = []
        for label, config in [("server", server), ("client", client)]:
            path = pathlib.Path(directory) / (label + ".json")
            with path.open("x", encoding="utf-8") as output:
                path.chmod(0o600)
                json.dump(config, output)
            paths.append(path)
            check = subprocess.run([binary, "check", "-c", str(path)],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=20)
            result[label + "_check"] = check.returncode
            if check.returncode:
                result["failure"] = "config_check"
                return result
        processes = []
        for path in paths:
            process = subprocess.Popen([binary, "run", "-c", str(path)],
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            cleanup.callback(stop, process)
            processes.append(process)
        ready = False
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline and all(p.poll() is None for p in processes):
            try:
                with socket.create_connection(("127.0.0.1", socks_port), timeout=0.1):
                    ready = True
                    break
            except OSError:
                time.sleep(0.05)
        if not ready:
            result["failure"] = "startup"
            return result
        transfer = subprocess.run([
            "curl", "--noproxy", "", "--proxy", f"socks5h://127.0.0.1:{socks_port}",
            "--max-time", "12", "-sS", f"http://10.77.0.1:{http_port}/"],
            capture_output=True, timeout=15)
        result["curl_exit"] = transfer.returncode
        result["transfer"] = transfer.returncode == 0 and transfer.stdout == PAYLOAD
        result["alive"] = all(p.poll() is None for p in processes)
        # Only a live-session timeout qualifies for a negative observation;
        # bind/config/process failure is not proof of key rejection.
        result["pass"] = result["alive"] and (result["transfer"] if wrong is None else
                          transfer.returncode == 28 and not transfer.stdout)
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=pathlib.Path)
    parser.add_argument("--case", action="append", help="Select named cases (repeatable)")
    args = parser.parse_args()
    binary = str(args.binary.resolve(strict=True))
    hp = {"header_protection_key": "12" * 32}
    combined = dict(hp, content_padding_addition="1", random_trailers=True)
    cases = [("wireguard", {}, None), ("awg2", {}, None), ("hp", hp, None),
             ("padding", {"content_padding_addition": "1"}, None),
             ("trailers", {"random_trailers": True}, None), ("combined", combined, None),
             ("wrong_hp", combined, "hp"), ("wrong_identity", combined, "identity"),
             ("wrong_psk", combined, "psk")]
    if args.case:
        unknown = set(args.case) - {case[0] for case in cases}
        if unknown:
            parser.error("unknown case")
        cases = [case for case in cases if case[0] in args.case]
    with open(binary, "rb") as executable:
        digest = hashlib.file_digest(executable, "sha256").hexdigest()
    print(json.dumps({"binary_sha256": digest}), flush=True)
    results = []
    with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler) as server:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            for name, feature, wrong in cases:
                try:
                    result = probe(binary, name, feature, server.server_port, wrong)
                except Exception as error:
                    result = {"case": name, "pass": False, "failure_type": type(error).__name__}
                results.append(result)
                print(json.dumps(result, sort_keys=True), flush=True)
        finally:
            server.shutdown()
            thread.join(timeout=5)
    return 0 if results and all(result["pass"] for result in results) else 1


if __name__ == "__main__":
    raise SystemExit(main())
