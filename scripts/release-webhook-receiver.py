#!/usr/bin/env python3
import argparse
import json
import os
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

REPO_DIR = Path(__file__).resolve().parents[1]
REPORT_DIR = REPO_DIR / "docs" / "release-reports"
LOG_FILE = REPORT_DIR / "webhook-receiver.log"
LOCK_FILE = REPORT_DIR / ".webhook-job.lock"

REPORT_DIR.mkdir(parents=True, exist_ok=True)

def log(msg: str):
    ts = time.strftime("%Y-%m-%d %H:%M:%S")
    line = f"[{ts}] {msg}"
    print(line, flush=True)
    with LOG_FILE.open("a", encoding="utf-8") as f:
        f.write(line + "\n")

def should_handle_alert(payload: dict) -> bool:
    alerts = payload.get("alerts", [])
    if not alerts:
        return False

    for alert in alerts:
        status = alert.get("status")
        labels = alert.get("labels", {})

        if status != "firing":
            continue

        project = labels.get("project", "")
        alert_type = labels.get("alert_type", "")
        alertname = labels.get("alertname", "")

        if project == "slo-rollout-demo":
            return True

        if alert_type == "rollout-slo":
            return True

        if alertname.startswith("DemoAppCanary"):
            return True

    return False

def run_release_advisor(payload: dict):
    if LOCK_FILE.exists():
        log("A report job is already running, skip this alert.")
        return

    try:
        LOCK_FILE.write_text(str(os.getpid()), encoding="utf-8")

        alertnames = []
        for alert in payload.get("alerts", []):
            labels = alert.get("labels", {})
            alertnames.append(labels.get("alertname", "unknown"))

        log(f"Start release report job. alerts={alertnames}")

        env = os.environ.copy()
        env.setdefault("OLLAMA_URL", "http://127.0.0.1:11434")
        env.setdefault("MODEL", "qwen2.5:0.5b")

        subprocess.run(
            ["bash", "scripts/collect-release-report.sh"],
            cwd=str(REPO_DIR),
            env=env,
            check=False,
        )

        subprocess.run(
            ["bash", "scripts/ai-release-advisor.sh"],
            cwd=str(REPO_DIR),
            env=env,
            check=False,
        )

        log("Release report job finished.")

    except Exception as e:
        log(f"Release report job failed: {e}")

    finally:
        try:
            LOCK_FILE.unlink(missing_ok=True)
        except Exception:
            pass

class WebhookHandler(BaseHTTPRequestHandler):
    def _send_json(self, code: int, body: dict):
        data = json.dumps(body).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/healthz":
            self._send_json(200, {"status": "ok"})
        else:
            self._send_json(404, {"error": "not found"})

    def do_POST(self):
        if self.path not in ["/alertmanager", "/webhook"]:
            self._send_json(404, {"error": "not found"})
            return

        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)

        try:
            payload = json.loads(raw.decode("utf-8"))
        except Exception as e:
            log(f"Invalid JSON: {e}")
            self._send_json(400, {"error": "invalid json"})
            return

        log(f"Received webhook: status={payload.get('status')} alerts={len(payload.get('alerts', []))}")

        if should_handle_alert(payload):
            t = threading.Thread(target=run_release_advisor, args=(payload,), daemon=True)
            t.start()
            self._send_json(202, {"status": "accepted", "message": "release report job started"})
        else:
            self._send_json(200, {"status": "ignored", "message": "not a rollout-slo firing alert"})

    def log_message(self, format, *args):
        return

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=18080)
    args = parser.parse_args()

    server = HTTPServer((args.host, args.port), WebhookHandler)
    log(f"release-webhook-receiver listening on {args.host}:{args.port}")
    server.serve_forever()

if __name__ == "__main__":
    main()
