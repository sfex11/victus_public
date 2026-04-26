# -*- coding: utf-8 -*-
"""
HADES + NFI vs BigVolver Dual Engine Dashboard

Real-time HTML dashboard. Auto-refreshes every 30 seconds.

Usage:
    python dashboard.py                  # Launch on :8888
    python dashboard.py --port 8888      # Custom port
"""

import json
import urllib.request
import os
from datetime import datetime, timezone
from http.server import HTTPServer, SimpleHTTPRequestHandler

KST = timezone(__import__("datetime").timedelta(hours=9))
ENGINE_URL = os.environ.get("ENGINE_URL", "http://localhost:8081")
ML_URL = os.environ.get("ML_URL", "http://localhost:5001")
DEFAULT_PORT = 8888

CSS = """
* { margin:0; padding:0; box-sizing:border-box; }
body { font-family: 'Segoe UI', sans-serif; background:#0d1117; color:#e6edf3; padding:20px; }
h1 { font-size:24px; margin-bottom:4px; }
.subtitle { color:#8b949e; font-size:13px; margin-bottom:20px; }
.grid { display:grid; grid-template-columns:1fr 1fr; gap:16px; margin-bottom:20px; }
.card { background:#161b22; border:1px solid #30363d; border-radius:8px; padding:20px; }
.card h2 { font-size:16px; color:#8b949e; margin-bottom:12px; }
.big-num { font-size:36px; font-weight:bold; }
.label { font-size:12px; color:#8b949e; margin-top:4px; }
.row { display:flex; justify-content:space-between; padding:8px 0; border-bottom:1px solid #21262d; }
.row:last-child { border:none; }
.stat-label { color:#8b949e; font-size:13px; }
.stat-val { font-weight:bold; font-size:14px; }
table { width:100%; border-collapse:collapse; font-size:13px; }
th { text-align:left; padding:8px; border-bottom:1px solid #30363d; color:#8b949e; }
td { padding:6px 8px; border-bottom:1px solid #21262d; }
.badges { display:flex; gap:8px; margin-bottom:16px; align-items:center; }
.vs { text-align:center; font-size:28px; color:#f0883e; font-weight:bold; padding:20px 0; }
@media (max-width:768px) { .grid { grid-template-columns:1fr; } }
"""


def fetch_json(url, timeout=5):
    try:
        with urllib.request.urlopen(url, timeout=timeout) as resp:
            return json.loads(resp.read().decode())
    except Exception:
        return None


