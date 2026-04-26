# -*- coding: utf-8 -*-
"""
HADES System Manager - Start/Stop/Status

Usage:
    python start_hades.py          # Start all (background)
    python start_hades.py --status # Check status
    python start_hades.py --stop   # Stop all
"""

import argparse
import json
import os
import signal
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

KST = timezone(__import__("datetime").timedelta(hours=9))
BASE_DIR = Path(__file__).resolve().parent
PROJECT_DIR = BASE_DIR / "project" / "bigvolver"
DATA_DIR = PROJECT_DIR / "internal" / "data"
SCRIPTS_DIR = PROJECT_DIR / "scripts"
ML_SERVICE_DIR = PROJECT_DIR / "ml_service"
MODELS_DIR = PROJECT_DIR / "models"
DB_PATH = BASE_DIR / "bigvolver.db"
PID_FILE = BASE_DIR / ".hades_pids.json"
LOG_DIR = BASE_DIR / "logs"
LOG_DIR.mkdir(exist_ok=True)

SYMBOLS = ["BTCUSDT", "ETHUSDT", "SOLUSDT"]
ENGINE_PORT = 8081
ML_PORT = 5001


def log(msg):
    ts = datetime.now(KST).strftime("%H:%M:%S")
    try:
        print("[{}] {}".format(ts, msg))
    except UnicodeEncodeError:
        print("[{}] {}".format(ts, msg.encode("ascii", "replace").decode()))


def save_pids(procs):
    with open(PID_FILE, "w") as f:
        json.dump({k: v.pid for k, v in procs.items()}, f, indent=2)


def load_pids():
    if not PID_FILE.exists():
        return {}
    with open(PID_FILE) as f:
        return json.load(f)


def kill_pids():
    pids = load_pids()
    if not pids:
        log("No running processes")
        return
    for name, pid in pids.items():
        try:
            os.kill(pid, signal.SIGTERM)
            log("  {} (PID {}) stopped".format(name, pid))
        except ProcessLookupError:
            log("  {} (PID {}) already stopped".format(name, pid))
    time.sleep(2)
    PID_FILE.unlink(missing_ok=True)
    log("All stopped")


def check_status():
    pids = load_pids()
    if not pids:
        log("HADES not running")
        return
    log("=== HADES Process Status ===")
    for name, pid in pids.items():
        try:
            os.kill(pid, 0)
            log("  {}: PID {} [RUNNING]".format(name, pid))
        except ProcessLookupError:
            log("  {}: PID {} [STOPPED]".format(name, pid))

    import urllib.request
    try:
        with urllib.request.urlopen("http://localhost:{}/health".format(ML_PORT), timeout=3) as resp:
            health = json.loads(resp.read().decode())
            log("  ML Service: {} (model: {})".format(health.get("status"), health.get("model_version")))
    except Exception:
        log("  ML Service: [OFFLINE]")

    try:
        with urllib.request.urlopen("http://localhost:{}/api/v1/status".format(ENGINE_PORT), timeout=3) as resp:
            status = json.loads(resp.read().decode())
            running = status.get("running", False)
            log("  DualEngine: [RUNNING]" if running else "  DualEngine: [OFFLINE]")
    except Exception:
        log("  DualEngine: [OFFLINE]")


def run_backfill():
    log("=== Data Backfill ===")
    import sqlite3

    conn = sqlite3.connect(str(DB_PATH))
    try:
        count_5m = conn.execute("SELECT COUNT(*) FROM market_5m_candles").fetchone()[0]
    except sqlite3.OperationalError:
        count_5m = 0
    conn.close()

    if count_5m < 10000:
        log("5m backfill starting (90 days)...")
        env = dict(os.environ, PYTHONIOENCODING="utf-8")
        proc = subprocess.run(
            [sys.executable, str(DATA_DIR / "hades_5m_collector.py"),
             "--symbols"] + SYMBOLS + ["--days", "90", "--db", str(DB_PATH)],
            cwd=str(DATA_DIR), timeout=600, env=env,
        )
        if proc.returncode != 0:
            log("[WARN] 5m backfill failed, continuing")
    else:
        log("5m data OK ({} candles), skip".format(count_5m))

    conn = sqlite3.connect(str(DB_PATH))
    try:
        count_1h = conn.execute("SELECT COUNT(*) FROM market_1h_candles").fetchone()[0]
    except sqlite3.OperationalError:
        count_1h = 0
    conn.close()

    if count_1h < 5000:
        log("1h backfill starting (90 days)...")
        env = dict(os.environ, PYTHONIOENCODING="utf-8")
        proc = subprocess.run(
            [sys.executable, str(DATA_DIR / "data_collector.py"),
             "--symbols"] + SYMBOLS + ["--days", "90", "--db", str(DB_PATH)],
            cwd=str(DATA_DIR), timeout=600, env=env,
        )
        if proc.returncode != 0:
            log("[WARN] 1h backfill failed, continuing")
    else:
        log("1h data OK ({} candles), skip".format(count_1h))


