"""
BigVolver Telemetry Config — MLflow connection and experiment settings.
"""

import os

# MLflow settings
MLFLOW_TRACKING_URI = os.environ.get("MLFLOW_TRACKING_URI", "http://localhost:5000")
MLFLOW_EXPERIMENT_NAME = os.environ.get("MLFLOW_EXPERIMENT_NAME", "bigvolver-v2")

# Data directory for local metrics storage (fallback when MLflow is unavailable)
METRICS_DIR = os.environ.get("METRICS_DIR", "./telemetry_data")
METRICS_DB_PATH = os.path.join(METRICS_DIR, "metrics.jsonl")

# Prediction sampling — log every Nth prediction to avoid overwhelming MLflow
PREDICTION_LOG_INTERVAL = int(os.environ.get("PREDICTION_LOG_INTERVAL", "10"))

# Tags for experiment organization
DEFAULT_TAGS = {
    "project": "bigvolver",
    "version": "v2",
}
