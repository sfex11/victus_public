"""
BigVolver DRL API Service — Flask REST API for DRL predictions and training.

Runs on port 5002 (separate from ML service on 5001).
"""

import json
import os
import sys
import time
import traceback
from datetime import datetime, timezone
from pathlib import Path

import numpy as np
import pandas as pd
from flask import Flask, request, jsonify

# Add parent dirs to path for imports
sys.path.insert(0, str(Path(__file__).parent))

from agent import DRLAgent, MODEL_DIR
from env import TradingEnv, FEATURE_COLS

app = Flask(__name__)

# --- Config ---
DRL_MODEL_DIR = Path(os.environ.get("DRL_MODEL_DIR", "./drl_models"))
DRL_MODEL_DIR.mkdir(exist_ok=True)
DRL_LOG_DIR = Path(os.environ.get("DRL_LOG_DIR", "./drl_logs"))
DRL_LOG_DIR.mkdir(exist_ok=True)

# --- Global State ---
agents = {}       # algorithm -> DRLAgent
envs = {}         # symbol -> TradingEnv
current_algo = "ppo"


def load_latest_model(algorithm: str = None):
    """Load the latest available model for an algorithm."""
    global current_algo
    if algorithm:
        current_algo = algorithm

    model_files = sorted(DRL_MODEL_DIR.glob(f"{current_algo}-v*.zip"))
    if not model_files:
        print(f"[DRL API] No {current_algo} model found. Train first.")
        return None

    latest = model_files[-1]
    return str(latest)


# --- Routes ---

@app.route("/drl/health", methods=["GET"])
def health():
    return jsonify({
        "status": "ok",
        "algorithms": list(agents.keys()),
        "current_algorithm": current_algo,
        "models_loaded": {k: v.get_model_info() for k, v in agents.items()},
        "env_symbols": list(envs.keys()),
    })


@app.route("/drl/predict", methods=["POST"])
def predict():
    """DRL prediction — returns weight vector for given features."""
    data = request.get_json()
    if not data:
        return jsonify({"error": "missing request body"}), 400

    algorithm = data.get("algorithm", current_algo)
    features_input = data.get("features", {})

    if algorithm not in agents or agents[algorithm].model is None:
        return jsonify({
            "error": f"no {algorithm} model loaded — train first",
        }), 503

    agent = agents[algorithm]

    weights = []
    total_confidence = 0.0

    for symbol, features in features_input.items():
        # Build observation from features
        # Features should include window_size rows
        if isinstance(features, dict):
            # Single observation point — need window from env
            if symbol not in envs:
                return jsonify({
                    "error": f"no environment loaded for {symbol} — load data first",
                }), 400

            env = envs[symbol]
            obs = env._get_observation()
        elif isinstance(features, list):
            # Array of observations (window)
            obs = np.array(features, dtype=np.float32)
        else:
            continue

        # Predict
        action = agent.predict(obs)
        weight = float(np.clip(action[0], -1.0, 1.0))

        # Determine signal
        if weight > 0.1:
            signal = "LONG"
        elif weight < -0.1:
            signal = "SHORT"
        else:
            signal = "NEUTRAL"

        confidence = min(abs(weight), 1.0)
        total_confidence += confidence

        weights.append({
            "symbol": symbol,
            "weight": round(weight, 4),
            "signal": signal,
            "confidence": round(confidence, 4),
        })

    avg_confidence = total_confidence / max(len(weights), 1)
    model_info = agent.get_model_info()

    return jsonify({
        "weights": weights,
        "algorithm": algorithm,
        "model_version": model_info["version"],
        "confidence": round(avg_confidence, 4),
    })


