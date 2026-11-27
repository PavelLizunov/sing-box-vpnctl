"""Check a Linux TUN and its uplink against a local coturn instance.

python3 scripts/turn_tun_probe.py --sing-box ./sing-box --turnserver /usr/bin/turnserver

Requires unprivileged user/network namespaces, /dev/net/tun, iproute2, tcpdump,
OpenSSL and Linux util-linux. The outer process creates a fresh namespace before
any interface is touched. All remote addresses and TLS credentials are synthetic.
"""
import argparse
from contextlib import ExitStack
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time


PEERS = ("198.51.100.11", "2001:db8:20::11")
RESOLVERS = ("198.51.100.12", "2001:db8:20::12")
RELAYS = ("192.0.2.2", "2001:db8:10::2")
ECHO_PORT = 18001


def require(ok, message):
    if not ok:
        raise RuntimeError(str(message))


def command(*args):
    return subprocess.check_output(args, text=True, timeout=10).strip()


def wait_for(condition, message):
    for _ in range(150):
        if condition():
            return
        time.sleep(0.04)
    raise RuntimeError(message)


def launch(stack, log, *args):
    stream = stack.enter_context(log.open("wb"))
    proc = subprocess.Popen(args, stdout=stream, stderr=stream)

    def stop():
        if proc.poll() is None:
            proc.kill()
        proc.wait(timeout=5)

    stack.callback(stop)
    return proc


def query(name):
    qname = b"".join(bytes([len(label)]) + label.encode() for label in name.split(".")) + b"\0"
    return struct.pack("!6H", 0x1234, 0x0100, 1, 0, 0, 0) + qname + struct.pack("!HH", 1, 1)


def serve():
    def worker(sock, tcp, host, port):
        while True:
            if tcp:
                conn, source = sock.accept()
                with conn:
                    data = conn.recv(256)
                    conn.sendall(data)
            else:
                data, source = sock.recvfrom(2048)
                reply = data
                if port == 53:
                    answer = bytes.fromhex("c00c000100010000000a0004c0000217")
                    reply = data[:2] + b"\x81\x80" + data[4:6] + b"\x00\x01" + data[8:] + answer
                sock.sendto(reply, source)
            print(json.dumps([host, port, source[0], data.hex()]), flush=True)

    for host in PEERS + RESOLVERS:
        sockets = [(ECHO_PORT, False), (ECHO_PORT, True), (53, False)] if host in PEERS else [(53, False)]
        for port, tcp in sockets:
            sock = socket.socket(socket.AF_INET6 if ":" in host else socket.AF_INET,
                                 socket.SOCK_STREAM if tcp else socket.SOCK_DGRAM)
            sock.bind((host, port))
            if tcp:
                sock.listen()
            threading.Thread(target=worker, args=(sock, tcp, host, port), daemon=True).start()
    print("ready", flush=True)
    signal.pause()


def exchange(host, proto, label):
    dns = proto == "DNS"
    data = query(label) if dns else label.encode()
    try:
        with socket.socket(socket.AF_INET6 if ":" in host else socket.AF_INET,
                           socket.SOCK_STREAM if proto == "TCP" else socket.SOCK_DGRAM) as sock:
            sock.settimeout(1)
            sock.connect((host, 53 if dns else ECHO_PORT))
            sock.sendall(data)
            reply = sock.recv(2048)
        if dns:
            return reply[:2] == data[:2] and data[12:] in reply and reply.endswith(bytes([192, 0, 2, 23]))
        return reply == data
    except OSError:
        return False


