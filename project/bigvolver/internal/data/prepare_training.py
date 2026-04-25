"""
Convert SQLite candles to JSONL training data for LightGBM.

Reads market_1h_candles + market_funding_rate from DB,
computes features (mirroring Go features.go), and outputs JSONL.
"""
import os
os.environ["PYTHONIOENCODING"] = "utf-8"

import json
import math
import sqlite3
import sys
from datetime import datetime, timezone
from pathlib import Path


def ema(data, period):
    if len(data) < period:
        return sum(data[-period:]) / period if len(data) >= period else data[-1] if data else 0
    k = 2.0 / (period + 1)
    result = sum(data[:period]) / period
    for i in range(period, len(data)):
        result = data[i] * k + result * (1 - k)
    return result


def rsi(data, period=14):
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


def compute_features(closes, highs, lows, volumes, idx, funding_rates_at_idx):
    """Compute 27 features at index idx."""
    n = len(closes)
    if idx < 200:
        return None

    # Technical
    ema_5 = ema(closes[:idx+1], 5)
    ema_20 = ema(closes[:idx+1], 20)
    ema_50 = ema(closes[:idx+1], 50)
    ema_200 = ema(closes[:idx+1], 200)
    rsi_14 = rsi(closes[:idx+1], 14)
    rsi_28 = rsi(closes[:idx+1], 28)

    macd_line = ema(closes[:idx+1], 12) - ema(closes[:idx+1], 26)
    macd_signal = ema(closes[max(0,idx-50):idx+1], 9)
    macd_histogram = macd_line - macd_signal

    # ATR
    atr_vals = []
    for i in range(max(1, idx-14), idx+1):
        tr = highs[i] - lows[i]
        tr = max(tr, abs(highs[i] - closes[i-1]), abs(lows[i] - closes[i-1]))
        atr_vals.append(tr)
    atr_14 = sum(atr_vals) / len(atr_vals) if atr_vals else 0

    # Bollinger
    window = closes[idx-19:idx+1]
    sma20 = sum(window) / 20
    std20 = math.sqrt(sum((c - sma20)**2 for c in window) / 20)
    bb_upper = sma20 + 2 * std20
    bb_lower = sma20 - 2 * std20

    # ADX (simplified)
    plus_dm, minus_dm, trs = [], [], []
    for i in range(max(1, idx-28), idx+1):
        up = highs[i] - highs[i-1]
        down = lows[i-1] - lows[i]
        plus_dm.append(max(up, 0) if up > down and up > 0 else 0)
        minus_dm.append(max(down, 0) if down > up and down > 0 else 0)
        tr = max(highs[i]-lows[i], abs(highs[i]-closes[i-1]), abs(lows[i]-closes[i-1]))
        trs.append(tr)
    if trs:
        avg_plus = sum(plus_dm[-14:]) / 14
        avg_minus = sum(minus_dm[-14:]) / 14
        avg_tr = sum(trs[-14:]) / 14
        if avg_tr > 0:
            dx = abs(avg_plus - avg_minus) / (avg_plus + avg_minus) * 100
            adx_14 = dx  # simplified single-period
        else:
            adx_14 = 0
    else:
        adx_14 = 0

    # OBV (normalized)
    obv = 0
    for i in range(1, n):
        if closes[i] > closes[i-1]:
            obv += volumes[i]
        elif closes[i] < closes[i-1]:
            obv -= volumes[i]
    obv_sma = sum(volumes[max(0,idx-19):idx+1]) / 20
    obv_norm = obv / obv_sma if obv_sma > 0 else 0

    # Volume ratio
    avg_vol = sum(volumes[max(0,idx-19):idx+1]) / 20
    vol_ratio = volumes[idx] / avg_vol if avg_vol > 0 else 1.0

    # Microstructure
    funding_rate = funding_rates_at_idx.get("rate", 0)
    funding_1h = funding_rates_at_idx.get("change_1h", 0)
    funding_8h = funding_rates_at_idx.get("change_8h", 0)

    # Derived
    def pct_change(a, b):
        return (a - b) / b * 100 if b != 0 else 0

    vol_1h = pct_change(closes[idx], closes[idx-1])
    vol_4h = pct_change(closes[idx], closes[max(0,idx-4)])
    vol_24h = pct_change(closes[idx], closes[max(0,idx-24)])
    mom_1h = vol_1h
    mom_4h = pct_change(closes[idx], closes[max(0,idx-4)])
    mean_rev = (closes[idx] - ema_20) / ema_20 * 100 if ema_20 != 0 else 0

    # Regime
    regime_trending = min(atr_14 / 50.0, 1.0) * 0.7
    regime_volatile = min(vol_24h / 5.0, 1.0) * 0.7
    regime_ranging = max(0, 1.0 - regime_trending - regime_volatile)

    return {
        "ema_5": round(ema_5, 6),
        "ema_20": round(ema_20, 6),
        "ema_50": round(ema_50, 6),
        "ema_200": round(ema_200, 6),
        "rsi_14": round(rsi_14, 4),
        "rsi_28": round(rsi_28, 4),
        "macd_line": round(macd_line, 6),
        "macd_signal": round(macd_signal, 6),
        "macd_histogram": round(macd_histogram, 6),
        "atr_14": round(atr_14, 6),
        "bollinger_upper": round(bb_upper, 6),
        "bollinger_lower": round(bb_lower, 6),
        "adx_14": round(adx_14, 4),
        "obv": round(obv_norm, 6),
        "volume_ratio": round(vol_ratio, 4),
        "funding_rate": round(funding_rate, 8),
        "funding_rate_change_1h": round(funding_1h, 8),
        "funding_rate_change_8h": round(funding_8h, 8),
        "volatility_1h": round(vol_1h, 4),
        "volatility_4h": round(vol_4h, 4),
        "volatility_24h": round(vol_24h, 4),
        "momentum_1h": round(mom_1h, 4),
        "momentum_4h": round(mom_4h, 4),
        "mean_reversion_score": round(mean_rev, 4),
        "regime_trending": round(regime_trending, 4),
        "regime_ranging": round(regime_ranging, 4),
        "regime_volatile": round(regime_volatile, 4),
    }


