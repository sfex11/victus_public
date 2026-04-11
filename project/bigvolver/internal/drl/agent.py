"""
BigVolver DRL Agent — PPO/SAC trading agents using stable-baselines3.

Based on FinRL-X architecture patterns, minimized dependencies.
Uses stable-baselines3 + gymnasium directly.
"""

import os
import time
from datetime import datetime, timezone
from pathlib import Path

import numpy as np

MODEL_DIR = Path(os.environ.get("DRL_MODEL_DIR", "./drl_models"))
MODEL_DIR.mkdir(exist_ok=True)

PPO_DEFAULTS = {
    "learning_rate": 3e-4,
    "n_steps": 2048,
    "batch_size": 256,
    "n_epochs": 10,
    "gamma": 0.99,
    "gae_lambda": 0.95,
    "clip_range": 0.2,
    "ent_coef": 0.01,
    "vf_coef": 0.5,
    "max_grad_norm": 0.5,
    "policy": "MlpPolicy",
    "policy_kwargs": {
        "net_arch": {"pi": [256, 256], "vf": [256, 256]},
    },
}

SAC_DEFAULTS = {
    "learning_rate": 3e-4,
    "buffer_size": 100_000,
    "learning_starts": 1000,
    "batch_size": 256,
    "tau": 0.005,
    "gamma": 0.99,
    "ent_coef": "auto",
    "policy": "MlpPolicy",
}