def phase(directory, name):
    interfaces = ["probe-up"] if name == "control" else ["probe-up", "probe-tun"]
    files = {}
    with ExitStack() as captures:
        processes = []
        for interface in interfaces:
            path = directory / (name + "-" + interface + ".pcap")
            log = path.with_suffix(".log")
            files[interface] = path
            processes.append(launch(captures, log, "tcpdump", "--immediate-mode", "-Z", "root",
                                    "-n", "-i", interface, "-s0", "-U", "-w", str(path)))
            wait_for(lambda: "listening on" in log.read_text(), f"tcpdump failed on {interface}")
        for host, family in zip(PEERS, ("v4", "v6")):
            for proto in ("UDP", "DNS", "TCP"):
                label = f"{name}.{proto.lower()}.{family}.test"
                expected = name == "control" or (name == "online" and proto != "TCP")
                require(exchange(host, proto, label) == expected, f"{label}: unexpected delivery")
        time.sleep(0.1)
        for proc in processes:
            proc.send_signal(signal.SIGINT)
            require(proc.wait(timeout=3) == 0, "tcpdump failed")

    counts = {}
    for interface, path in files.items():
        def count(expr):
            with path.open("rb") as capture:
                result = subprocess.run(["tcpdump", "-n", "-r", "-", expr], stdin=capture,
                                        capture_output=True, text=True, check=False, timeout=10)
            require(result.returncode == 0, f"tcpdump could not read {path.name}: {result.stderr.strip()}")
            return len(result.stdout.splitlines())

        report = path.with_suffix(".log").read_text().splitlines()
        require("0 packets dropped by kernel" in report, "capture dropped packets")
        for host in PEERS:
            for proto, port in [("udp", ECHO_PORT), ("tcp", ECHO_PORT), ("udp", 53)]:
                matches = count(f"dst host {host} and {proto} and dst port {port}")
                require(bool(matches) == (interface == "probe-tun" or name == "control"),
                        f"{name} {interface} {host} {proto}/{port}: {matches}")
        decoy = count("(dst net 198.51.100.0/24 or dst net 2001:db8:20::/64) and (tcp or udp)")
        tls = count("tcp port 5349")
        if interface == "probe-up" and name != "control":
            require(decoy == 0, "plaintext traffic reached the uplink")
            if name == "online":
                require(tls > 0, "missing client-to-relay traffic")
        counts[interface] = {"decoy_packets": decoy, "turn_port_packets": tls}
    print(json.dumps({"phase": name, "captures": counts}), flush=True)


