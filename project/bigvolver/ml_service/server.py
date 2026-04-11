"""
BigVolver ML Service — LightGBM Prediction & Retraining API

Endpoints:
  GET  /health       — Service health check
  POST /predict      — Predict signal from features
  POST /retrain      — Trigger model retraining
  GET  /model/status — Current model info
"""

import json
import time
import hashlib
import os
from datetime import datetime, timezone
from pathlib import Path

import numpy as np
import pandas as pd
import lightgbm as lgb
from flask import Flask, request, jsonify
from sklearn.metrics import accuracy_score, mean_squared_error
from sklearn.model_selection import TimeSeriesSplit

app = Flask(__name__)

# --- Config ---
MODEL_DIR = Path(os.environ.get("MODEL_DIR", "./models"))
MODEL_DIR.mkdir(exist_ok=True)
MODEL_PATH = MODEL_DIR / "lightgbm_model.txt"
FEATURE_ORDER_PATH = MODEL_DIR / "feature_order.json"

DEFAULT_WINDOW_DAYS = 30
DEFAULT_MIN_SAMPLES = 500

# --- Feature order (must match Go side) ---
DEFAULT_FEATURES = [
    # Technical
    "ema_5", "ema_20", "ema_50", "ema_200",
    "rsi_14", "rsi_28",
    "macd_line", "macd_signal", "macd_histogram",
    "atr_14", "bollinger_upper", "bollinger_lower",
    "adx_14", "obv", "volume_ratio",
    # Microstructure
    "funding_rate", "funding_rate_change_1h", "funding_rate_change_8h",
    # Derived
    "volatility_1h", "volatility_4h", "volatility_24h",
    "momentum_1h", "momentum_4h",
    "mean_reversion_score",
    "regime_trending", "regime_ranging", "regime_volatile",
]

# --- Global state ---
model = None
model_version = "no-model"
model_metadata = {
    "trained_at": None,
    "samples_used": 0,
    "sharpe_ratio": 0.0,
    "win_rate": 0.0,
}


def load_model():
    """Load model from disk if available."""
    global model, model_version, model_metadata
    if MODEL_PATH.exists():
        model = lgb.Booster(model_file=str(MODEL_PATH))
        meta_path = MODEL_DIR / "metadata.json"
        if meta_path.exists():
            with open(meta_path) as f:
                model_metadata = json.load(f)
        model_version = model_metadata.get("version", "unknown")
        print(f"[INFO] Loaded model v{model_version}")


def save_model():
    """Persist model and metadata to disk (versioned)."""
    global model, model_version, model_metadata
    if model is None:
        return

    # Save model with version in filename
    versioned_model_path = MODEL_DIR / f"model_{model_version}.txt"
    model.save_model(str(versioned_model_path))

    # Also save as default (latest)
    model.save_model(str(MODEL_PATH))

    # Save versioned metadata
    model_metadata["version"] = model_version
    versioned_meta_path = MODEL_DIR / f"metadata_{model_version}.json"
    with open(versioned_meta_path, "w") as f:
        json.dump(model_metadata, f, indent=2)

    # Also save as default metadata
    with open(MODEL_DIR / "metadata.json", "w") as f:
        json.dump(model_metadata, f, indent=2)

    print(f"[INFO] Saved model v{model_version}")


def features_to_vector(features: dict, feature_order: list = None) -> np.ndarray:
    """Convert feature dict to ordered numpy array."""
    if feature_order is None:
        feature_order = DEFAULT_FEATURES
    return np.array([features.get(f, 0.0) for f in feature_order])


def generate_version() -> str:
    """Generate a unique model version string."""
    ts = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
    return f"lgm-{ts}"


def compute_sharpe(returns: np.ndarray) -> float:
    """Compute annualized Sharpe ratio (assuming hourly returns)."""
    if len(returns) < 2:
        return 0.0
    mean_ret = np.mean(returns)
    std_ret = np.std(returns)
    if std_ret == 0:
        return 0.0
    # Annualize: ~8760 hours per year
    sharpe = (mean_ret / std_ret) * np.sqrt(8760)
    return round(sharpe, 4)