class DRLAgent:
    """Deep Reinforcement Learning trading agent.

    Supports PPO (fast convergence, stable) and SAC (continuous action, sample efficient).
    """

    def __init__(
        self,
        env,
        algorithm: str = "ppo",
        policy_type: str = "MlpPolicy",
        tensorboard_log: str = "./drl_logs",
        verbose: int = 0,
    ):
        """
        Args:
            env: A gymnasium environment (TradingEnv).
            algorithm: "ppo" or "sac".
            policy_type: Policy network type (default MlpPolicy).
            tensorboard_log: Path for TensorBoard logs.
            verbose: SB3 verbosity level.
        """
        self.env = env
        self.algorithm = algorithm.lower()
        self.policy_type = policy_type
        self.tensorboard_log = tensorboard_log
        self.verbose = verbose

        self.model = None
        self.model_version = "no-model"
        self.train_info = {}

    def _get_params(self, overrides: dict = None) -> dict:
        """Get hyperparameters with optional overrides."""
        if self.algorithm == "ppo":
            params = PPO_DEFAULTS.copy()
            params["policy_kwargs"] = params["policy_kwargs"].copy()
        elif self.algorithm == "sac":
            params = SAC_DEFAULTS.copy()
        else:
            raise ValueError(f"Unknown algorithm: {self.algorithm}. Use 'ppo' or 'sac'.")

        if overrides:
            for k, v in overrides.items():
                params[k] = v

        return params

    def train(
        self,
        total_timesteps: int = 100_000,
        callback_list=None,
        hyperparams: dict = None,
    ) -> dict:
        """Train the DRL agent.

        Args:
            total_timesteps: Number of training steps.
            callback_list: Optional SB3 callback list.
            hyperparams: Optional hyperparameter overrides.

        Returns:
            Training metrics dict.
        """
        from stable_baselines3 import PPO, SAC

        params = self._get_params(hyperparams)
        policy_kwargs = params.pop("policy_kwargs", None)
        policy_type = params.pop("policy", self.policy_type)

        start_time = time.time()

        # Create model
        if self.algorithm == "ppo":
            self.model = PPO(
                policy_type,
                self.env,
                tensorboard_log=self.tensorboard_log,
                policy_kwargs=policy_kwargs,
                verbose=self.verbose,
                **params,
            )
        elif self.algorithm == "sac":
            self.model = SAC(
                policy_type,
                self.env,
                tensorboard_log=self.tensorboard_log,
                policy_kwargs=policy_kwargs,
                verbose=self.verbose,
                **params,
            )

        # Train
        self.model.learn(
            total_timesteps=total_timesteps,
            callback=callback_list,
            reset_num_timesteps=False,
        )

        training_time = time.time() - start_time

        # Evaluate post-training
        eval_result = self._evaluate(n_episodes=3)

        # Update version
        self.model_version = self._generate_version()
        self.train_info = {
            "algorithm": self.algorithm,
            "total_timesteps": total_timesteps,
            "training_time_sec": round(training_time, 2),
            "eval_mean_reward": eval_result["mean_reward"],
            "eval_std_reward": eval_result["std_reward"],
            "eval_sharpe": eval_result.get("sharpe_ratio", 0),
            "hyperparams": params,
        }

        return self.train_info

    def _evaluate(self, n_episodes: int = 5) -> dict:
        """Run evaluation episodes and return metrics."""
        rewards = []
        sharpe_values = []

        for _ in range(n_episodes):
            obs, _ = self.env.reset()
            episode_reward = 0
            done = False

            while not done:
                action, _ = self.model.predict(obs, deterministic=True)
                obs, reward, terminated, truncated, info = self.env.step(action)
                episode_reward += reward
                done = terminated or truncated

            rewards.append(episode_reward)
            metrics = self.env.get_portfolio_metrics()
            sharpe_values.append(metrics.get("sharpe_ratio", 0))

        return {
            "mean_reward": round(float(np.mean(rewards)), 4),
            "std_reward": round(float(np.std(rewards)), 4),
            "sharpe_ratio": round(float(np.mean(sharpe_values)), 4),
        }

    def predict(self, obs: np.ndarray) -> np.ndarray:
        """Predict action (weight) from observation.

        Args:
            obs: Current observation state.

        Returns:
            np.ndarray of shape matching action space — weights [-1, 1].
        """
        if self.model is None:
            raise RuntimeError("Model not trained. Call train() first.")

        action, _ = self.model.predict(obs, deterministic=True)
        return action

    def save(self, path: str = None) -> str:
        """Save the model to disk.

        Args:
            path: Optional custom path. Defaults to MODEL_DIR/version.

        Returns:
            Path where the model was saved.
        """
        if self.model is None:
            raise RuntimeError("No model to save.")

        if path is None:
            path = str(MODEL_DIR / f"{self.model_version}.zip")

        os.makedirs(os.path.dirname(path), exist_ok=True)
        self.model.save(path)

        # Save metadata
        meta = {
            "version": self.model_version,
            "algorithm": self.algorithm,
            "saved_at": datetime.now(timezone.utc).isoformat(),
            "train_info": self.train_info,
        }
        meta_path = path.replace(".zip", "_meta.json")
        import json
        with open(meta_path, "w") as f:
            json.dump(meta, f, indent=2)

        print(f"[DRL] Saved model to {path}")
        return path

    def load(self, path: str) -> None:
        """Load a model from disk.

        Args:
            path: Path to the saved model (.zip file).
        """
        from stable_baselines3 import PPO, SAC

        # Load metadata first
        meta_path = path.replace(".zip", "_meta.json")
        if os.path.exists(meta_path):
            import json
            with open(meta_path) as f:
                meta = json.load(f)
            self.model_version = meta.get("version", "unknown")
            self.algorithm = meta.get("algorithm", self.algorithm)
            self.train_info = meta.get("train_info", {})
        else:
            # Try to infer from filename
            fname = os.path.basename(path).replace(".zip", "")
            self.model_version = fname

        # Load the model
        if self.algorithm == "ppo":
            self.model = PPO.load(path, env=self.env)
        elif self.algorithm == "sac":
            self.model = SAC.load(path, env=self.env)
        else:
            raise ValueError(f"Cannot load algorithm: {self.algorithm}")

        print(f"[DRL] Loaded model {self.model_version} from {path}")

    def get_model_info(self) -> dict:
        """Return current model information."""
        return {
            "version": self.model_version,
            "algorithm": self.algorithm,
            "trained": self.model is not None,
            "train_info": self.train_info,
        }

    def _generate_version(self) -> str:
        """Generate a unique model version string."""
        ts = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
        return f"{self.algorithm}-v{ts}"