def execute(directory, args):
    require(os.readlink("/proc/self/ns/net") != os.environ["TURN_PROBE_PARENT_NETNS"],
            "run through the outer namespace launcher")
    require([i["ifname"] for i in json.loads(command("ip", "-j", "link"))] == ["lo"],
            "expected a newly created, empty network namespace")
    command("ip", "link", "set", "lo", "up")
    with ExitStack() as stack:
        ns = launch(stack, directory / "namespace.log", "unshare", "-n", "sleep", "infinity")
        wait_for(lambda: os.readlink(f"/proc/{ns.pid}/ns/net") != os.readlink("/proc/self/ns/net"),
                 "peer namespace did not start")
        peer = ["nsenter", "-t", str(ns.pid), "-n", "--"]
        command("ip", "link", "add", "probe-up", "type", "veth", "peer", "name", "probe-srv")
        command("ip", "link", "set", "probe-srv", "netns", str(ns.pid))
        command(*peer, "ip", "link", "set", "lo", "up")
        for prefix, dev, ip4, ip6 in [([], "probe-up", "192.0.2.1", "2001:db8:10::1"),
                                       (peer, "probe-srv", *RELAYS)]:
            command(*prefix, "ip", "link", "set", dev, "up")
            command(*prefix, "ip", "addr", "add", ip4 + "/30", "dev", dev)
            command(*prefix, "ip", "-6", "addr", "add", ip6 + "/64", "dev", dev, "nodad")
        command("ip", "route", "add", "default", "via", RELAYS[0])
        command("ip", "-6", "route", "add", "default", "via", RELAYS[1])
        for ip in PEERS + RESOLVERS:
            command(*peer, "ip", "addr", "add", ip + ("/128" if ":" in ip else "/32"), "dev", "lo")
        peer_log = directory / "peer.log"
        launch(stack, peer_log, *peer, sys.executable, __file__, "--peer")
        wait_for(lambda: "ready" in peer_log.read_text(), "decoy listeners did not start")
        phase(directory, "control")

        cert, key = directory / "cert.pem", directory / "key.pem"
        command("openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256",
                "-nodes", "-days", "1", "-subj", "/CN=relay.test", "-addext", "subjectAltName=DNS:relay.test",
                "-keyout", str(key), "-out", str(cert))
        config = directory / "turn.conf"
        config.write_text("\n".join([
            "listening-ip=" + RELAYS[0], *("relay-ip=" + ip for ip in RELAYS), "tls-listening-port=5349",
            "realm=probe.test", "lt-cred-mech", "user=probe:synthetic-test", "no-cli", "no-udp", "no-tcp",
            "no-dtls", "no-tcp-relay", "no-multicast-peers", "cert=" + str(cert), "pkey=" + str(key),
            "userdb=" + str(directory / "db"), "pidfile=" + str(directory / "pid"), "log-file=stdout",
            "relay-threads=1",
        ]) + "\n")
        relay = launch(stack, directory / "coturn.log", *peer, args.turnserver, "-c", str(config))

        def listening():
            if relay.poll() is not None:
                raise RuntimeError((directory / "coturn.log").read_text())
            try:
                with socket.create_connection((RELAYS[0], 5349), timeout=0.1):
                    return True
            except OSError:
                return False

        wait_for(listening, "coturn did not open TLS listener")
        configs = {
            "inbounds": [dict(type="tun", tag="tun", interface_name="probe-tun", stack="gvisor",
                              address=["10.72.0.1/30", "fd00:72::1/126"], dns_mode="disabled",
                              auto_route=True, strict_route=True,
                              route_exclude_address=["192.0.2.0/30", "2001:db8:10::/64"])],
            "dns": {"servers": [dict(type="udp", tag=tag, server=ip, detour="relay")
                                for tag, ip in zip(("dns4", "dns6"), RESOLVERS)],
                    "rules": [dict(domain_suffix="v6.test", action="route", server="dns6")],
                    "final": "dns4", "disable_cache": True},
            "outbounds": [dict(type="turn", tag="relay", server=RELAYS[0], server_port=5349,
                               username="probe", password="synthetic-test",
                               tls=dict(enabled=True, server_name="relay.test", certificate_path=str(cert)))],
            "route": {"auto_detect_interface": True, "rules": [dict(inbound="tun", port=53, action="hijack-dns")],
                      "final": "relay"},
        }
        config = directory / "sing.json"
        config.write_text(json.dumps(configs))
        core_log = directory / "core.log"
        core = launch(stack, core_log, args.sing_box, "run", "-c", str(config))

        def routed():
            if core.poll() is not None:
                raise RuntimeError(core_log.read_text())
            return all("dev probe-tun" in command("ip", "route", "get", ip) for ip in PEERS)

        wait_for(routed, "IPv4/IPv6 decoy routes did not enter TUN")
        require("dev probe-up" in command("ip", "route", "get", RELAYS[0]), "bad relay route")
        phase(directory, "online")
        relay.kill()
        relay.wait(timeout=3)
        phase(directory, "offline")
        records = [json.loads(line) for line in peer_log.read_text().splitlines() if line.startswith("[")]
        for index, family in enumerate(("v4", "v6")):
            datagram = f"online.udp.{family}.test".encode().hex()
            require([PEERS[index], ECHO_PORT, RELAYS[index], datagram] in records, "incorrect UDP source")
            message = query(f"online.dns.{family}.test").hex()[4:]
            require(any(row[:3] == [RESOLVERS[index], 53, RELAYS[index]] and row[3][4:] == message
                        for row in records), "incorrect DNS source or question")
            for proto in ("udp", "tcp", "dns"):
                label = f"offline.{proto}.{family}.test"
                marker = query(label).hex()[4:] if proto == "dns" else label.encode().hex()
                require(all(marker not in row[3] for row in records), "decoy received traffic during outage")


if __name__ == "__main__":
    if sys.argv[1:] == ["--peer"]:
        serve()
    else:
        parser = argparse.ArgumentParser(description=__doc__)
        parser.add_argument("--sing-box", required=True)
        parser.add_argument("--turnserver", default="turnserver")
        parser.add_argument("--isolated", action="store_true", help=argparse.SUPPRESS)
        args = parser.parse_args()
        if args.isolated:
            with tempfile.TemporaryDirectory(prefix="turn-tun-") as tmp:
                execute(Path(tmp), args)
        else:
            for field in ("sing_box", "turnserver"):
                binary = shutil.which(getattr(args, field))
                require(binary, f"missing executable: {getattr(args, field)}")
                setattr(args, field, str(Path(binary).resolve()))
            env = dict(os.environ, LC_ALL="C", TURN_PROBE_PARENT_NETNS=os.readlink("/proc/self/ns/net"))
            os.execvpe("unshare", ["unshare", "-Urnm", "--propagation", "private", "--", sys.executable,
                                   __file__, "--isolated", "--sing-box", args.sing_box,
                                   "--turnserver", args.turnserver], env)