def build_html():
    status = fetch_json(ENGINE_URL + "/api/v1/status")
    signals = fetch_json(ENGINE_URL + "/api/v1/signals")
    ml = fetch_json(ML_URL + "/health")

    if not status:
        return "<h1>Cannot connect to DualEngine at {}</h1>".format(ENGINE_URL)

    now = datetime.now(KST).strftime("%Y-%m-%d %H:%M:%S")
    running = status.get("running", False)
    nfi = status.get("nfi", {})
    bv = status.get("bigvolver", {})
    initial = status.get("initial_capital", 10000)

    nfi_eq = nfi.get("equity", 0)
    bv_eq = bv.get("equity", 0)
    nfi_pnl = nfi.get("total_pnl", 0)
    bv_pnl = bv.get("total_pnl", 0)

    ml_ver = ml.get("model_version", "N/A") if ml else "N/A"
    ml_ok = ml.get("status") == "ok" if ml else False

    recent = signals if isinstance(signals, list) else []
    if len(recent) > 20:
        recent = recent[-20:]

    def pnl_color(v):
        return "#4caf50" if v > 0 else "#f44336" if v < 0 else "#9e9e9e"

    def badge(ok):
        c = "#4caf50" if ok else "#f44336"
        t = "RUNNING" if ok else "STOPPED"
        return '<span style="background:{};color:white;padding:2px 8px;border-radius:10px;font-size:12px">{}</span>'.format(c, t)

    signals_rows = ""
    for s in reversed(recent):
        ts = s.get("timestamp", "")[:19]
        src = s.get("source", "")[:15]
        pair = s.get("pair", "")
        direction = s.get("direction", "")
        conf = s.get("confidence", 0)
        dc = "#4caf50" if direction == "LONG" else "#f44336" if direction == "SHORT" else "#9e9e9e"
        signals_rows += "<tr><td>{}</td><td>{}</td><td>{}</td><td style='color:{};font-weight:bold'>{}</td><td>{:.2f}</td></tr>\n".format(ts, src, pair, dc, direction, conf)

    html_parts = [
        "<!DOCTYPE html><html lang='ko'><head><meta charset='UTF-8'>",
        "<meta http-equiv='refresh' content='30'>",
        "<title>HADES Dual Engine</title>",
        "<style>", CSS, "</style></head><body>",
        "<h1>HADES Dual Engine</h1>",
        "<p class='subtitle'>NFI vs BigVolver (HADES) Paper Trading</p>",
        "<div class='badges'>",
        "  Engine: {} &nbsp;&nbsp; ML: {} &nbsp;&nbsp; Updated: {}".format(badge(running), badge(ml_ok), now),
        "</div>",
        "<div class='grid'>",
        # NFI Card
        "  <div class='card'>",
        "    <h2>NFI (Freqtrade)</h2>",
        "    <div class='big-num' style='color:{}'>${:,.2f}</div>".format(pnl_color(nfi_pnl), nfi_eq),
        "    <div class='label'>PnL: {:+.2f}</div>".format(nfi_pnl),
        "    <div class='row'><span class='stat-label'>Open</span><span class='stat-val'>{}</span></div>".format(nfi.get("open_positions", 0)),
        "    <div class='row'><span class='stat-label'>Trades</span><span class='stat-val'>{}</span></div>".format(nfi.get("total_trades", 0)),
        "    <div class='row'><span class='stat-label'>Win Rate</span><span class='stat-val'>{:.1f}%</span></div>".format(nfi.get("win_rate", 0)),
        "    <div class='row'><span class='stat-label'>Max DD</span><span class='stat-val'>{:.2f}%</span></div>".format(nfi.get("max_dd", 0)),
        "  </div>",
        # BigV Card
        "  <div class='card'>",
        "    <h2>HADES (BigVolver ML)</h2>",
        "    <div class='big-num' style='color:{}'>${:,.2f}</div>".format(pnl_color(bv_pnl), bv_eq),
        "    <div class='label'>PnL: {:+.2f}</div>".format(bv_pnl),
        "    <div class='row'><span class='stat-label'>Open</span><span class='stat-val'>{}</span></div>".format(bv.get("open_positions", 0)),
        "    <div class='row'><span class='stat-label'>Trades</span><span class='stat-val'>{}</span></div>".format(bv.get("total_trades", 0)),
        "    <div class='row'><span class='stat-label'>Win Rate</span><span class='stat-val'>{:.1f}%</span></div>".format(bv.get("win_rate", 0)),
        "    <div class='row'><span class='stat-label'>Max DD</span><span class='stat-val'>{:.2f}%</span></div>".format(bv.get("max_dd", 0)),
        "  </div>",
        "</div>",
        # VS
        "<div class='vs'>NFI ${:,.2f} &nbsp;vs&nbsp; HADES ${:,.2f} &nbsp;&nbsp; Delta ${:,.2f}</div>".format(nfi_eq, bv_eq, bv_eq - nfi_eq),
        # Signals
        "<div class='card'>",
        "  <h2>Recent Signals</h2>",
        "  <table><tr><th>Time</th><th>Source</th><th>Pair</th><th>Dir</th><th>Conf</th></tr>",
        signals_rows,
        "  </table>",
        "</div>",
        # ML
        "<div class='card'>",
        "  <h2>ML Model</h2>",
        "  <div class='row'><span class='stat-label'>Version</span><span class='stat-val'>{}</span></div>".format(ml_ver),
        "</div>",
        "</body></html>",
    ]

    return "\n".join(html_parts)


class Handler(SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.path in ("/", "/index.html"):
            html = build_html().encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", len(html))
            self.end_headers()
            self.wfile.write(html)
        elif self.path.startswith("/api/"):
            url_map = {
                "/api/status": ENGINE_URL + "/api/v1/status",
                "/api/signals": ENGINE_URL + "/api/v1/signals",
                "/api/ml": ML_URL + "/health",
            }
            target = url_map.get(self.path)
            if target:
                data = fetch_json(target)
                body = json.dumps(data, indent=2, ensure_ascii=False).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", len(body))
                self.end_headers()
                self.wfile.write(body)
            else:
                self.send_error(404)
        else:
            self.send_error(404)

    def log_message(self, fmt, *args):
        pass


def main():
    import argparse
    p = argparse.ArgumentParser()
    p.add_argument("--port", type=int, default=DEFAULT_PORT)
    args = p.parse_args()
    server = HTTPServer(("0.0.0.0", args.port), Handler)
    print("Dashboard: http://localhost:{}".format(args.port))
    print("Press Ctrl+C to stop")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nStopped")
        server.server_close()


if __name__ == "__main__":
    main()
