"""
BigVolver Experiment Tracker — Unified ML/DRL experiment tracking.

Uses MLflow as primary backend (self-hosted, REST API accessible from Go).
Falls back to local JSONL file when MLflow is unavailable.
"""

import json
import os
import time
from datetime import datetime, timezone
from pathlib import Path

import pandas as pd

from config import (
    MLFLOW_TRACKING_URI,
    MLFLOW_EXPERIMENT_NAME,
    METRICS_DIR,
    METRICS_DB_PATH,
    PREDICTION_LOG_INTERVAL,
    DEFAULT_TAGS,
)


class ExperimentTracker:
    """Unified experiment tracking for ML and DRL pipelines.

    Supports two backends:
    - "mlflow": Full MLflow integration (preferred)
    - "local": Local JSONL file fallback (no external dependency)
    """

    def __init__(
        self,
        backend: str = "mlflow",
        project_name: str = None,
        tracking_uri: str = None,
    ):
        """
        Args:
            backend: "mlflow" or "local".
            project_name: MLflow experiment name.
            tracking_uri: MLflow server URI.
        """
        self.backend = backend.lower()
        self.project_name = project_name or MLFLOW_EXPERIMENT_NAME
        self.tracking_uri = tracking_uri or MLFLOW_TRACKING_URI

        self._mlflow_client = None
        self._prediction_counter = 0

        # Ensure metrics directory exists
        Path(METRICS_DIR).mkdir(parents=True, exist_ok=True)

        # Try to initialize MLflow
        if self.backend == "mlflow":
            try:
                import mlflow
                mlflow.set_tracking_uri(self.tracking_uri)
                mlflow.set_experiment(self.project_name)
                self._mlflow_client = mlflow
                print(f"[Tracker] MLflow connected: {self.tracking_uri}")
            except ImportError:
                print("[Tracker] mlflow not installed. Falling back to local storage.")
                self.backend = "local"
            except Exception as e:
                print(f"[Tracker] MLflow connection failed: {e}. Falling back to local.")
                self.backend = "local"

        if self.backend == "local":
            print(f"[Tracker] Using local storage: {METRICS_DB_PATH}")

    # --- Core Logging Methods ---

    def log_training(
        self,
        model_type: str,
        version: str,
        params: dict,
        metrics: dict,
        artifacts: list = None,
        tags: dict = None,
    ) -> str:
        """Log a training event (ML retrain or DRL train).

        Args:
            model_type: "lightgbm", "ppo", or "sac".
            version: Model version string.
            params: Hyperparameters.
            metrics: Training metrics (sharpe, win_rate, mse, etc.).
            artifacts: List of file paths to log as artifacts.
            tags: Additional tags (symbol, phase, data_range).

        Returns:
            run_id string.
        """
        all_tags = {**DEFAULT_TAGS, "model_type": model_type, "version": version}
        if tags:
            all_tags.update(tags)

        if self.backend == "mlflow" and self._mlflow_client:
            return self._log_training_mlflow(model_type, version, params, metrics, artifacts, all_tags)
        else:
            return self._log_training_local(model_type, version, params, metrics, artifacts, all_tags)

    def _log_training_mlflow(self, model_type, version, params, metrics, artifacts, tags):
        """Log training via MLflow."""
        mlflow = self._mlflow_client

        with mlflow.start_run(run_name=f"{model_type}-{version}") as run:
            mlflow.set_tags(tags)
            mlflow.log_params(params)
            mlflow.log_metrics(metrics)

            if artifacts:
                for artifact_path in artifacts:
                    if os.path.exists(artifact_path):
                        mlflow.log_artifact(artifact_path)

            run_id = run.info.run_id
            print(f"[Tracker] Logged training {model_type} v{version} → run {run_id[:8]}")
            return run_id

    def _log_training_local(self, model_type, version, params, metrics, artifacts, tags):
        """Log training to local JSONL file."""
        record = {
            "event": "training",
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "model_type": model_type,
            "version": version,
            "params": params,
            "metrics": metrics,
            "tags": tags,
        }

        if artifacts:
            record["artifacts"] = [str(a) for a in artifacts if os.path.exists(a)]

        run_id = f"local-{int(time.time() * 1000)}"
        record["run_id"] = run_id

        self._append_record(record)
        print(f"[Tracker] Logged training {model_type} v{version} → {run_id}")
        return run_id

    def log_prediction(
        self,
        model_type: str,
        version: str,
        symbol: str,
        features: dict,
        prediction: dict,
    ) -> None:
        """Log a prediction event (sampled to avoid flooding).

        Args:
            model_type: "lightgbm", "ppo", or "sac".
            version: Model version.
            symbol: Trading symbol.
            features: Feature dict (summary only).
            prediction: Signal, confidence, predicted_return.
        """
        self._prediction_counter += 1
        if self._prediction_counter % PREDICTION_LOG_INTERVAL != 0:
            return

        record = {
            "event": "prediction",
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "model_type": model_type,
            "version": version,
            "symbol": symbol,
            "prediction": prediction,
            "feature_summary": {
                k: v for k, v in features.items()
                if k in ["rsi_14", "macd_histogram", "atr_14", "funding_rate",
                         "volatility_24h", "momentum_4h", "regime_trending"]
            },
        }

        if self.backend == "mlflow" and self._mlflow_client:
            # Log as a metric update to the latest run
            try:
                self._mlflow_client.log_metrics({
                    f"pred_{symbol}_confidence": prediction.get("confidence", 0),
                    f"pred_{symbol}_return": prediction.get("predicted_return", 0),
                })
            except Exception:
                pass
        else:
            self._append_record(record)

    def log_retrain(
        self,
        symbol: str,
        old_version: str,
        new_version: str,
        old_sharpe: float,
        new_sharpe: float,
        rolled_back: bool,
        reason: str = "",
    ) -> None:
        """Log a retrain event (including rollback).

        Args:
            symbol: Trading symbol.
            old_version: Previous model version.
            new_version: New model version.
            old_sharpe: Previous Sharpe ratio.
            new_sharpe: New Sharpe ratio.
            rolled_back: Whether a rollback occurred.
            reason: Reason for rollback (if applicable).
        """
        record = {
            "event": "retrain",
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "symbol": symbol,
            "old_version": old_version,
            "new_version": new_version,
            "old_sharpe": old_sharpe,
            "new_sharpe": new_sharpe,
            "rolled_back": rolled_back,
            "reason": reason,
            "sharpe_change": round(new_sharpe - old_sharpe, 4),
            "sharpe_change_pct": round(
                (new_sharpe - old_sharpe) / max(abs(old_sharpe), 0.01) * 100, 2
            ) if old_sharpe != 0 else 0,
        }

        if self.backend == "mlflow" and self._mlflow_client:
            try:
                with self._mlflow_client.start_run(run_name=f"retrain-{symbol}") as run:
                    self._mlflow_client.log_params({
                        "symbol": symbol,
                        "old_version": old_version,
                        "new_version": new_version,
                        "rolled_back": str(rolled_back),
                    })
                    self._mlflow_client.log_metrics({
                        "old_sharpe": old_sharpe,
                        "new_sharpe": new_sharpe,
                        "sharpe_change": record["sharpe_change"],
                    })
                    run_id = run.info.run_id
                    print(f"[Tracker] Logged retrain {symbol} → run {run_id[:8]}")
            except Exception:
                self._append_record(record)
        else:
            self._append_record(record)
            print(f"[Tracker] Logged retrain {symbol}")

    # --- Query Methods ---

    def get_latest_metrics(self, model_type: str) -> dict:
        """Get the latest training metrics for a model type.

        Args:
            model_type: "lightgbm", "ppo", or "sac".

        Returns:
            Dict of latest metrics, or empty dict if none found.
        """
        if self.backend == "mlflow" and self._mlflow_client:
            try:
                mlflow = self._mlflow_client
                client = mlflow.tracking.MlflowClient(self.tracking_uri)

                experiment = client.get_experiment_by_name(self.project_name)
                if not experiment:
                    return {}

                runs = client.search_runs(
                    experiment_ids=[experiment.experiment_id],
                    filter_string=f"tags.model_type = '{model_type}'",
                    max_results=1,
                    order_by=["start_time DESC"],
                )

                if not runs:
                    return {}

                run = runs[0]
                return {
                    "run_id": run.info.run_id,
                    "version": run.data.tags.get("version", "unknown"),
                    "metrics": {k: v for k, v in run.data.metrics.items()},
                    "params": {k: v for k, v in run.data.params.items()},
                    "start_time": datetime.fromtimestamp(run.info.start_time / 1000).isoformat(),
                }
            except Exception as e:
                print(f"[Tracker] MLflow query failed: {e}")
                return self._get_latest_metrics_local(model_type)
        else:
            return self._get_latest_metrics_local(model_type)

    def _get_latest_metrics_local(self, model_type: str) -> dict:
        """Get latest metrics from local JSONL."""
        records = self._read_records()
        training_records = [
            r for r in records
            if r.get("event") == "training" and r.get("model_type") == model_type
        ]

        if not training_records:
            return {}

        latest = training_records[-1]
        return {
            "run_id": latest.get("run_id", ""),
            "version": latest.get("version", "unknown"),
            "metrics": latest.get("metrics", {}),
            "params": latest.get("params", {}),
            "start_time": latest.get("timestamp", ""),
        }

    def compare_runs(self, run_ids: list) -> pd.DataFrame:
        """Compare metrics across multiple runs.

        Args:
            run_ids: List of run IDs to compare.

        Returns:
            DataFrame with metrics comparison.
        """
        if self.backend == "mlflow" and self._mlflow_client:
            try:
                mlflow = self._mlflow_client
                client = mlflow.tracking.MlflowClient(self.tracking_uri)

                rows = []
                for run_id in run_ids:
                    try:
                        run = client.get_run(run_id)
                        row = {
                            "run_id": run_id[:8],
                            "version": run.data.tags.get("version", "?"),
                            "model_type": run.data.tags.get("model_type", "?"),
                            "symbol": run.data.tags.get("symbol", "?"),
                        }
                        row.update(run.data.metrics)
                        rows.append(row)
                    except Exception:
                        continue

                return pd.DataFrame(rows) if rows else pd.DataFrame()
            except Exception:
                pass

        # Fallback: local
        records = self._read_records()
        training_records = [r for r in records if r.get("event") == "training"]

        rows = []
        for r in training_records:
            if r.get("run_id") in run_ids:
                row = {
                    "run_id": r.get("run_id", "?")[:8],
                    "version": r.get("version", "?"),
                    "model_type": r.get("model_type", "?"),
                    "symbol": r.get("tags", {}).get("symbol", "?"),
                }
                row.update(r.get("metrics", {}))
                rows.append(row)

        return pd.DataFrame(rows) if rows else pd.DataFrame()

    def get_retrain_history(self, limit: int = 50) -> list:
        """Get recent retrain/rollback events.

        Args:
            limit: Maximum number of events to return.

        Returns:
            List of retrain event dicts.
        """
        records = self._read_records()
        retrain_records = [r for r in records if r.get("event") == "retrain"]
        return retrain_records[-limit:]

    def get_all_model_metrics(self) -> dict:
        """Get latest metrics for all model types.

        Returns:
            Dict keyed by model_type with latest metrics.
        """
        result = {}
        for model_type in ["lightgbm", "ppo", "sac"]:
            metrics = self.get_latest_metrics(model_type)
            if metrics:
                result[model_type] = metrics
        return result

    # --- Local Storage Helpers ---

    def _append_record(self, record: dict):
        """Append a record to the local JSONL file."""
        with open(METRICS_DB_PATH, "a") as f:
            f.write(json.dumps(record) + "\n")

    def _read_records(self) -> list:
        """Read all records from local JSONL file."""
        if not os.path.exists(METRICS_DB_PATH):
            return []

        records = []
        with open(METRICS_DB_PATH) as f:
            for line in f:
                line = line.strip()
                if line:
                    try:
                        records.append(json.loads(line))
                    except json.JSONDecodeError:
                        continue
        return records

    def get_status(self) -> dict:
        """Return tracker status information."""
        return {
            "backend": self.backend,
            "mlflow_uri": self.tracking_uri if self.backend == "mlflow" else None,
            "experiment": self.project_name,
            "mlflow_connected": self._mlflow_client is not None,
            "prediction_counter": self._prediction_counter,
            "local_db_exists": os.path.exists(METRICS_DB_PATH),
        }


# --- Module-level singleton ---
_tracker = None


def get_tracker(backend: str = "mlflow") -> ExperimentTracker:
    """Get or create the global tracker instance."""
    global _tracker
    if _tracker is None:
        _tracker = ExperimentTracker(backend=backend)
    return _tracker
