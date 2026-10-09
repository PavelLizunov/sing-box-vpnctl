#!/usr/bin/env python3
"""Synthetic native AmneziaWG interop; never run in the host network namespace.

Example (root, no PID namespace; binaries must already exist locally):
  unshare --net --mount --propagation private -- python3 awg_official_interop.py \
    --isolated-netns --sing-box /path/sing-box --official /path/awg31-reference \
    --official-sha256 BUILD_SHA256 --case combined_conf

The caller must attest BUILD_SHA256 was built from official v3.1.20260828,
commit b5928efb6ca19f0153958460c3d141f04abc5c2e. The official --version embeds
an older string and is NOT a source-revision attestation. No build/download is
performed. Requires installed Python cryptography and iproute2. Both binary
arguments are trusted executable code. JSONL output is allowlisted; no daemon
logs, UAPI replies, configuration, keys, paths or exception text are emitted.

Each role tests actual TCP HTTP POST request/response bytes and UDP echo bytes.
"official_server": sing-box userspace client -> native TUN 10.77.0.1.
"official_client": native TUN client -> sing-box userspace 10.78.0.1 -> loopback.
The combined_conf case uses synthetic .conf-equivalent J/S/H/HP/CPA/trailers
values, not a .conf parser or a production configuration. Cookies stay enabled.
"wrong_hp" requires a successful fresh correct-key combined_conf baseline in
EACH role, then fresh sessions with only the official HP key changed. Only
live-process TCP AND UDP timeouts and no native handshake qualify as rejection.
"""
import argparse
import base64
import contextlib
import hashlib
import json
import os
from pathlib import Path
import secrets
import resource
import shutil
import signal
import socket
import socketserver
import stat
import subprocess
import tempfile
import threading
import time


REVISION = "b5928efb6ca19f0153958460c3d141f04abc5c2e"
RELEASE = "v3.1.20260828"
CASES = ("awg2", "hp_s12", "hp_sgt12", "cpa1", "trailers", "combined", "combined_conf", "wrong_hp")
ROLES = ("official_server", "official_client")
PAYLOAD = b"AWG31_SYNTHETIC_HTTP_POST\x00" + bytes(range(256)) * 4
UDP_PAYLOADS = (b"AWG31_ECHO_" + bytes(range(17)), bytes(range(256)) * 4)
REQUEST = (b"POST /synthetic HTTP/1.1\r\nHost: synthetic.invalid\r\n"
           b"Connection: close\r\nContent-Length: " + str(len(PAYLOAD)).encode() + b"\r\n\r\n" + PAYLOAD)
RESPONSE = (b"HTTP/1.1 200 OK\r\nConnection: close\r\nContent-Length: "
            + str(len(PAYLOAD)).encode() + b"\r\n\r\n" + PAYLOAD)


class Failure(Exception):
    """Only internal fixed reason codes may cross the reporting boundary."""


class Interrupted(BaseException):
    """Do not swallow cancellation as a failed matrix row and keep running."""


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise Failure("arguments")


def emit(value):
    print(json.dumps(value, sort_keys=True), flush=True)


def digest(path):
    with path.open("rb") as source:
        value = hashlib.sha256()
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
        return value.hexdigest()


def keys():
    # Keep dependency failures inside the sanitized top-level error boundary.
    from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey
    from cryptography.hazmat.primitives.serialization import Encoding, NoEncryption, PrivateFormat, PublicFormat
    result = []
    for _ in range(2):
        key = X25519PrivateKey.generate()
        result.append((key.private_bytes(Encoding.Raw, PrivateFormat.Raw, NoEncryption()),
                       key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)))
    return result, secrets.token_hex(32), secrets.token_hex(32)