@app.route("/drl/train", methods=["POST"])
def train():
    """Start DRL training."""
    global current_algo

    data = request.get_json() or {}
    algorithm = data.get("algorithm", "ppo")
    symbol = data.get("symbol", "BTCUSDT")
    timesteps = data.get("timesteps", 100_000)
    records = data.get("data", None)

    current_algo = algorithm

    # Build DataFrame from records or load from file
    if records and len(records) > 0:
        df = pd.DataFrame(records)
    else:
        # Try to load from training data file
        data_dir = Path(os.environ.get("TRAINING_DATA_DIR", "./data"))
        data_path = data_dir / f"training_data_{symbol}.jsonl"

        if not data_path.exists():
            return jsonify({
                "success": False,
                "error": f"no data at {data_path} — send records or ensure data file exists",
            }), 400

        records_list = []
        with open(data_path) as f:
            for line in f:
                line = line.strip()
                if line:
                    records_list.append(json.loads(line))
        df = pd.DataFrame(records_list)

    if len(df) < 200:
        return jsonify({
            "success": False,
            "error": f"insufficient data: {len(df)} rows (need >= 200)",
        }), 400

    # Add OHLCV columns if missing (needed by TradingEnv)
    if "close" not in df.columns and "target" in df.columns:
        # Minimal: use target as close proxy for training
        df["close"] = df["target"] + 100  # Offset to avoid negative prices
        df["open"] = df["close"]
        df["high"] = df["close"] * 1.001
        df["low"] = df["close"] * 0.999
        df["volume"] = 1000

    # Flatten features into columns if they're nested
    if "features" in df.columns:
        features_df = pd.json_normalize(df["features"])
        for col in features_df.columns:
            df[col] = features_df[col].values
        df = df.drop(columns=["features"])

    # Fill missing feature columns with 0
    for col in FEATURE_COLS:
        if col not in df.columns:
            df[col] = 0.0

    # Create environment
    env = TradingEnv(
        df,
        reward_scheme="sharpe",
        window_size=50,
        verbose=False,
    )
    envs[symbol] = env

    # Create agent and train
    agent = DRLAgent(
        env,
        algorithm=algorithm,
        tensorboard_log=str(DRL_LOG_DIR),
        verbose=1,
    )

    try:
        train_info = agent.train(total_timesteps=timesteps)

        # Save model
        model_path = agent.save()

        agents[algorithm] = agent

        # Get post-training metrics
        metrics = env.get_portfolio_metrics()

        # Log training to telemetry
        try:
            import sys as _sys
            telemetry_path = str(Path(__file__).parent.parent / "telemetry")
            if telemetry_path not in _sys.path:
                _sys.path.insert(0, telemetry_path)
            from tracker import get_tracker
            tracker = get_tracker()
            tracker.log_training(
                model_type=algorithm,
                version=agent.model_version,
                params=train_info.get("hyperparams", {}),
                metrics={
                    "sharpe_ratio": metrics["sharpe_ratio"],
                    "win_rate": metrics["win_rate"],
                    "mean_reward": train_info.get("eval_mean_reward", 0),
                    "total_return": metrics["total_return"],
                    "max_drawdown": metrics["max_drawdown"],
                },
                tags={"symbol": symbol, "phase": "drl_train", "timesteps": str(timesteps)},
            )
        except Exception as te:
            print(f"[WARN] Telemetry logging failed: {te}")

        return jsonify({
            "success": True,
            "model_version": agent.model_version,
            "model_path": model_path,
            "mean_reward": train_info.get("eval_mean_reward", 0),
            "sharpe_ratio": metrics["sharpe_ratio"],
            "win_rate": metrics["win_rate"],
            "total_return": metrics["total_return"],
            "max_drawdown": metrics["max_drawdown"],
            "training_time_sec": train_info.get("training_time_sec", 0),
            "timesteps": timesteps,
            "data_rows": len(df),
        })

    except Exception as e:
        traceback.print_exc()
        return jsonify({
            "success": False,
            "error": str(e),
        }), 500


@app.route("/drl/model/list", methods=["GET"])
def list_models():
    """List all available DRL models."""
    models = []

    for model_file in sorted(DRL_MODEL_DIR.glob("*-v*.zip")):
        meta_file = model_file.with_suffix("").with_name(model_file.stem + "_meta.json")
        if not meta_file.exists():
            # Try alternate naming
            meta_file = model_file.parent / (model_file.stem + "_meta.json")

        info = {
            "path": str(model_file),
            "algorithm": model_file.stem.split("-v")[0] if "-v" in model_file.stem else "unknown",
        }

        if meta_file.exists():
            with open(meta_file) as f:
                meta = json.load(f)
            info.update({
                "version": meta.get("version", "unknown"),
                "saved_at": meta.get("saved_at", ""),
                "train_info": meta.get("train_info", {}),
            })

        models.append(info)

    return jsonify({
        "models": models,
        "loaded": {k: v.get_model_info() for k, v in agents.items()},
    })


@app.route("/drl/model/load", methods=["POST"])
def load_model():
    """Load a specific DRL model version."""
    global current_algo

    data = request.get_json() or {}
    version = data.get("version", "")
    algorithm = data.get("algorithm", "ppo")

    if not version:
        # Try to load the latest model
        model_files = sorted(DRL_MODEL_DIR.glob(f"{algorithm}-v*.zip"))
        if not model_files:
            return jsonify({
                "success": False,
                "error": f"no {algorithm} models found",
            }), 404
        version = model_files[-1].stem  # e.g., "ppo-v20260411-120000"

    model_path = DRL_MODEL_DIR / f"{version}.zip"
    if not model_path.exists():
        return jsonify({
            "success": False,
            "error": f"model {version} not found at {model_path}",
        }), 404

    # Need an env to load into — use existing or create dummy
    if algorithm not in agents:
        # Create a dummy env for loading
        dummy_df = pd.DataFrame(
            {col: [0.0] * 200 for col in FEATURE_COLS},
        )
        dummy_df["close"] = np.cumsum(np.random.randn(200)) + 50000
        dummy_df["open"] = dummy_df["close"]
        dummy_df["high"] = dummy_df["close"] * 1.001
        dummy_df["low"] = dummy_df["close"] * 0.999
        dummy_df["volume"] = 1000

        env = TradingEnv(dummy_df, verbose=False)
        envs["_dummy"] = env

        agent = DRLAgent(env, algorithm=algorithm)
    else:
        agent = agents[algorithm]

    try:
        agent.load(str(model_path))
        agents[algorithm] = agent
        current_algo = algorithm

        return jsonify({
            "success": True,
            "version": agent.model_version,
            "algorithm": algorithm,
            "model_info": agent.get_model_info(),
        })
    except Exception as e:
        traceback.print_exc()
        return jsonify({
            "success": False,
            "error": str(e),
        }), 500


if __name__ == "__main__":
    # Try to load latest models on startup
    for algo in ["ppo", "sac"]:
        latest = load_latest_model(algo)
        if latest:
            print(f"[DRL API] Found {algo} model: {latest}")

    print("[DRL API] BigVolver DRL Service starting on :5002")
    app.run(host="0.0.0.0", port=5002, debug=False)