def walk_forward_validate(X: np.ndarray, y: np.ndarray, n_splits: int = 5) -> dict:
    """Walk-forward cross-validation for time series."""
    tscv = TimeSeriesSplit(n_splits=n_splits)
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

    # Compute metrics
    returns = all_preds  # predicted returns as strategy returns
    sharpe = compute_sharpe(returns)

    # Win rate: fraction of correct direction predictions
    correct_dir = ((all_preds > 0) & (all_actuals > 0)) | ((all_preds <= 0) & (all_actuals <= 0))
    win_rate = float(np.mean(correct_dir))

    mse = mean_squared_error(all_actuals, all_preds)

    return {
        "sharpe_ratio": sharpe,
        "win_rate": round(win_rate, 4),
        "mse": round(mse, 6),
    }


# --- Routes ---

@app.route("/health", methods=["GET"])
def health():
    return jsonify({
        "status": "ok",
        "model_version": model_version,
        "model_loaded": model is not None,
    })


@app.route("/predict", methods=["POST"])
def predict():
    global model

    if model is None:
        return jsonify({"error": "no model loaded — run /retrain first"}), 503

    data = request.get_json()
    if not data or "features" not in data:
        return jsonify({"error": "missing 'features' field"}), 400

    features = data["features"]
    symbol = data.get("symbol", "unknown")

    X = features_to_vector(features).reshape(1, -1)
    predicted_return = float(model.predict(X)[0])

    # Determine signal — dynamic threshold based on ATR
    atr = features.get("atr_14", 0)
    close_price = features.get("ema_20", 50000)  # proxy for current price
    if close_price > 0 and atr > 0:
        # Threshold = 1 ATR as % of price (typically 0.1-0.5%)
        dynamic_threshold = (atr / close_price) * 100
    else:
        dynamic_threshold = 0.3  # fallback default

    if predicted_return > dynamic_threshold:
        signal = "LONG"
    elif predicted_return < -dynamic_threshold:
        signal = "SHORT"
    else:
        signal = "NEUTRAL"

    confidence = min(abs(predicted_return) / max(dynamic_threshold*2, 0.1), 1.0)

    # SHAP-like feature importance (use built-in gain)
    importance = dict(zip(DEFAULT_FEATURES, model.feature_importance(importance_type="gain")))
    top_features = dict(sorted(importance.items(), key=lambda x: x[1], reverse=True)[:5])

    return jsonify({
        "predicted_return": round(predicted_return, 4),
        "signal": signal,
        "confidence": round(confidence, 4),
        "model_version": model_version,
        "symbol": symbol,
        "shap_values": top_features,
    })


@app.route("/retrain", methods=["POST"])
def retrain():
    global model, model_version, model_metadata

    data = request.get_json() or {}
    symbol = data.get("symbol", "BTCUSDT")
    window_days = data.get("window_size_days", DEFAULT_WINDOW_DAYS)
    min_samples = data.get("min_samples", DEFAULT_MIN_SAMPLES)

    # --- Load training data ---
    # Option 1: Direct JSON body (records array)
    # Option 2: JSONL file from Go pipeline
    records = []

    if "records" in data and isinstance(data["records"], list) and len(data["records"]) > 0:
        records = data["records"]
        print(f"[INFO] Using {len(records)} records from request body")
    else:
        # Load from JSONL file
        data_path = Path(os.environ.get("TRAINING_DATA_DIR", "./data")) / f"training_data_{symbol}.jsonl"

        if not data_path.exists():
            return jsonify({
                "success": False,
                "error": f"no training data at {data_path} — run Go feature pipeline first or send records in body",
            }), 400

        with open(data_path) as f:
            for line in f:
                line = line.strip()
                if line:
                    records.append(json.loads(line))

    if len(records) < min_samples:
        return jsonify({
            "success": False,
            "error": f"insufficient samples: {len(records)} (need >= {min_samples})",
        }), 400

    # Build feature matrix
    X = np.array([features_to_vector(r["features"]) for r in records])
    y = np.array([r["target"] for r in records])

    # Walk-forward validation
    print(f"[INFO] Walk-forward validation with {len(X)} samples...")
    cv_metrics = walk_forward_validate(X, y, n_splits=5)
    print(f"[INFO] CV Sharpe: {cv_metrics['sharpe_ratio']}, Win Rate: {cv_metrics['win_rate']}")

    # Train final model on all data
    print(f"[INFO] Training final model...")
    train_data = lgb.Dataset(X, label=y)

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

    model = lgb.train(params, train_data, num_boost_round=200)

    # Update metadata
    model_version = generate_version()
    model_metadata = {
        "trained_at": datetime.now(timezone.utc).isoformat(),
        "samples_used": len(records),
        "sharpe_ratio": cv_metrics["sharpe_ratio"],
        "win_rate": cv_metrics["win_rate"],
        "features": DEFAULT_FEATURES,
    }

    save_model()

    print(f"[INFO] Training complete. v{model_version}")

    return jsonify({
        "success": True,
        "model_version": model_version,
        "sharpe_ratio": cv_metrics["sharpe_ratio"],
        "win_rate": cv_metrics["win_rate"],
        "samples_used": len(records),
        "train_time_sec": 0,  # TODO: measure actual time
    })


