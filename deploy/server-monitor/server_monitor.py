#!/usr/bin/env python3
import json
import hmac
import os
import re
import shutil
import subprocess
import time
import urllib.error
import urllib.request
from collections import deque
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Lock, Thread
from urllib.parse import parse_qs, urlparse


HOST = os.environ.get("MONITOR_HOST", "127.0.0.1")
TOKEN = os.environ.get("SERVER_MONITOR_TOKEN", "")
ACTIVE_RELEASE_FILE = os.environ.get("MONITOR_ACTIVE_RELEASE_FILE", "/opt/server-monitor/active-release.json")
POSTGRES_CONTAINER = os.environ.get("POSTGRES_CONTAINER", "sub2api-hk-postgres")
REDIS_CONTAINER = os.environ.get("REDIS_CONTAINER", "sub2api-hk-redis")
PORT = int(os.environ.get("MONITOR_PORT", "18787"))
EDGE_CONTAINER = os.environ.get("EDGE_CONTAINER", "starlight-relay-caddy-1")
LOG_SERVICES = {"sub2api", "postgres", "redis", "caddy", "server-monitor"}
SAMPLE_INTERVAL_SECONDS = 5
SAMPLE_EXPORT_COUNT = 10
SAMPLE_HISTORY_LIMIT = 240
SAMPLE_HISTORY = deque(maxlen=SAMPLE_HISTORY_LIMIT)
SAMPLE_LOCK = Lock()
LAST_NETWORK_SAMPLE = None
LATEST_SUMMARY = None
SECRET_PATTERNS = [
    re.compile(r"sk-[A-Za-z0-9_-]{12,}"),
    re.compile(r"(Authorization:\s*Bearer\s+)[A-Za-z0-9._~+/=-]+", re.I),
    re.compile(r"([?&](?:key|token|password|secret)=)[^\s&]+", re.I),
    re.compile(r"((?:X-Monitor-Token|SERVER_MONITOR_TOKEN)\s*[:=]\s*)[^\s,]+", re.I),
]


def now_iso():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def active_release():
    # The cutover process atomically publishes the routed release, not a candidate.
    try:
        data = json.loads(read_text(ACTIVE_RELEASE_FILE))
        container = data["container"]
        url = data["health_url"]
        parsed = urlparse(url)
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", container):
            return None
        if parsed.scheme != "http" or parsed.hostname not in {"127.0.0.1", "localhost"} or parsed.username or parsed.password:
            return None
        if parsed.path != "/health" or parsed.query or parsed.fragment:
            return None
        return {"container": container, "health_url": url}
    except (ValueError, KeyError, TypeError):
        return None


def run(cmd, timeout=3):
    try:
        proc = subprocess.run(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=timeout,
            check=False,
        )
        return {
            "ok": proc.returncode == 0,
            "code": proc.returncode,
            "stdout": proc.stdout.strip(),
            "stderr": proc.stderr.strip(),
        }
    except subprocess.TimeoutExpired as exc:
        return {"ok": False, "code": 124, "stdout": exc.stdout or "", "stderr": "timeout"}
    except Exception as exc:
        return {"ok": False, "code": 1, "stdout": "", "stderr": str(exc)}


def read_text(path):
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return handle.read()
    except Exception:
        return ""


def parse_meminfo():
    values = {}
    for line in read_text("/proc/meminfo").splitlines():
        parts = line.split()
        if len(parts) >= 2:
            values[parts[0].rstrip(":")] = int(parts[1]) * 1024
    total = values.get("MemTotal", 0)
    available = values.get("MemAvailable", 0)
    used = max(total - available, 0)
    percent = (used / total * 100) if total else 0
    return {"total": total, "used": used, "available": available, "percent": round(percent, 1)}


def cpu_totals():
    first = read_text("/proc/stat").splitlines()[0].split()[1:]
    values = [int(v) for v in first]
    idle = values[3] + (values[4] if len(values) > 4 else 0)
    total = sum(values)
    return idle, total


def cpu_percent():
    idle1, total1 = cpu_totals()
    time.sleep(0.12)
    idle2, total2 = cpu_totals()
    total_delta = total2 - total1
    idle_delta = idle2 - idle1
    if total_delta <= 0:
        return 0.0
    return round((1 - idle_delta / total_delta) * 100, 1)


def uptime_seconds():
    raw = read_text("/proc/uptime").split()
    if not raw:
        return 0
    return int(float(raw[0]))


def disk_usage(path="/"):
    usage = shutil.disk_usage(path)
    percent = usage.used / usage.total * 100 if usage.total else 0
    return {
        "path": path,
        "total": usage.total,
        "used": usage.used,
        "free": usage.free,
        "percent": round(percent, 1),
    }


