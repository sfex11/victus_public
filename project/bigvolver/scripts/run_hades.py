"""
HADES 메인 실행 루프 — 5분봉 수집 → Feature 계산 → ML 예측 → DualEngine 시그널 전송

Usage:
    # 기본 실행 (ML 서비스 :5001, DualEngine :8080)
    python run_hades.py --symbols BTCUSDT ETHUSDT SOLUSDT --db bigvolver.db

    # 커스텀 포트
    python run_hades.py --ml-port 5001 --engine-port 8080 --interval 300

    # 1회 실행 (테스트용)
    python run_hades.py --once --db bigvolver.db

Dependencies:
    pip install numpy pandas

Flow:
    1. DB에서 최근 5분봉 250개 조회
    2. 27개 feature 계산 (hades_features.py)
    3. ML 서비스에 예측 요청 (POST /predict)
    4. 시그널이 LONG/SHORT이면 DualEngine에 전송 (POST /api/v1/bigv/signals)
    5. 5분마다 반복
"""

import argparse
import json
import sqlite3
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import urllib.request
import urllib.error

KST = timezone(__import__("datetime").timedelta(hours=9))
DEFAULT_SYMBOLS = ["BTCUSDT", "ETHUSDT", "SOLUSDT"]


def fetch_latest_candles(db_path: str, symbol: str, limit: int = 250) -> list:
    """DB에서 최근 5분봉 조회 (오래된 순)"""
    conn = sqlite3.connect(db_path)
    rows = conn.execute("""
        SELECT timestamp, open, high, low, close, volume
        FROM market_5m_candles
        WHERE symbol = ?
        ORDER BY timestamp DESC
        LIMIT ?
    """, (symbol, limit)).fetchall()
    conn.close()

    rows = list(reversed(rows))
    return [{"timestamp": r[0], "open": r[1], "high": r[2], "low": r[3], "close": r[4], "volume": r[5]} for r in rows]


def get_current_price(db_path: str, symbol: str) -> float:
    conn = sqlite3.connect(db_path)
    row = conn.execute("SELECT close FROM market_5m_candles WHERE symbol = ? ORDER BY timestamp DESC LIMIT 1", (symbol,)).fetchone()
    conn.close()
    return row[0] if row else 0.0


def request_ml_predict(ml_url: str, symbol: str, features: dict) -> dict:
    """ML 서비스에 예측 요청"""
    payload = json.dumps({"symbol": symbol, "features": features}).encode()
    req = urllib.request.Request(
        f"{ml_url}/predict",
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.URLError as e:
        print(f"  [WARN] ML 서비스 연결 실패: {e}")
        return None
    except Exception as e:
        print(f"  [WARN] ML 예측 오류: {e}")
        return None


def send_signal_to_engine(engine_url: str, symbol: str, signal: str, confidence: float, price: float):
    """DualEngine에 시그널 전송"""
    payload = json.dumps({
        "pair": symbol,
        "direction": signal,
        "confidence": confidence,
        "price": price,
        "source": "HADES_ML",
    }).encode()
    req = urllib.request.Request(
        f"{engine_url}/api/v1/bigv/signals",
        data=payload,
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.URLError as e:
        print(f"  [WARN] DualEngine 연결 실패: {e}")
        return None
    except Exception as e:
        print(f"  [WARN] 시그널 전송 오류: {e}")
        return None


def run_cycle(symbols: list, db_path: str, ml_url: str, engine_url: str):
    """HADES 1사이클 실행"""
    # Feature 모듈 임포트
    sys.path.insert(0, str(Path(__file__).parent.parent / "internal" / "data"))
    from hades_features import compute_features

    now = datetime.now(KST).strftime("%H:%M:%S")
    print(f"\n{'='*50}")
    print(f"[HADES] {now} — 사이클 시작")

    for symbol in symbols:
        # 1. 데이터 조회
        candles = fetch_latest_candles(db_path, symbol, 250)
        if len(candles) < 200:
            print(f"  {symbol}: 데이터 부족 ({len(candles)}/200), 스킵")
            continue

        # 2. Feature 계산
        try:
            features = compute_features(candles)
        except ValueError as e:
            print(f"  {symbol}: Feature 계산 실패 — {e}")
            continue

        # 3. ML 예측
        prediction = request_ml_predict(ml_url, symbol, features)
        if prediction is None:
            continue

        signal = prediction.get("signal", "NEUTRAL")
        confidence = prediction.get("confidence", 0.0)
        predicted_return = prediction.get("predicted_return", 0.0)
        model_ver = prediction.get("model_version", "unknown")

        price = get_current_price(db_path, symbol)

        print(f"  {symbol}: signal={signal} conf={confidence:.3f} ret={predicted_return:.4f}% model={model_ver}")

        # 4. 시그널 전송 (NEUTRAL은 전송하지 않음)
        if signal in ("LONG", "SHORT"):
            result = send_signal_to_engine(engine_url, symbol, signal, confidence, price)
            if result:
                print(f"  {symbol}: ✅ {signal} 시그널 전송 완료")
            else:
                print(f"  {symbol}: ⚠️ 시그널 전송 실패")
        elif signal == "NEUTRAL":
            # 기존 포지션이 있으면 청산 시그널
            send_signal_to_engine(engine_url, symbol, "CLOSE", 0.0, price)

    print(f"[HADES] {now} — 사이클 완료")


def main():
    parser = argparse.ArgumentParser(description="HADES 메인 실행 루프")
    parser.add_argument("--symbols", nargs="+", default=DEFAULT_SYMBOLS)
    parser.add_argument("--db", type=str, default="bigvolver.db")
    parser.add_argument("--ml-url", type=str, default="http://localhost:5001")
    parser.add_argument("--engine-url", type=str, default="http://localhost:8081")
    parser.add_argument("--interval", type=int, default=300, help="실행 간격 (초)")
    parser.add_argument("--once", action="store_true", help="1회 실행 후 종료")
    args = parser.parse_args()

    print(f"[HADES] 시작 — symbols={args.symbols}")
    print(f"[HADES] ML 서비스: {args.ml_url}")
    print(f"[HADES] DualEngine: {args.engine_url}")
    print(f"[HADES] 실행 간격: {args.interval}s")
    print(f"[HADES] DB: {args.db}")

    if args.once:
        run_cycle(args.symbols, args.db, args.ml_url, args.engine_url)
        return

    # ML 서비스 헬스체크
    try:
        with urllib.request.urlopen(f"{args.ml_url}/health", timeout=5) as resp:
            health = json.loads(resp.read().decode())
            print(f"[HADES] ML 서비스 상태: {health.get('status')} (model: {health.get('model_version')})")
    except Exception as e:
        print(f"[HADES] [WARN] ML 서비스 연결 불가: {e}")
        print(f"[HADES] ML 서비스를 먼저 시작하세요: python ml_service/server.py")
        print(f"[HADES] 30초 후 재시도...")
        time.sleep(30)

    while True:
        try:
            run_cycle(args.symbols, args.db, args.ml_url, args.engine_url)
            time.sleep(args.interval)
        except KeyboardInterrupt:
            print("\n[HADES] 중지됨.")
            break
        except Exception as e:
            print(f"[HADES] 오류: {e}")
            time.sleep(60)


if __name__ == "__main__":
    main()
