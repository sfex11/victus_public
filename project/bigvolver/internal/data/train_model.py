"""Train LightGBM model directly using JSONL training data."""
import os
os.environ["PYTHONIOENCODING"] = "utf-8"

import json
import sys
from pathlib import Path
import numpy as np
import lightgbm as lgb
from sklearn.model_selection import TimeSeriesSplit
from sklearn.metrics import mean_squared_error

TRAINING_DIR = Path(os.environ.get("TRAINING_DATA_DIR", r"C:\Users\Test\.openclaw\workspace-hex\project\bigvolver\training_data"))
MODEL_DIR = Path(r"C:\Users\Test\.openclaw\workspace-hex\project\bigvolver\project\bigvolver\models")
MODEL_DIR.mkdir(exist_ok=True)

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


def train(symbol="BTCUSDT"):
    data_path = TRAINING_DIR / f"training_data_{symbol}.jsonl"
    if not data_path.exists():
        print(f"ERROR: {data_path} not found")
        return

    records = []
    bad = 0
    with open(data_path) as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    records.append(json.loads(line))
                except json.JSONDecodeError:
                    bad += 1
    if bad:
        print(f"Skipped {bad} bad lines")

    print(f"Loaded {len(records)} records from {data_path}")

    if len(records) < 100:
        print(f"ERROR: Need >= 100 samples, got {len(records)}")
        return

    # Build feature matrix
    X = np.array([[r["features"].get(f, 0.0) for f in DEFAULT_FEATURES] for r in records])
    y = np.array([r["target"] for r in records])

    print(f"Feature matrix: {X.shape}")
    print(f"Target range: [{y.min():.4f}, {y.max():.4f}]")

    # Walk-forward validation
    tscv = TimeSeriesSplit(n_splits=5)
    all_preds = []
    all_actuals = []

    for train_idx, val_idx in tscv.split(X):
        X_train, X_val = X[train_idx], X[val_idx]
        y_train, y_val = y[train_idx], y[val_idx]

        train_data = lgb.Dataset(X_train, label=y_train)
        val_data = lgb.Dataset(X_val, label=y_val, reference=train_data)

        params = {
            "objective": "regression",
            "metric": "mse",
            "learning_rate": 0.05,
            "num_leaves": 31,
            "max_depth": 6,
            "feature_fraction": 0.8,
            "bagging_fraction": 0.8,
            "bagging_freq": 5,
            "verbose": -1,
            "seed": 42,
        }

        bst = lgb.train(
            params,
            train_data,
            num_boost_round=200,
            valid_sets=[val_data],
            callbacks=[lgb.early_stopping(20, verbose=False)],
        )

        preds = bst.predict(X_val)
        all_preds.extend(preds)
        all_actuals.extend(y_val)

    all_preds = np.array(all_preds)
    all_actuals = np.array(all_actuals)

    # Metrics
    correct_dir = ((all_preds > 0) & (all_actuals > 0)) | ((all_preds <= 0) & (all_actuals <= 0))
    win_rate = float(np.mean(correct_dir))
    mse = mean_squared_error(all_actuals, all_preds)
    mean_ret = np.mean(all_preds)
    std_ret = np.std(all_preds)
    sharpe = (mean_ret / std_ret * np.sqrt(8760)) if std_ret > 0 else 0

    print(f"\n=== Walk-Forward CV Results ===")
    print(f"  Sharpe Ratio: {sharpe:.2f}")
    print(f"  Win Rate: {win_rate*100:.1f}%")
    print(f"  MSE: {mse:.6f}")
    print(f"  Mean predicted return: {mean_ret*100:.4f}%")

    # Train final model on all data
    print(f"\nTraining final model on all {len(records)} samples...")
    train_data = lgb.Dataset(X, label=y)
    model = lgb.train(params, train_data, num_boost_round=200)

    # Save
    from datetime import datetime, timezone
    version = datetime.now(timezone.utc).strftime("lgm-%Y%m%d-%H%M%S")

    model.save_model(str(MODEL_DIR / f"model_{version}.txt"))
    model.save_model(str(MODEL_DIR / "lightgbm_model.txt"))

    metadata = {
        "version": version,
        "trained_at": datetime.now(timezone.utc).isoformat(),
        "samples_used": len(records),
        "sharpe_ratio": round(sharpe, 4),
        "win_rate": round(win_rate, 4),
        "mse": round(mse, 6),
        "symbol": symbol,
        "features": DEFAULT_FEATURES,
    }

    with open(MODEL_DIR / f"metadata_{version}.json", "w") as f:
        json.dump(metadata, f, indent=2)
    with open(MODEL_DIR / "metadata.json", "w") as f:
        json.dump(metadata, f, indent=2)

    print(f"\nModel saved: v{version}")
    print(f"  Sharpe: {sharpe:.2f} | Win Rate: {win_rate*100:.1f}%")

    # Feature importance
    importance = dict(zip(DEFAULT_FEATURES, model.feature_importance(importance_type="gain")))
    top5 = sorted(importance.items(), key=lambda x: x[1], reverse=True)[:5]
    print(f"\n  Top 5 features:")
    for name, gain in top5:
        print(f"    {name}: {gain:.1f}")

    return model, metadata


if __name__ == "__main__":
    symbol = sys.argv[1] if len(sys.argv) > 1 else "BTCUSDT"
    train(symbol)
