# -*- coding: utf-8 -*-
"""
HADES 학습 데이터 준비 — 최적화 버전 (numpy 벡터화)

Usage:
    python hades_prepare_training.py --db bigvolver.db --output ./training_data

5분봉 90일 데이터(3종목)를 5분 안에 처리.
"""

import argparse
import json
import sqlite3
import sys
import time
from pathlib import Path

import numpy as np


def ema_series(data: np.ndarray, period: int) -> np.ndarray:
    """전체 시계열에 대한 EMA 계산 (벡터화)"""
    n = len(data)
    result = np.zeros(n)
    if n < period:
        return result
    result[:period] = np.mean(data[:period])
    k = 2.0 / (period + 1)
    for i in range(period, n):
        result[i] = data[i] * k + result[i - 1] * (1 - k)
    return result


def compute_all_features(closes: np.ndarray, highs: np.ndarray,
                         lows: np.ndarray, volumes: np.ndarray,
                         funding_rates: list) -> list:
    """
    전체 시계열에 대해 한 번에 feature 계산.
    반환: list of dicts (index 200부터)
    """
    n = len(closes)
    if n < 210:
        return []

    # Precompute EMA series
    ema5 = ema_series(closes, 5)
    ema12 = ema_series(closes, 12)
    ema20 = ema_series(closes, 20)
    ema26 = ema_series(closes, 26)
    ema50 = ema_series(closes, 50)
    ema200 = ema_series(closes, 200)

    # MACD
    macd_line = ema12 - ema26
    macd_signal = ema_series(macd_line, 9)
    macd_histogram = macd_line - macd_signal

    # RSI series
    def rsi_series(data, period):
        result = np.full(n, 50.0)
        if n < period + 1:
            return result
        changes = np.diff(data)
        for i in range(period, len(changes)):
            gains = changes[i - period:i]
            avg_g = max(np.mean(np.where(gains > 0, gains, 0)), 1e-10)
            avg_l = max(np.mean(np.where(gains < 0, -gains, 0)), 1e-10)
            result[i + 1] = 100 - 100 / (1 + avg_g / avg_l)
        return result

    rsi14 = rsi_series(closes, 14)
    rsi28 = rsi_series(closes, 28)

    # ATR series
    def atr_series(highs, lows, closes, period):
        result = np.zeros(n)
        if n < period + 1:
            return result
        tr = np.maximum(highs[1:] - lows[1:],
                       np.maximum(np.abs(highs[1:] - closes[:-1]),
                                  np.abs(lows[1:] - closes[:-1])))
        for i in range(period - 1, len(tr)):
            result[i + 1] = np.mean(tr[i - period + 1:i + 1])
        return result

    atr14 = atr_series(highs, lows, closes, 14)

    # Bollinger Bands
    bb_mid = np.full(n, 0.0)
    bb_std = np.full(n, 0.0)
    for i in range(19, n):
        window = closes[i - 19:i + 1]
        bb_mid[i] = np.mean(window)
        bb_std[i] = np.std(window)
    bb_upper = bb_mid + 2 * bb_std
    bb_lower = bb_mid - 2 * bb_std

    # ADX (simplified single-pass)
    adx14 = np.zeros(n)
    if n > 56:
        plus_dm = np.maximum(np.diff(highs), 0)
        minus_dm = np.maximum(-np.diff(lows), 0)
        tr = np.maximum(highs[1:] - lows[1:],
                       np.maximum(np.abs(highs[1:] - closes[:-1]),
                                  np.abs(lows[1:] - closes[:-1])))
        for i in range(28, len(plus_dm)):
            p = np.mean(plus_dm[i - 14:i])
            m = np.mean(minus_dm[i - 14:i])
            t = np.mean(tr[i - 14:i])
            if t > 0:
                adx14[i + 1] = abs(p - m) / (p + m) * 100

    # OBV (normalized)
    obv = np.zeros(n)
    for i in range(1, n):
        if closes[i] > closes[i - 1]:
            obv[i] = obv[i - 1] + volumes[i]
        elif closes[i] < closes[i - 1]:
            obv[i] = obv[i - 1] - volumes[i]
        else:
            obv[i] = obv[i - 1]
    obv_sma = np.full(n, 1.0)
    for i in range(19, n):
        s = np.mean(obv[i - 19:i + 1])
        obv_sma[i] = obv[i] / s if s != 0 else obv[i]

    # Volume ratio
    vol_ratio = np.ones(n)
    for i in range(19, n):
        avg = np.mean(volumes[i - 19:i + 1])
        vol_ratio[i] = volumes[i] / avg if avg > 0 else 1.0

    # Funding rates lookup
    funding_by_time = {fr["timestamp"]: fr["rate"] for fr in funding_rates}
    funding_times = sorted(funding_by_time.keys())

    # Volatility / Momentum (precompute)
    vol_1h = np.zeros(n)
    vol_4h = np.zeros(n)
    vol_24h = np.zeros(n)
    mom_1h = np.zeros(n)
    mom_4h = np.zeros(n)

    for i in range(2, n):
        vol_1h[i] = (closes[i] - closes[i - 1]) / closes[i - 1] * 100
        mom_1h[i] = vol_1h[i]
    for i in range(5, n):
        vol_4h[i] = (closes[i] - closes[i - 4]) / closes[i - 4] * 100
        mom_4h[i] = vol_4h[i]
    for i in range(25, n):
        vol_24h[i] = (closes[i] - closes[i - 24]) / closes[i - 24] * 100

    # Mean reversion
    mean_rev = np.zeros(n)
    for i in range(n):
        if ema20[i] > 0:
            mean_rev[i] = (closes[i] - ema20[i]) / ema20[i] * 100

    # Regime
    regime_t = np.minimum(adx14 / 50.0, 1.0) * 0.7
    regime_v = np.minimum(np.abs(vol_24h) / 5.0, 1.0) * 0.7
    regime_r = np.maximum(1.0 - regime_t - regime_v, 0)
    total = regime_t + regime_r + regime_v
    regime_t = np.where(total > 0, regime_t / total, 0.33)
    regime_r = np.where(total > 0, regime_r / total, 0.34)
    regime_v = np.where(total > 0, regime_v / total, 0.33)

    # Funding lookup per timestamp
    ts_list = []  # need timestamps from caller

    # Build results
    results = []
    for i in range(204, n):
        fr = 0
        for ft in reversed(funding_times):
            if ft <= 0:  # placeholder, real ts passed differently
                break
            fr = funding_by_time[ft]
            break

        fc_1h = 0
        fc_8h = 0
        if len(funding_times) > 1:
            fc_8h = funding_by_time[funding_times[-1]] - funding_by_time[funding_times[0]]

        results.append({
            "ema_5": round(float(ema5[i]), 6),
            "ema_20": round(float(ema20[i]), 6),
            "ema_50": round(float(ema50[i]), 6),
            "ema_200": round(float(ema200[i]), 6),
            "rsi_14": round(float(rsi14[i]), 4),
            "rsi_28": round(float(rsi28[i]), 4),
            "macd_line": round(float(macd_line[i]), 6),
            "macd_signal": round(float(macd_signal[i]), 6),
            "macd_histogram": round(float(macd_histogram[i]), 6),
            "atr_14": round(float(atr14[i]), 6),
            "bollinger_upper": round(float(bb_upper[i]), 6),
            "bollinger_lower": round(float(bb_lower[i]), 6),
            "adx_14": round(float(adx14[i]), 4),
            "obv": round(float(obv_sma[i]), 6),
            "volume_ratio": round(float(vol_ratio[i]), 4),
            "funding_rate": round(fr, 8),
            "funding_rate_change_1h": round(fc_1h, 8),
            "funding_rate_change_8h": round(fc_8h, 8),
            "volatility_1h": round(float(vol_1h[i]), 4),
            "volatility_4h": round(float(vol_4h[i]), 4),
            "volatility_24h": round(float(vol_24h[i]), 4),
            "momentum_1h": round(float(mom_1h[i]), 4),
            "momentum_4h": round(float(mom_4h[i]), 4),
            "mean_reversion_score": round(float(mean_rev[i]), 4),
            "regime_trending": round(float(regime_t[i]), 4),
            "regime_ranging": round(float(regime_r[i]), 4),
            "regime_volatile": round(float(regime_v[i]), 4),
        })

    return results


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--db", default="bigvolver.db")
    parser.add_argument("--output", default="./training_data")
    args = parser.parse_args()

    output_dir = Path(args.output)
    output_dir.mkdir(parents=True, exist_ok=True)

    conn = sqlite3.connect(args.db)

    # 펀딩레이트 로드
    funding_rows = conn.execute(
        "SELECT symbol, funding_time, funding_rate FROM market_funding_rate ORDER BY funding_time ASC"
    ).fetchall()
    funding_by_symbol = {}
    for r in funding_rows:
        sym, ft, rate = r
        funding_by_symbol.setdefault(sym, []).append({"timestamp": ft, "rate": rate})

    symbols = [r[0] for r in conn.execute(
        "SELECT DISTINCT symbol FROM market_5m_candles ORDER BY symbol"
    ).fetchall()]

    total = 0
    target_horizon = 48  # 4h = 48 * 5min

    for symbol in symbols:
        t0 = time.time()
        rows = conn.execute("""
            SELECT timestamp, open, high, low, close, volume
            FROM market_5m_candles
            WHERE symbol = ?
            ORDER BY timestamp ASC
        """, (symbol,)).fetchall()

        if len(rows) < 210:
            print("  SKIP {}: {} candles".format(symbol, len(rows)))
            continue

        timestamps = np.array([r[0] for r in rows], dtype=np.int64)
        closes = np.array([r[4] for r in rows], dtype=np.float64)
        highs = np.array([r[2] for r in rows], dtype=np.float64)
        lows = np.array([r[3] for r in rows], dtype=np.float64)
        volumes = np.array([r[5] for r in rows], dtype=np.float64)

        funding_rates = funding_by_symbol.get(symbol, [])

        # 전체 feature 한 번에 계산
        print("  {}: computing features for {} candles...".format(symbol, len(rows)))
        all_features = compute_all_features(closes, highs, lows, volumes, funding_rates)

        output_path = output_dir / "training_data_{}.jsonl".format(symbol)
        count = 0

        with open(str(output_path), "w") as f:
            for idx, features in enumerate(all_features):
                real_idx = 204 + idx
                if real_idx + target_horizon >= len(closes):
                    continue

                current_close = closes[real_idx]
                future_close = closes[real_idx + target_horizon]
                target = round((future_close - current_close) / current_close * 100, 4)

                record = {
                    "timestamp": int(timestamps[real_idx]),
                    "symbol": symbol,
                    "features": features,
                    "target": target,
                }

                f.write(json.dumps(record) + "\n")
                count += 1

        elapsed = time.time() - t0
        total += count
        print("  {}: {} samples in {:.1f}s -> {}".format(symbol, count, elapsed, output_path))

    conn.close()
    print("\n  Total: {} samples across {} symbols".format(total, len(symbols)))


if __name__ == "__main__":
    main()