def train_if_needed():
    model_path = MODELS_DIR / "lightgbm_model.txt"
    if model_path.exists():
        log("ML model exists, skip training")
        return True

    log("No ML model, starting training...")
    MODELS_DIR.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, PYTHONIOENCODING="utf-8")

    proc = subprocess.run(
        [sys.executable, str(DATA_DIR / "prepare_training.py"),
         "--db", str(DB_PATH), "--symbol", "BTCUSDT"],
        cwd=str(DATA_DIR), timeout=300, env=env,
    )
    if proc.returncode != 0:
        log("[WARN] Training data prep failed")
        return False

    proc = subprocess.run(
        [sys.executable, str(DATA_DIR / "train_model.py"), "BTCUSDT"],
        cwd=str(DATA_DIR), timeout=300, env=env,
    )
    if proc.returncode != 0:
        log("[WARN] ML training failed")
        return False

    log("ML model trained OK")
    return True


def build_dual_engine():
    cmd_dir = PROJECT_DIR / "cmd" / "dual_engine"
    bin_path = BASE_DIR / "dual_engine.exe"

    if bin_path.exists():
        log("DualEngine binary exists, skip build")
        return str(bin_path)

    log("Building DualEngine...")
    try:
        subprocess.run(
            ["go", "build", "-o", str(bin_path), "."],
            cwd=str(cmd_dir), timeout=120, check=True,
        )
        log("DualEngine built: {}".format(bin_path))
        return str(bin_path)
    except FileNotFoundError:
        log("[WARN] Go compiler not found")
        return None
    except subprocess.CalledProcessError as e:
        log("[WARN] DualEngine build failed: {}".format(e))
        return None


def start_all(foreground=False):
    log("=== HADES System Starting ===")

    run_backfill()
    train_if_needed()

    procs = {}

    # ML Service
    log("Starting ML Service (:{} )...".format(ML_PORT))
    ml_log = open(LOG_DIR / "ml_service.log", "a")
    ml_env = dict(os.environ, MODEL_DIR=str(MODELS_DIR), PYTHONIOENCODING="utf-8")
    ml_proc = subprocess.Popen(
        [sys.executable, str(ML_SERVICE_DIR / "server.py")],
        cwd=str(ML_SERVICE_DIR), stdout=ml_log, stderr=ml_log, env=ml_env,
    )
    procs["ml_service"] = ml_proc
    log("  ML Service PID: {}".format(ml_proc.pid))
    time.sleep(3)

    # DualEngine
    bin_path = build_dual_engine()
    if bin_path:
        log("Starting DualEngine (:{} )...".format(ENGINE_PORT))
        engine_log = open(LOG_DIR / "dual_engine.log", "a")
        env_engine = dict(os.environ, WEBHOOK_PORT=str(ENGINE_PORT))
        engine_proc = subprocess.Popen(
            [bin_path],
            stdout=engine_log, stderr=engine_log, env=env_engine,
        )
        procs["dual_engine"] = engine_proc
        log("  DualEngine PID: {}".format(engine_proc.pid))
        time.sleep(2)

    # HADES Loop
    log("Starting HADES Loop...")
    hades_log = open(LOG_DIR / "hades.log", "a")
    hades_proc = subprocess.Popen(
        [sys.executable, str(SCRIPTS_DIR / "run_hades.py"),
         "--symbols"] + SYMBOLS +
         ["--db", str(DB_PATH),
          "--ml-url", "http://localhost:{}".format(ML_PORT),
          "--engine-url", "http://localhost:{}".format(ENGINE_PORT)],
        cwd=str(SCRIPTS_DIR), stdout=hades_log, stderr=hades_log,
    )
    procs["hades_loop"] = hades_proc
    log("  HADES Loop PID: {}".format(hades_proc.pid))

    save_pids(procs)

    log("")
    log("=== HADES System Started ===")
    log("  ML Service:   http://localhost:{}/health".format(ML_PORT))
    log("  DualEngine:   http://localhost:{}/api/v1/status".format(ENGINE_PORT))
    log("  Logs:         {}".format(LOG_DIR))
    log("  Stop:         python start_hades.py --stop")

    if foreground:
        log("")
        log("Foreground mode - Ctrl+C to stop")
        try:
            for proc in procs.values():
                proc.wait()
        except KeyboardInterrupt:
            log("Shutting down...")
            kill_pids()
    else:
        try:
            while True:
                time.sleep(30)
                for name, proc in procs.items():
                    if proc.poll() is not None:
                        log("[WARN] {} (PID {}) exited (code: {})".format(name, proc.pid, proc.returncode))
        except KeyboardInterrupt:
            kill_pids()


def main():
    parser = argparse.ArgumentParser(description="HADES System Manager")
    parser.add_argument("--stop", action="store_true")
    parser.add_argument("--status", action="store_true")
    parser.add_argument("--foreground", action="store_true")
    args = parser.parse_args()

    if args.stop:
        kill_pids()
    elif args.status:
        check_status()
    else:
        start_all(foreground=args.foreground)


if __name__ == "__main__":
    main()