def parameters(case, hp):
    value = dict(jc=3, jmin=40, jmax=80, s1=16, s2=24, s3=16, s4=16,
                 h1="100000-100010", h2="200000-200010", h3="300000-300010", h4="400000-400010")
    if case in ("hp_s12", "hp_sgt12", "combined", "combined_conf"):
        value["header_protection_key"] = hp
    if case == "hp_s12":
        value.update(s1=12, s2=12, s3=12, s4=12)
    if case in ("cpa1", "combined", "combined_conf"):
        value["content_padding_addition"] = "1"
    if case in ("trailers", "combined", "combined_conf"):
        value["random_trailers"] = True
    if case == "combined_conf":
        # Equivalent synthetic .conf: Jc=4 Jmin=64 Jmax=128 S1=24 S2=32
        # S3=12 S4=48 H1..H4 as below, HP=<fresh key>, CPA=1, trailers=true.
        value.update(jc=4, jmin=64, jmax=128, s1=24, s2=32, s3=12, s4=48,
                     h1="110000-110099", h2="220000-220099", h3="330000-330099", h4="440000-440099")
    # Never downgrade by disabling cookies, including on the reference side.
    value["disable_cookies"] = False
    return value


def net_inode():
    return os.stat("/proc/self/ns/net").st_ino


class Network:
    def __init__(self, acknowledged):
        if not acknowledged or os.geteuid() != 0 or net_inode() == os.stat("/proc/1/ns/net").st_ino:
            raise Failure("namespace_guard")
        self.inode = net_inode()
        self.ip = shutil.which("ip")
        if not self.ip:
            raise Failure("ip_missing")
        links = self.inspect("link", "show")
        if [link["ifname"] for link in links] != ["lo"]:
            raise Failure("namespace_not_fresh")
        for family in ("-4", "-6"):
            routes = self.inspect(family, "route", "show", "table", "all")
            if any(route.get("dev") != "lo" or route.get("dst") == "default" for route in routes):
                raise Failure("namespace_not_fresh")
        self.change("link", "set", "lo", "up")

    def guard(self):
        if net_inode() != self.inode or net_inode() == os.stat("/proc/1/ns/net").st_ino:
            raise Failure("namespace_guard")

    def inspect(self, *args):
        self.guard()
        result = subprocess.run([self.ip, "-j", *args], stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL, timeout=5)
        if result.returncode:
            raise Failure("ip_inspect")
        return json.loads(result.stdout)

    def change(self, *args):
        self.guard()
        result = subprocess.run([self.ip, *args], stdout=subprocess.DEVNULL,
                                stderr=subprocess.DEVNULL, timeout=5)
        if result.returncode:
            raise Failure("ip_change")


def free_port(kind):
    with socket.socket(socket.AF_INET, kind) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def stop(process):
    if process.poll() is None:
        process.terminate()
    try:
        process.wait(timeout=3)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=3)


def launch(cleanup, command, env=None):
    process = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, env=env, close_fds=True)
    cleanup.callback(stop, process)
    return process


def alive(processes):
    if not all(process.poll() is None for process in processes):
        raise Failure("process_exit")


def uapi(path, body):
    # get replies contain private_key: retain in memory only, never output.
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as sock:
        sock.settimeout(2)
        sock.connect(str(path))
        sock.sendall(body.encode())
        reply = b""
        deadline = time.monotonic() + 2
        while not reply.endswith(b"\n\n"):
            if time.monotonic() >= deadline or len(reply) > 65536:
                raise Failure("uapi_response")
            part = sock.recv(4096)
            if not part:
                raise Failure("uapi_response")
            reply += part
        lines = reply.decode("ascii").splitlines()
        if lines[-2:] != ["errno=0", ""]:
            raise Failure("uapi_errno")
        return lines


def handshake_seen(path):
    lines = uapi(path, "get=1\n\n")
    values = [line.split("=", 1)[1] for line in lines if line.startswith("last_handshake_time_sec=")]
    if len(values) != 1:
        raise Failure("uapi_peer")
    return int(values[0]) > 0


def wait_ready(test, processes, seconds=5):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        alive(processes)
        try:
            if test():
                return
        except (OSError, Failure):
            pass
        time.sleep(0.05)
    raise Failure("startup")