@app.route("/model/status", methods=["GET"])
def model_status():
    return jsonify({
        "version": model_version,
        "loaded": model is not None,
        "metadata": model_metadata,
        "feature_count": len(DEFAULT_FEATURES),
    })


@app.route("/model/load", methods=["POST"])
def load_specific_version():
    """Load a specific model version from the models directory."""
    global model, model_version, model_metadata

    data = request.get_json() or {}
    target_version = data.get("version", "")

    if not target_version:
        return jsonify({"success": False, "error": "missing 'version' field"}), 400

    # Look for a model file matching the version
    # Version format: lgm-YYYYMMDD-HHMMSS
    # Model files saved with timestamp in metadata
    model_files = sorted(MODEL_DIR.glob("*.txt"))

    for mf in model_files:
        # Try loading and checking metadata
        try:
            candidate = lgb.Booster(model_file=str(mf))
        except Exception:
            continue

        # Check if this matches the requested version
        # Try to find matching metadata file
        meta_file = MODEL_DIR / "metadata.json"
        if meta_file.exists():
            with open(meta_file) as f:
                all_meta = json.load(f)
            # Check if any saved version matches
            # The metadata stores the latest; for older versions, check model dir

    # Strategy: find metadata files with version info
    meta_files = sorted(MODEL_DIR.glob("metadata_*.json"))
    for mf in meta_files:
        try:
            with open(mf) as f:
                meta = json.load(f)
            if meta.get("version") == target_version:
                model_path = MODEL_DIR / f"model_{target_version}.txt"
                if not model_path.exists():
                    return jsonify({
                        "success": False,
                        "error": f"model file not found for version {target_version}",
                    }), 404

                model = lgb.Booster(model_file=str(model_path))
                model_version = target_version
                model_metadata = meta
                print(f"[INFO] Loaded model version {target_version}")
                return jsonify({"success": True, "version": target_version})
        except Exception:
            continue

    return jsonify({
        "success": False,
        "error": f"version {target_version} not found in registry",
    }), 404


@app.route("/model/list", methods=["GET"])
def list_versions():
    """List all available model versions."""
    versions = []

    # Read from metadata files
    for mf in sorted(MODEL_DIR.glob("metadata_*.json")):
        try:
            with open(mf) as f:
                meta = json.load(f)
            versions.append({
                "version": meta.get("version", "unknown"),
                "trained_at": meta.get("trained_at", ""),
                "sharpe_ratio": meta.get("sharpe_ratio", 0),
                "win_rate": meta.get("win_rate", 0),
                "samples_used": meta.get("samples_used", 0),
            })
        except Exception:
            continue

    # Also check default metadata
    default_meta = MODEL_DIR / "metadata.json"
    if default_meta.exists():
        try:
            with open(default_meta) as f:
                meta = json.load(f)
            ver = meta.get("version", "unknown")
            if not any(v["version"] == ver for v in versions):
                versions.append({
                    "version": ver,
                    "trained_at": meta.get("trained_at", ""),
                    "sharpe_ratio": meta.get("sharpe_ratio", 0),
                    "win_rate": meta.get("win_rate", 0),
                    "samples_used": meta.get("samples_used", 0),
                })
        except Exception:
            pass

    return jsonify({"versions": versions, "current": model_version})


if __name__ == "__main__":
    load_model()
    print("[INFO] BigVolver ML Service starting on :5001")
    app.run(host="0.0.0.0", port=5001, debug=False)