def network_totals():
    rx = 0
    tx = 0
    devices = []
    for line in read_text("/proc/net/dev").splitlines()[2:]:
        if ":" not in line:
            continue
        name, rest = line.split(":", 1)
        name = name.strip()
        fields = rest.split()
        if len(fields) < 16 or name == "lo" or name.startswith(("veth", "docker", "br-")):
            continue
        rx_bytes = int(fields[0])
        tx_bytes = int(fields[8])
        rx += rx_bytes
        tx += tx_bytes
        devices.append({"name": name, "rx": rx_bytes, "tx": tx_bytes})
    return {"rx": rx, "tx": tx, "devices": devices}


def local_probe(name, url):
    start = time.time()
    try:
        req = urllib.request.Request(url, headers={"Host": "starlight123.top"})
        with urllib.request.urlopen(req, timeout=2) as resp:
            body = resp.read(300).decode("utf-8", errors="replace")
            return {
                "name": name,
                "ok": 200 <= resp.status < 400,
                "status": resp.status,
                "ms": round((time.time() - start) * 1000),
                "body": body,
            }
    except urllib.error.HTTPError as exc:
        return {
            "name": name,
            "ok": False,
            "status": exc.code,
            "ms": round((time.time() - start) * 1000),
            "body": exc.reason,
        }
    except Exception as exc:
        return {
            "name": name,
            "ok": False,
            "status": 0,
            "ms": round((time.time() - start) * 1000),
            "body": str(exc),
        }


def docker_containers():
    result = run(["docker", "ps", "-a", "--format", "{{json .}}"], timeout=4)
    rows = []
    for line in result["stdout"].splitlines():
        try:
            item = json.loads(line)
        except Exception:
            continue
        rows.append(
            {
                "name": item.get("Names", ""),
                "image": item.get("Image", ""),
                "state": item.get("State", ""),
                "status": item.get("Status", ""),
                "ports": item.get("Ports", ""),
            }
        )
    return rows


def service_state(name):
    result = run(["systemctl", "is-active", name], timeout=2)
    return {"name": name, "active": result["stdout"].strip(), "ok": result["stdout"].strip() == "active"}


def container_state(container):
    result = run(
        [
            "docker",
            "inspect",
            "--format",
            "{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}",
            container,
        ],
        timeout=3,
    )
    parts = result["stdout"].split("|", 1) if result["stdout"] else []
    status = parts[0] if parts else ""
    health = parts[1] if len(parts) > 1 else ""
    return {
        "name": container,
        "active": status,
        "health": health,
        "ok": result["ok"] and status == "running" and health in {"", "none", "healthy"},
    }


def ports():
    result = run(["ss", "-ltnp"], timeout=2)
    rows = []
    for line in result["stdout"].splitlines()[1:]:
        if any(port in line for port in (":22", ":80", ":443", ":8080", ":18089", ":18787", ":5432", ":6379")):
            rows.append(line)
    return rows


def redact(text):
    for pattern in SECRET_PATTERNS:
        text = pattern.sub(lambda m: (m.group(1) if m.lastindex else "") + "[redacted]", text)
    return text


def collect_summary(include_samples=True):
    load = os.getloadavg()
    release = active_release()
    data = {
        "generated_at": now_iso(),
        "host": {
            "hostname": os.uname().nodename,
            "kernel": os.uname().release,
            "uptime_seconds": uptime_seconds(),
            "load": [round(v, 2) for v in load],
        },
        "cpu": {"percent": cpu_percent(), "cores": os.cpu_count() or 0},
        "memory": parse_meminfo(),
        "disk": disk_usage("/"),
        "network": network_totals(),
        "services": [
            container_state(release["container"]) if release else {"name": "sub2api", "ok": False, "active": "active release not configured"},
            container_state(EDGE_CONTAINER),
            service_state("docker"),
            service_state("server-monitor"),
        ],
        "probes": [
            local_probe("sub2api 应用", release["health_url"]) if release else {"name": "sub2api", "ok": False, "status": 0, "ms": 0, "body": "active release not configured"},
            local_probe("Caddy 根路径", "http://127.0.0.1/health"),
            local_probe("Caddy /starlightai", "http://127.0.0.1/starlightai/health"),
        ],
        "containers": docker_containers(),
        "ports": ports(),
    }
    if include_samples:
        with SAMPLE_LOCK:
            data["samples"] = list(SAMPLE_HISTORY)[-SAMPLE_EXPORT_COUNT:]
    return data