def listener_ready(port):
    with socket.create_connection(("127.0.0.1", port), timeout=0.2):
        return True


class HTTPHandler(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(2)
        received = b""
        try:
            while len(received) < len(REQUEST):
                part = self.request.recv(len(REQUEST) - len(received))
                if not part:
                    return
                received += part
            if received == REQUEST:
                self.request.sendall(RESPONSE)
        except OSError:
            pass


class UDPHandler(socketserver.BaseRequestHandler):
    def handle(self):
        data, sock = self.request
        if data in UDP_PAYLOADS:
            sock.sendto(data, self.client_address)


class TCPServer(socketserver.ThreadingTCPServer):
    daemon_threads = True
    block_on_close = False

    def handle_error(self, request, client_address):
        pass


class UDPServer(socketserver.ThreadingUDPServer):
    daemon_threads = True
    block_on_close = False

    def handle_error(self, request, client_address):
        pass


def serve(cleanup, server_type, address, handler, port=0):
    server = server_type((address, port), handler)
    cleanup.callback(server.server_close)
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
    thread.start()
    cleanup.callback(thread.join, 3)
    cleanup.callback(server.shutdown)
    return server.server_address[1]


def http_probe(address, port, timeout):
    response = b""
    try:
        deadline = time.monotonic() + timeout
        with socket.create_connection((address, port), timeout=timeout) as sock:
            sock.settimeout(max(0.001, deadline - time.monotonic()))
            sock.sendall(REQUEST)
            while len(response) < len(RESPONSE):
                sock.settimeout(max(0.001, deadline - time.monotonic()))
                part = sock.recv(len(RESPONSE) + 1 - len(response))
                if not part:
                    return "mismatch"
                response += part
            return "ok" if response == RESPONSE else "mismatch"
    except TimeoutError:
        return "mismatch" if response else "timeout"
    except OSError:
        return "io_error"


def udp_probe(address, port, timeout):
    received = False
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.connect((address, port))
            deadline = time.monotonic() + timeout
            for payload in UDP_PAYLOADS:
                sock.settimeout(max(0.001, deadline - time.monotonic()))
                sock.send(payload)
                if sock.recv(4096) != payload:
                    return "mismatch"
                received = True
            return "ok"
    except TimeoutError:
        return "mismatch" if received else "timeout"
    except OSError:
        return "io_error"


def remember_socket(path, identity):
    try:
        info = path.lstat()
        if not identity and stat.S_ISSOCK(info.st_mode) and info.st_uid == os.geteuid():
            identity.append((info.st_dev, info.st_ino))
    except FileNotFoundError:
        pass


def unlink_owned(path, identity):
    try:
        current = path.lstat()
        if identity and (current.st_dev, current.st_ino) == identity[0] and stat.S_ISSOCK(current.st_mode):
            path.unlink()
    except FileNotFoundError:
        pass


def scenario(args, network, case, role, key_material, wrong=False):
    pairs, hp, wrong_hp = key_material
    (native_private, native_public), (sing_private, sing_public) = pairs
    params = parameters(case, hp)
    native_server = role == "official_server"
    native_address = "10.77.0.1" if native_server else "10.78.0.2"
    sing_address = "10.77.0.2" if native_server else "10.78.0.1"
    target_address = native_address if native_server else sing_address
    interface = "awi" + secrets.token_hex(5)  # Linux IFNAMSIZ <= 15.
    socket_path = Path("/var/run/amneziawg") / (interface + ".sock")
    if os.path.lexists(socket_path):
        raise Failure("socket_collision")
    identity = []
    # Registration order ensures children stop before socket/tempfile cleanup.
    with tempfile.TemporaryDirectory(prefix="awg-official-") as directory, contextlib.ExitStack() as cleanup:
        cleanup.callback(unlink_owned, socket_path, identity)
        env = {"PATH": os.environ.get("PATH", "/usr/sbin:/usr/bin:/sbin:/bin"),
               "LOG_LEVEL": "silent", "WG_PROCESS_FOREGROUND": "1"}
        native = launch(cleanup, [str(args.official), "-f", interface], env)
        # Also discover the owned socket during failure cleanup if startup exits
        # before the readiness callback observes it; then stop PID, then unlink.
        cleanup.callback(remember_socket, socket_path, identity)

        def native_ready():
            info = socket_path.lstat()
            if not stat.S_ISSOCK(info.st_mode) or info.st_uid != os.geteuid():
                raise Failure("socket_type")
            if not identity:
                identity.append((info.st_dev, info.st_ino))
            return bool(uapi(socket_path, "get=1\n\n"))

        wait_ready(native_ready, [native])
        native_port = free_port(socket.SOCK_DGRAM)
        sing_port = free_port(socket.SOCK_DGRAM)
        while sing_port == native_port:
            sing_port = free_port(socket.SOCK_DGRAM)
        native_params = dict(params)
        if wrong:
            native_params["header_protection_key"] = wrong_hp
        native_config = dict(private_key=native_private.hex(), listen_port=native_port, **native_params)
        lines = ["set=1"] + [key + "=" + (str(value).lower() if isinstance(value, bool) else str(value))
                                 for key, value in native_config.items()]
        lines += ["replace_peers=true", "public_key=" + sing_public.hex(),
                  "replace_allowed_ips=true", "allowed_ip=" + sing_address + "/32"]
        if not native_server:
            lines += ["endpoint=127.0.0.1:" + str(sing_port)]
        uapi(socket_path, "\n".join(lines) + "\n\n")
        network.change("address", "add", native_address + "/32", "dev", interface)
        network.change("link", "set", "dev", interface, "mtu", "1280", "up")
        network.change("route", "add", sing_address + "/32", "dev", interface, "src", native_address)
        route = network.inspect("route", "get", sing_address)
        if len(route) != 1 or route[0].get("dev") != interface or route[0].get("prefsrc") != native_address:
            raise Failure("route_guard")
        service_address = native_address if native_server else "127.0.0.1"
        http_port = serve(cleanup, TCPServer, service_address, HTTPHandler)
        # Avoid a service binding the not-yet-started sing-box wildcard UDP port.
        udp_port = free_port(socket.SOCK_DGRAM)
        while udp_port in (native_port, sing_port):
            udp_port = free_port(socket.SOCK_DGRAM)
        udp_port = serve(cleanup, UDPServer, service_address, UDPHandler, udp_port)
        health_port = free_port(socket.SOCK_STREAM)
        peer = dict(public_key=base64.b64encode(native_public).decode(), allowed_ips=[native_address + "/32"])
        if native_server:
            peer.update(address="127.0.0.1", port=native_port)
        config = {"log": {"disabled": True}, "inbounds": [dict(type="mixed", tag="health",
                  listen="127.0.0.1", listen_port=health_port)], "endpoints": [dict(type="wireguard",
                  tag="awg", system=False, address=[sing_address + "/32"], mtu=1280,
                  private_key=base64.b64encode(sing_private).decode(), listen_port=sing_port,
                  peers=[peer], **params)]}
        if native_server:
            tcp_entry = free_port(socket.SOCK_STREAM)
            while tcp_entry == health_port:
                tcp_entry = free_port(socket.SOCK_STREAM)
            udp_entry = free_port(socket.SOCK_DGRAM)
            while udp_entry in (native_port, sing_port, udp_port):
                udp_entry = free_port(socket.SOCK_DGRAM)
            config["inbounds"] += [dict(type="direct", tag="tcp-test", listen="127.0.0.1",
                 listen_port=tcp_entry, network="tcp", override_address=target_address, override_port=http_port),
                 dict(type="direct", tag="udp-test", listen="127.0.0.1", listen_port=udp_entry,
                      network="udp", override_address=target_address, override_port=udp_port)]
            config["route"] = {"final": "awg"}
            probe_address, probe_http, probe_udp = "127.0.0.1", tcp_entry, udp_entry
        else:
            config["outbounds"] = [{"type": "direct", "tag": "local"}]
            # WireGuard endpoint NewConnectionEx/NewPacketConnectionEx already
            # map destinations within its own /32 to loopback (including UDP NAT).
            config["route"] = {"final": "local"}
            probe_address, probe_http, probe_udp = sing_address, http_port, udp_port
        config_path = Path(directory) / "sing.json"
        with config_path.open("x", encoding="utf-8") as output:
            os.chmod(config_path, 0o600)
            json.dump(config, output)
        check = subprocess.run([str(args.sing_box), "check", "-c", str(config_path)],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
        if check.returncode:
            raise Failure("config_check")
        sing = launch(cleanup, [str(args.sing_box), "run", "-c", str(config_path)], env)
        processes = [native, sing]
        wait_ready(lambda: listener_ready(health_port), processes)
        if native_server:
            wait_ready(lambda: listener_ready(probe_http), processes)
        http = http_probe(probe_address, probe_http, args.timeout)
        alive(processes)
        udp = udp_probe(probe_address, probe_udp, args.timeout)
        alive(processes)
        handshake = handshake_seen(socket_path)
        passed = (http == "timeout" and udp == "timeout" and not handshake) if wrong else (
                  http == "ok" and udp == "ok" and handshake)
        return {"pass": passed, "http": http, "udp": udp, "native_handshake": handshake, "alive": True}


def main():
    parser = Parser(description=__doc__)
    parser.add_argument("--sing-box", required=True, type=Path)
    parser.add_argument("--official", required=True, type=Path)
    parser.add_argument("--official-sha256", required=True)
    parser.add_argument("--isolated-netns", action="store_true")
    parser.add_argument("--case", choices=CASES)
    parser.add_argument("--role", choices=ROLES)
    parser.add_argument("--timeout", type=float, default=12)
    args = parser.parse_args()
    if not 3 <= args.timeout <= 30:
        raise Failure("arguments")
    os.umask(0o077)
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    network = Network(args.isolated_netns)
    args.sing_box = args.sing_box.resolve(strict=True)
    args.official = args.official.resolve(strict=True)
    for binary in (args.sing_box, args.official):
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise Failure("binary_missing")
    official_digest = digest(args.official)
    if len(args.official_sha256) != 64 or official_digest != args.official_sha256.lower():
        raise Failure("official_digest_mismatch")
    emit({"kind": "provenance", "official_release": RELEASE, "official_revision": REVISION,
          "source_attestation": "caller_supplied_digest", "official_sha256": official_digest,
          "sing_box_sha256": digest(args.sing_box)})
    outcomes = []
    for case in ([args.case] if args.case else CASES):
        for role in ([args.role] if args.role else ROLES):
            result = {"kind": "case", "case": case, "role": role, "pass": False}
            try:
                material = keys()
                if case == "wrong_hp":
                    baseline = scenario(args, network, "combined_conf", role, material)
                    result["baseline"] = baseline
                    if baseline["pass"]:
                        result.update(scenario(args, network, "combined_conf", role, material, wrong=True))
                    else:
                        result["failure"] = "baseline_failed"
                else:
                    result.update(scenario(args, network, case, role, material))
            except Failure as error:
                result["failure"] = str(error)
            except Exception:
                result["failure"] = "internal_error"
            outcomes.append(result["pass"])
            emit(result)
    passed = bool(outcomes) and all(outcomes)
    emit({"kind": "summary", "pass": passed, "cases": len(outcomes)})
    return 0 if passed else 1


def interrupted(signum, frame):
    # Finish owned-PID cleanup even if the caller repeats cancellation.
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    raise Interrupted()


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        code = main()
    except Interrupted:
        emit({"kind": "fatal", "pass": False, "failure": "interrupted"})
        code = 1
    except Failure as error:
        emit({"kind": "fatal", "pass": False, "failure": str(error)})
        code = 1
    except Exception:
        emit({"kind": "fatal", "pass": False, "failure": "internal_error"})
        code = 1
    raise SystemExit(code)
