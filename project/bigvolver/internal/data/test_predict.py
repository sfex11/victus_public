"""Predict using the trained model — test with latest market data."""
import os
os.environ["PYTHONIOENCODING"] = "utf-8"

import json
import sqlite3
import numpy as np
import lightgbm as lgb
from pathlib import Path

MODEL_DIR = Path(r"C:\Users\Test\.openclaw\workspace-hex\project\bigvolver\project\bigvolver\models")
DB_PATH = Path(r"C:\Users\Test\.openclaw\workspace-hex\project\bigvolver\project\bigvolver\internal\data\bigvolver.db")

DEFAULT_FEATURES = [
    "ema_5", "ema_20", "ema_50", "ema_200",
    "rsi_14", "rsi_28",
    "macd_line", "macd_signal", "macd_histogram",
    "atr_14", "bollinger_upper", "bollinger_lower",
    "adx_14", "obv", "volume_ratio",
    "funding_rate", "funding_rate_change_1h", "funding_rate_change_8h",
    "volatility_1h", "volatility_4h", "volatility_24h",
    "momentum_1h", "momentum_4h",
    "mean_reversion_score",
    "regime_trending", "regime_ranging", "regime_volatile",
]

# Load metadata
with open(MODEL_DIR / "metadata.json") as f:
    meta = json.load(f)

symbol = meta.get("symbol", "BTCUSDT")
version = meta.get("version", "unknown")

print(f"Model: v{version} | Symbol: {symbol}")
print(f"Training: Sharpe={meta.get('sharpe_ratio')} | WinRate={meta.get('win_rate', 0)*100:.1f}%")

# Load model
model = lgb.Booster(model_file=str(MODEL_DIR / "lightgbm_model.txt"))

# Get latest candles from DB
conn = sqlite3.connect(str(DB_PATH))
rows = conn.execute("""
    SELECT timestamp, open, high, low, close, volume
    FROM market_1h_candles
    WHERE symbol = ?
    ORDER BY timestamp DESC
    LIMIT 210
""", (symbol,)).fetchall()
conn.close()

# Reverse to oldest first
rows = rows[::-1]
closes = [r[4] for r in rows]
highs = [r[2] for r in rows]
lows = [r[3] for r in rows]
volumes = [r[5] for r in rows]

# Compute features for the latest candle
from prepare_training import compute_features

funding_info = {"rate": 0, "change_1h": 0, "change_8h": 0}
features = compute_features(closes, highs, lows, volumes, len(rows) - 1, funding_info)

if features is None:
    print("ERROR: Cannot compute features")
    exit(1)

print(f"\n--- Latest Features ({symbol}) ---")
print(f"  Close: {closes[-1]:.2f}")
print(f"  RSI-14: {features['rsi_14']:.1f}")
print(f"  EMA-20: {features['ema_20']:.2f}")
print(f"  MACD Hist: {features['macd_histogram']:.2f}")
print(f"  ATR-14: {features['atr_14']:.2f}")
print(f"  Vol-24h: {features['volatility_24h']:.2f}%")
print(f"  Momentum-4h: {features['momentum_4h']:.2f}%")

# Predict
X = np.array([[features.get(f, 0.0) for f in DEFAULT_FEATURES]])
pred = float(model.predict(X)[0])

# Signal
atr = features.get("atr_14", 0)
close = features.get("ema_20", 50000)
threshold = (atr / close * 100) if close > 0 and atr > 0 else 0.3

if pred > threshold:
    signal = "LONG"
elif pred < -threshold:
    signal = "SHORT"
else:
    signal = "NEUTRAL"

confidence = min(abs(pred) / max(threshold * 2, 0.1), 1.0)

print(f"\n--- Prediction ---")
print(f"  Signal: {signal}")
print(f"  Predicted 4h Return: {pred:.4f}%")
print(f"  Confidence: {confidence:.2f}")
print(f"  Dynamic Threshold: {threshold:.4f}%")

# SHAP-like top features
importance = dict(zip(DEFAULT_FEATURES, model.feature_importance(importance_type="gain")))
top5 = sorted(importance.items(), key=lambda x: x[1], reverse=True)[:5]
print(f"\n--- Feature Importance ---")
for name, gain in top5:
    val = features.get(name, 0)
    print(f"  {name}: {val:.4f} (gain: {gain:.0f})")
