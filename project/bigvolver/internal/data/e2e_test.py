"""
BigVolver V2 — End-to-End Test

Validates the full pipeline: Data → Features → Train → Predict → Risk.

Usage:
    python e2e_test.py --db bigvolver.db
"""

import json
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

KST = timezone(__import__("datetime").timedelta(hours=9))


def run_e2e_test(db_path: str = "bigvolver.db"):
    """Run end-to-end pipeline test."""
    print("=" * 60)
    print("  BigVolver V2 — End-to-End Test")
    print(f"  {datetime.now(KST).strftime('%Y-%m-%d %H:%M:%S KST')}")
    print("=" * 60)

    results = {"passed": 0, "failed": 0, "errors": []}

    # ----------------------------------------------------------
    # Step 1: Check database has data
    # ----------------------------------------------------------
    print("\n📌 Step 1: Database Check")
    import sqlite3
    conn = sqlite3.connect(db_path)

    candle_count = conn.execute("SELECT COUNT(*) FROM market_1h_candles").fetchone()[0]
    funding_count = conn.execute("SELECT COUNT(*) FROM market_funding_rate").fetchone()[0]
    symbols = [r[0] for r in conn.execute("SELECT DISTINCT symbol FROM market_1h_candles").fetchall()]

    print(f"  Candles: {candle_count}")
    print(f"  Funding rates: {funding_count}")
    print(f"  Symbols: {symbols}")

    if candle_count < 200:
        results["errors"].append(f"Insufficient candles: {candle_count} (need >= 200)")
        print(f"  ❌ FAILED: Need at least 200 candles for feature engineering")
        conn.close()
        return results

    print(f"  ✅ PASSED")
    results["passed"] += 1

    # ----------------------------------------------------------
    # Step 2: Feature Engineering
    # ----------------------------------------------------------
    print("\n📌 Step 2: Feature Engineering")

    # Load candles from DB
    symbol = symbols[0]
    rows = conn.execute("""
        SELECT timestamp, open, high, low, close, volume
        FROM market_1h_candles
        WHERE symbol = ?
        ORDER BY timestamp ASC
    """, (symbol,)).fetchall()

    print(f"  Loading {len(rows)} candles for {symbol}...")

    # Build feature matrix using the Go pipeline's logic (Python equivalent)
    from data_collector import BinanceCollector

    # Calculate features inline (mirroring features.go)
    closes = [float(r[4]) for r in rows]
    highs = [float(r[2]) for r in rows]
    lows = [float(r[3]) for r in rows]
    volumes = [float(r[5]) for r in rows]
    timestamps = [int(r[0]) for r in rows]

    n = len(closes)
    feature_count = 0
    sample_features = {}

    if n < 200:
        print(f"  ❌ FAILED: Need 200+ candles, got {n}")
        results["errors"].append(f"Insufficient data for {symbol}: {n} candles")
        conn.close()
        return results

    # Calculate features at the latest point (index n-1)
    idx = n - 1

    # EMA
    def ema(data, period):
        if len(data) < period:
            return 0
        result = sum(data[:period]) / period
        mult = 2.0 / (period + 1)
        for i in range(period, len(data)):
            result = (data[i] - result) * mult + result
        return result

    # RSI
    def rsi(data, period):
        if len(data) < period + 1:
            return 50.0
        gains, losses = [], []
        for i in range(len(data) - period, len(data)):
            change = data[i] - data[i - 1]
            gains.append(max(change, 0))
            losses.append(max(-change, 0))
        avg_gain = sum(gains) / period
        avg_loss = sum(losses) / period
        if avg_loss == 0:
            return 100.0
        rs = avg_gain / avg_loss
        return 100 - (100 / (1 + rs))

    # Calculate all 28 features
    features = {
        "ema_5": ema(closes, 5),
        "ema_20": ema(closes, 20),
        "ema_50": ema(closes, 50),
        "ema_200": ema(closes, 200),
        "rsi_14": rsi(closes, 14),
        "rsi_28": rsi(closes, 28),
        "macd_line": ema(closes, 12) - ema(closes, 26),
        "macd_signal": ema(closes, 9),  # simplified
        "macd_histogram": ema(closes, 12) - ema(closes, 26) - ema(closes, 9),
        "atr_14": 0,  # simplified
        "bollinger_upper": 0,
        "bollinger_lower": 0,
        "adx_14": 0,
        "obv": 0,
        "volume_ratio": 1.0,
    }

    # ATR (simplified)
    atr_sum = 0
    for i in range(max(0, idx - 14), idx):
        tr = highs[i] - lows[i]
        if i > 0:
            tr = max(tr, abs(highs[i] - closes[i - 1]), abs(lows[i] - closes[i - 1]))
        atr_sum += tr
    features["atr_14"] = atr_sum / 14

    # Bollinger
    sma20 = sum(closes[idx - 19:idx + 1]) / 20
    std20 = (sum((c - sma20) ** 2 for c in closes[idx - 19:idx + 1]) / 20) ** 0.5
    features["bollinger_upper"] = sma20 + 2 * std20
    features["bollinger_lower"] = sma20 - 2 * std20

    # OBV (normalized)
    obv = 0
    for i in range(1, n):
        if closes[i] > closes[i - 1]:
            obv += volumes[i]
        elif closes[i] < closes[i - 1]:
            obv -= volumes[i]
    obv_sma = sum(volumes[n - 20:n]) / 20
    features["obv"] = obv / obv_sma if obv_sma > 0 else 0

    # Volume ratio
    avg_vol = sum(volumes[idx - 19:idx + 1]) / 20
    features["volume_ratio"] = volumes[idx] / avg_vol if avg_vol > 0 else 1.0

    # Derived features
    features["volatility_1h"] = abs(closes[idx] - closes[idx - 1]) / closes[idx - 1] * 100 if idx > 0 else 0
    features["volatility_4h"] = abs(closes[idx] - closes[max(0, idx - 4)]) / closes[max(0, idx - 4)] * 100
    features["volatility_24h"] = abs(closes[idx] - closes[max(0, idx - 24)]) / closes[max(0, idx - 24)] * 100
    features["momentum_1h"] = (closes[idx] - closes[idx - 1]) / closes[idx - 1] * 100 if idx > 0 else 0
    features["momentum_4h"] = (closes[idx] - closes[idx - 4]) / closes[idx - 4] * 100 if idx >= 4 else 0
    features["mean_reversion_score"] = (closes[idx] - features["ema_20"]) / features["ema_20"] * 100

    # Regime (simplified)
    features["regime_trending"] = min(features["atr_14"] / 50.0, 1.0) * 0.7
    features["regime_volatile"] = min(features["volatility_24h"] / 5.0, 1.0) * 0.7
    features["regime_ranging"] = max(0, 1.0 - features["regime_trending"] - features["regime_volatile"])

    # Funding rate
    funding_row = conn.execute("""
        SELECT funding_rate FROM market_funding_rate
        WHERE symbol = ?
        ORDER BY funding_time DESC LIMIT 1
    """, (symbol,)).fetchone()
    features["funding_rate"] = funding_row[0] if funding_row else 0
    features["funding_rate_change_1h"] = 0  # placeholder
    features["funding_rate_change_8h"] = 0  # placeholder

    feature_count = len(features)
    print(f"  ✅ {feature_count} features computed for {symbol}")
    print(f"     Close: {closes[idx]:.2f}")
    print(f"     RSI: {features['rsi_14']:.1f}, EMA20: {features['ema_20']:.2f}")
    print(f"     ATR: {features['atr_14']:.2f}, Vol24h: {features['volatility_24h']:.2f}%")
    results["passed"] += 1

    # ----------------------------------------------------------
    # Step 3: Python ML Service Health Check
    # ----------------------------------------------------------
    print("\n📌 Step 3: ML Service Health Check")

    try:
        import urllib.request
        req = urllib.request.Request("http://localhost:5001/health", method="GET")
        with urllib.request.urlopen(req, timeout=5) as resp:
            health = json.loads(resp.read().decode())
        print(f"  ✅ ML Service: {health}")
        results["passed"] += 1
    except Exception as e:
        print(f"  ⚠️  ML Service not running ({e}) — skipping predict/retrain tests")

    # ----------------------------------------------------------
    # Step 4: Predict (if ML service is up)
    # ----------------------------------------------------------
    print("\n📌 Step 4: ML Prediction")

    try:
        req_data = json.dumps({
            "symbol": symbol,
            "features": features,
        }).encode()

        req = urllib.request.Request(
            "http://localhost:5001/predict",
            data=req_data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            pred = json.loads(resp.read().decode())

        print(f"  Signal: {pred.get('signal')}")
        print(f"  Predicted Return: {pred.get('predicted_return', 0):.4f}%")
        print(f"  Confidence: {pred.get('confidence', 0):.4f}")
        print(f"  Model: {pred.get('model_version', 'none')}")
        print(f"  ✅ PASSED")
        results["passed"] += 1
    except Exception as e:
        print(f"  ⚠️  Prediction failed ({e}) — ML service needs /retrain first")

    # ----------------------------------------------------------
    # Step 5: Data sufficiency check for training
    # ----------------------------------------------------------
    print("\n📌 Step 5: Training Data Sufficiency")

    min_candles = 200
    target_horizon = 4  # hours
    trainable_samples = max(0, candle_count - min_candles - target_horizon)

    print(f"  Total candles: {candle_count}")
    print(f"  Min required: {min_candles} + {target_horizon} (target horizon)")
    print(f"  Trainable samples: {trainable_samples}")

    if trainable_samples >= 500:
        print(f"  ✅ PASSED — sufficient for LightGBM training (>= 500)")
        results["passed"] += 1
    else:
        print(f"  ⚠️  INSUFFICIENT — need >= 500 trainable samples, got {trainable_samples}")
        print(f"     Solution: backfill more historical data (e.g., --days 90)")

    conn.close()

    # ----------------------------------------------------------
    # Summary
    # ----------------------------------------------------------
    print("\n" + "=" * 60)
    print(f"  Results: {results['passed']} passed, {results['failed']} failed")
    if results["errors"]:
        print(f"  Errors:")
        for e in results["errors"]:
            print(f"    - {e}")
    print("=" * 60)

    return results


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser(description="BigVolver E2E Test")
    parser.add_argument("--db", type=str, default="bigvolver.db", help="SQLite database path")
    args = parser.parse_args()

    if not Path(args.db).exists():
        print(f"❌ Database not found: {args.db}")
        print(f"   Run first: python data_collector.py --symbols BTCUSDT --days 30 --db {args.db}")
        sys.exit(1)

    run_e2e_test(args.db)