def sample_from_summary(data):
    global LAST_NETWORK_SAMPLE
    generated_at = data.get("generated_at") or now_iso()
    timestamp_ms = int(time.time() * 1000)
    network = data.get("network") or {}
    rx_total = float(network.get("rx") or 0)
    tx_total = float(network.get("tx") or 0)
    rx_rate = 0.0
    tx_rate = 0.0
    if LAST_NETWORK_SAMPLE and timestamp_ms > LAST_NETWORK_SAMPLE["at"]:
        seconds = (timestamp_ms - LAST_NETWORK_SAMPLE["at"]) / 1000
        rx_rate = max(0.0, (rx_total - LAST_NETWORK_SAMPLE["rx"]) / seconds)
        tx_rate = max(0.0, (tx_total - LAST_NETWORK_SAMPLE["tx"]) / seconds)
    LAST_NETWORK_SAMPLE = {"at": timestamp_ms, "rx": rx_total, "tx": tx_total}
    return {
        "at": timestamp_ms,
        "generated_at": generated_at,
        "cpu": float((data.get("cpu") or {}).get("percent") or 0),
        "mem": float((data.get("memory") or {}).get("percent") or 0),
        "disk": float((data.get("disk") or {}).get("percent") or 0),
        "mem_used": int((data.get("memory") or {}).get("used") or 0),
        "mem_total": int((data.get("memory") or {}).get("total") or 0),
        "mem_available": int((data.get("memory") or {}).get("available") or 0),
        "disk_used": int((data.get("disk") or {}).get("used") or 0),
        "disk_total": int((data.get("disk") or {}).get("total") or 0),
        "disk_free": int((data.get("disk") or {}).get("free") or 0),
        "rx": rx_rate,
        "tx": tx_rate,
    }


def record_sample():
    global LATEST_SUMMARY
    data = collect_summary(include_samples=False)
    sample = sample_from_summary(data)
    with SAMPLE_LOCK:
        SAMPLE_HISTORY.append(sample)
        LATEST_SUMMARY = data
    return sample


def sampler_loop():
    while True:
        try:
            record_sample()
        except Exception as exc:
            print(f"server-monitor sample failed: {exc}", flush=True)
        time.sleep(SAMPLE_INTERVAL_SECONDS)


def logs_for(service, lines):
    lines = max(20, min(lines, 200))
    if service == "sub2api":
        release = active_release()
        if not release:
            return {"service": service, "lines": "", "ok": False, "error": "active release not configured"}
        cmd = ["docker", "logs", "--tail", str(lines), release["container"]]
    elif service in {"postgres", "redis"}:
        container = POSTGRES_CONTAINER if service == "postgres" else REDIS_CONTAINER
        cmd = ["docker", "logs", "--tail", str(lines), container]
    elif service == "caddy":
        cmd = ["docker", "logs", "--tail", str(lines), EDGE_CONTAINER]
    elif service == "server-monitor":
        cmd = ["journalctl", "-u", "server-monitor", "-n", str(lines), "--no-pager", "-o", "short-iso"]
    else:
        return {"service": service, "lines": "", "error": "unsupported service"}
    result = run(cmd, timeout=5)
    text = str(result["stdout"]) + chr(10) + str(result["stderr"])
    text = text[-250000:]
    return {"service": service, "lines": redact(text), "ok": result["ok"], "code": result["code"]}


class Handler(BaseHTTPRequestHandler):
    server_version = "HKServerMonitor/1.0"

    def do_GET(self):
        supplied = self.headers.get("X-Monitor-Token", "")
        if not TOKEN or not hmac.compare_digest(supplied.encode(), TOKEN.encode()):
            self.write_json({"error": "unauthorized"}, status=401)
            return
        parsed = urlparse(self.path)
        if parsed.path == "/api/health":
            self.write_json({"status": "ok", "generated_at": now_iso()})
            return
        if parsed.path == "/api/summary":
            with SAMPLE_LOCK:
                data = dict(LATEST_SUMMARY or {})
                data["samples"] = list(SAMPLE_HISTORY)[-SAMPLE_EXPORT_COUNT:]
            if not data.get("generated_at"):
                self.write_json({"error": "collector warming up"}, status=503)
                return
            self.write_json(data)
            return
        if parsed.path == "/api/logs":
            qs = parse_qs(parsed.query)
            service = qs.get("service", ["sub2api"])[0]
            if service not in LOG_SERVICES:
                self.write_json({"error": "unsupported service"}, status=400)
                return
            try:
                lines = int(qs.get("lines", ["80"])[0])
            except ValueError:
                lines = 80
            self.write_json(logs_for(service, lines))
            return
        self.write_json({"error": "not found"}, status=404)

    def log_message(self, fmt, *args):
        return

    def write_json(self, value, status=200):
        body = json.dumps(value, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    if len(TOKEN) < 32:
        raise SystemExit("SERVER_MONITOR_TOKEN must contain at least 32 characters")
    record_sample()
    Thread(target=sampler_loop, daemon=True).start()
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"server-monitor listening on {HOST}:{PORT}", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()