def main():
    db_path = sys.argv[1] if len(sys.argv) > 1 else "bigvolver.db"
    output_dir = Path(sys.argv[2]) if len(sys.argv) > 2 else Path("./training_data")
    output_dir.mkdir(parents=True, exist_ok=True)

    conn = sqlite3.connect(db_path)

    symbols = [r[0] for r in conn.execute(
        "SELECT DISTINCT symbol FROM market_1h_candles ORDER BY symbol"
    ).fetchall()]

    total_samples = 0

    for symbol in symbols:
        rows = conn.execute("""
            SELECT timestamp, open, high, low, close, volume
            FROM market_1h_candles
            WHERE symbol = ?
            ORDER BY timestamp ASC
        """, (symbol,)).fetchall()

        if len(rows) < 210:
            print(f"  SKIP {symbol}: only {len(rows)} candles (need >= 210)")
            continue

        closes = [r[4] for r in rows]
        highs = [r[2] for r in rows]
        lows = [r[3] for r in rows]
        volumes = [r[5] for r in rows]
        timestamps = [r[0] for r in rows]

        # Load funding rates
        funding_rows = conn.execute("""
            SELECT funding_time, funding_rate
            FROM market_funding_rate
            WHERE symbol = ?
            ORDER BY funding_time ASC
        """, (symbol,)).fetchall()

        funding_by_time = {r[0]: r[1] for r in funding_rows}
        funding_times = sorted(funding_by_time.keys())

        output_path = output_dir / f"training_data_{symbol}.jsonl"
        count = 0

        with open(output_path, "w") as f:
            for idx in range(204, len(rows) - 4):
                # Find nearest funding rate
                ts = timestamps[idx]
                nearest_funding = 0
                for ft in reversed(funding_times):
                    if ft <= ts:
                        nearest_funding = funding_by_time[ft]
                        break

                # Funding rate changes
                change_1h = 0
                change_8h = 0
                for ft in reversed(funding_times):
                    if ft <= ts:
                        prev_idx = funding_times.index(ft) - 1
                        if prev_idx >= 0:
                            change_8h = funding_by_time[ft] - funding_by_time[funding_times[prev_idx]]
                        break

                funding_info = {
                    "rate": nearest_funding,
                    "change_1h": change_1h,
                    "change_8h": change_8h,
                }

                features = compute_features(closes, highs, lows, volumes, idx, funding_info)
                if features is None:
                    continue

                # Target: 4h forward return
                future_close = closes[idx + 4] if idx + 4 < len(closes) else closes[-1]
                target = round((future_close - closes[idx]) / closes[idx] * 100, 4)

                record = {
                    "timestamp": timestamps[idx],
                    "symbol": symbol,
                    "features": features,
                    "target": target,
                }

                f.write(json.dumps(record) + "\n")
                count += 1

        total_samples += count
        print(f"  {symbol}: {count} samples -> {output_path}")

    conn.close()
    print(f"\n  Total: {total_samples} training samples across {len(symbols)} symbols")


if __name__ == "__main__":
    main()
