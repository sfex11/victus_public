"""
HADES 전체 시스템 자동 기동/종료 스크립트

Usage:
    # 전체 시스템 기동 (백그라운드)
    python start_hades.py

    # 상태 확인
    python start_hades.py --status

    # 전체 종료
    python start_hades.py --stop

    # 포그라운드 (로그 실시간 보기)
    python start_hades.py --foreground

자동으로 다음 순서로 실행:
    1. 5분봉 백필 (최초 1회, 90일)
    2. 1h봉 백필 (최초 1회, 90일)
    3. ML 모델 학습 (모델 없으면 자동)
    4. ML 서비스 기동 (:5001)
    5. Go DualEngine 기동 (:8080)
    6. HADES 루프 기동
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


def log(msg):
    ts = datetime.now(KST).strftime("%H:%M:%S")
    print(f"[{ts}] {msg}")


def save_pids(procs: dict):
    with open(PID_FILE, "w") as f:
        json.dump({k: v.pid for k, v in procs.items()}, f, indent=2)


def load_pids() -> dict:
    if not PID_FILE.exists():
        return {}
    with open(PID_FILE) as f:
        return json.load(f)


def kill_pids():
    pids = load_pids()
    if not pids:
        log("실행 중인 프로세스 없음")
        return

    for name, pid in pids.items():
        try:
            os.kill(pid, signal.SIGTERM)
            log(f"  {name} (PID {pid}) 종료 요청")
        except ProcessLookupError:
            log(f"  {name} (PID {pid}) 이미 종료됨")

    time.sleep(2)
    PID_FILE.unlink(missing_ok=True)
    log("전체 종료 완료")


def check_status():
    pids = load_pids()
    if not pids:
        log("HADES 미실행 중")
        return

    log("=== HADES 프로세스 상태 ===")
    for name, pid in pids.items():
        try:
            os.kill(pid, 0)
            status = "✅ 실행 중"
        except ProcessLookupError:
            status = "❌ 종료됨"
        log(f"  {name}: PID {pid} {status}")

    # ML 서비스 헬스체크
    try:
        import urllib.request
        with urllib.request.urlopen("http://localhost:5001/health", timeout=3) as resp:
            health = json.loads(resp.read().decode())
            log(f"  ML 서비스: {health.get('status')} (model: {health.get('model_version')})")
    except:
        log(f"  ML 서비스: ❌ 연결 불가")

    # DualEngine 상태
    try:
        with urllib.request.urlopen("http://localhost:8080/api/v1/status", timeout=3) as resp:
            status = json.loads(resp.read().decode())
            running = status.get("running", False)
            log(f"  DualEngine: {'✅ 실행 중' if running else '❌ 미실행'}")
    except:
        log(f"  DualEngine: ❌ 연결 불가")


def run_backfill():
    """5분봉 + 1h봉 백필 (최초 1회)"""
    log("=== 데이터 백필 ===")

    # 5분봉 백필 여부 확인
    import sqlite3
    conn = sqlite3.connect(str(DB_PATH))
    count_5m = conn.execute("SELECT COUNT(*) FROM market_5m_candles").fetchone()[0]
    conn.close()

    if count_5m < 10000:
        log("5분봉 백필 시작 (90일)...")
        proc = subprocess.run(
            [sys.executable, str(DATA_DIR / "hades_5m_collector.py"),
             "--symbols"] + SYMBOLS + ["--days", "90", "--db", str(DB_PATH)],
            cwd=str(DATA_DIR),
            timeout=600,
        )
        if proc.returncode != 0:
            log("⚠️ 5분봉 백필 실패, 계속 진행")
    else:
        log(f"5분봉 데이터 충분 ({count_5m}개), 스킵")

    # 1h봉 백필 여부 확인
    conn = sqlite3.connect(str(DB_PATH))
    count_1h = conn.execute("SELECT COUNT(*) FROM market_1h_candles").fetchone()[0]
    conn.close()

    if count_1h < 5000:
        log("1h봉 백필 시작 (90일)...")
        proc = subprocess.run(
            [sys.executable, str(DATA_DIR / "data_collector.py"),
             "--symbols"] + SYMBOLS + ["--days", "90", "--db", str(DB_PATH)],
            cwd=str(DATA_DIR),
            timeout=600,
        )
        if proc.returncode != 0:
            log("⚠️ 1h봉 백필 실패, 계속 진행")
    else:
        log(f"1h봉 데이터 충분 ({count_1h}개), 스킵")


def train_if_needed():
    """모델이 없으면 자동 학습"""
    model_path = MODELS_DIR / "lightgbm_model.txt"
    if model_path.exists():
        log("ML 모델 이미 존재, 학습 스킵")
        return True

    log("ML 모델 없음, 학습 시작...")
    MODELS_DIR.mkdir(exist_ok=True)

    # 학습 데이터 준비
    proc = subprocess.run(
        [sys.executable, str(DATA_DIR / "prepare_training.py"),
         "--db", str(DB_PATH), "--symbol", "BTCUSDT"],
        cwd=str(DATA_DIR),
        timeout=300,
    )
    if proc.returncode != 0:
        log("⚠️ 학습 데이터 준비 실패")
        return False

    # 학습
    proc = subprocess.run(
        [sys.executable, str(DATA_DIR / "train_model.py"), "BTCUSDT"],
        cwd=str(DATA_DIR),
        timeout=300,
        env={**os.environ, "PYTHONIOENCODING": "utf-8"},
    )
    if proc.returncode != 0:
        log("⚠️ ML 학습 실패")
        return False

    log("✅ ML 모델 학습 완료")
    return True


def build_dual_engine():
    """Go DualEngine 바이너리 빌드"""
    cmd_dir = PROJECT_DIR / "cmd" / "dual_engine"
    bin_path = BASE_DIR / "dual_engine.exe"

    if bin_path.exists():
        log("DualEngine 바이너리 이미 존재, 빌드 스킵")
        return str(bin_path)

    log("DualEngine 빌드 중...")
    try:
        subprocess.run(
            ["go", "build", "-o", str(bin_path), "."],
            cwd=str(cmd_dir),
            timeout=120,
            check=True,
        )
        log(f"✅ DualEngine 빌드 완료: {bin_path}")
        return str(bin_path)
    except FileNotFoundError:
        log("⚠️ Go 컴파일러 없음 — DualEngine을 수동으로 빌드하세요")
        return None
    except subprocess.CalledProcessError as e:
        log(f"⚠️ DualEngine 빌드 실패: {e}")
        return None


def start_all(foreground=False):
    """전체 시스템 기동"""
    log("=== HADES 시스템 기동 ===")

    # 1. 백필
    run_backfill()

    # 2. 모델 학습
    train_if_needed()

    procs = {}

    # 3. ML 서비스
    log("ML 서비스 기동 (:5001)...")
    ml_log = open(LOG_DIR / "ml_service.log", "a")
    ml_env = {**os.environ, "MODEL_DIR": str(MODELS_DIR), "PYTHONIOENCODING": "utf-8"}
    ml_proc = subprocess.Popen(
        [sys.executable, str(ML_SERVICE_DIR / "server.py")],
        cwd=str(ML_SERVICE_DIR),
        stdout=ml_log,
        stderr=ml_log,
        env=ml_env,
    )
    procs["ml_service"] = ml_proc
    log(f"  ML 서비스 PID: {ml_proc.pid}")
    time.sleep(3)  # 서비스 시작 대기

    # 4. DualEngine
    bin_path = build_dual_engine()
    if bin_path:
        log("DualEngine 기동 (:8080)...")
        engine_log = open(LOG_DIR / "dual_engine.log", "a")
        engine_proc = subprocess.Popen(
            [bin_path],
            stdout=engine_log,
            stderr=engine_log,
        )
        procs["dual_engine"] = engine_proc
        log(f"  DualEngine PID: {engine_proc.pid}")
        time.sleep(2)

    # 5. HADES 루프
    log("HADES 루프 기동...")
    hades_log = open(LOG_DIR / "hades.log", "a")
    hades_proc = subprocess.Popen(
        [sys.executable, str(SCRIPTS_DIR / "run_hades.py"),
         "--symbols"] + SYMBOLS +
         ["--db", str(DB_PATH),
          "--ml-url", "http://localhost:5001",
          "--engine-url", "http://localhost:8080"],
        cwd=str(SCRIPTS_DIR),
        stdout=hades_log,
        stderr=hades_log,
    )
    procs["hades_loop"] = hades_proc
    log(f"  HADES 루프 PID: {hades_proc.pid}")

    save_pids(procs)

    log("\n=== HADES 시스템 기동 완료 ===")
    log(f"  ML 서비스:      http://localhost:5001/health")
    log(f"  DualEngine:     http://localhost:8080/api/v1/status")
    log(f"  로그 디렉토리:   {LOG_DIR}")
    log(f"  PID 파일:       {PID_FILE}")
    log("\n종료: python start_hades.py --stop")

    if foreground:
        log("\n포그라운드 모드 — Ctrl+C로 종료")
        try:
            for proc in procs.values():
                proc.wait()
        except KeyboardInterrupt:
            log("종료 중...")
            kill_pids()
    else:
        # 백그라운드 모드: 프로세스 모니터링
        try:
            while True:
                time.sleep(30)
                for name, proc in procs.items():
                    if proc.poll() is not None:
                        log(f"⚠️ {name} (PID {proc.pid}) 종료됨 (code: {proc.returncode})")
        except KeyboardInterrupt:
            kill_pids()


def main():
    parser = argparse.ArgumentParser(description="HADES 시스템 관리")
    parser.add_argument("--stop", action="store_true", help="전체 종료")
    parser.add_argument("--status", action="store_true", help="상태 확인")
    parser.add_argument("--foreground", action="store_true", help="포그라운드 실행")
    args = parser.parse_args()

    if args.stop:
        kill_pids()
    elif args.status:
        check_status()
    else:
        start_all(foreground=args.foreground)


if __name__ == "__main__":
    main()
