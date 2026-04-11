"""
BigVolver DRL Trading Environment — Binance Futures simulator.

OpenAI Gym (gymnasium) compatible environment for DRL agent training.
Uses Sharpe-based reward function.
"""

import numpy as np
import gymnasium as gym
from gymnasium import spaces


# Feature columns matching config/features.yaml
FEATURE_COLS = [
    # Technical
    "ema_5", "ema_20", "ema_50", "ema_200",
    "rsi_14", "rsi_28", "macd_line", "macd_signal", "macd_histogram",
    "atr_14", "bollinger_upper", "bollinger_lower",
    "adx_14", "obv", "volume_ratio",
    # Microstructure
    "funding_rate", "funding_rate_change_1h", "funding_rate_change_8h",
    # Derived
    "volatility_1h", "volatility_4h", "volatility_24h",
    "momentum_1h", "momentum_4h", "mean_reversion_score",
    "regime_trending", "regime_ranging", "regime_volatile",
    # OHLCV (normalized)
    "open", "high", "low", "close", "volume",
    # Target
    "future_return_4h",
]


class TradingEnv(gym.Env):
    """Binance Futures trading environment for DRL.

    Simulates futures trading with:
    - Continuous action space: Box(-1, 1) for position weight
    - Sharpe-based reward function
    - 0.04% taker commission (both sides)
    - 0.05% slippage simulation
    """

    metadata = {"render_modes": ["human", "none"]}

    def __init__(
        self,
        df,
        initial_balance: float = 10000,
        commission: float = 0.0004,
        slippage: float = 0.0005,
        max_position: float = 1.0,
        reward_scheme: str = "sharpe",
        window_size: int = 50,
        verbose: bool = False,
        render_mode: str = "none",
    ):
        super().__init__()

        self.df = df.reset_index(drop=True)
        self.initial_balance = initial_balance
        self.commission = commission
        self.slippage = slippage
        self.max_position = max_position
        self.reward_scheme = reward_scheme
        self.window_size = window_size
        self.verbose = verbose
        self.render_mode = render_mode

        # Validate columns
        self.feature_cols = [c for c in FEATURE_COLS if c in self.df.columns]
        self.price_col = "close"
        n_features = len(self.feature_cols)

        # Action space: continuous weight [-1, 1]
        self.action_space = spaces.Box(
            low=-np.array([1.0]), high=np.array([1.0]), dtype=np.float32
        )

        # Observation space: [window_size x n_features]
        self.observation_space = spaces.Box(
            low=-np.inf, high=np.inf,
            shape=(window_size, n_features),
            dtype=np.float32,
        )

        # Portfolio state
        self.balance = initial_balance
        self.position = 0.0  # Current position weight [-1, 1]
        self.entry_price = 0.0
        self.equity_curve = [initial_balance]
        self.returns_history = []
        self.peak_equity = initial_balance
        self.total_trades = 0
        self.winning_trades = 0

        self.current_step = 0
        self.max_steps = len(self.df) - 1

    def reset(self, seed=None, options=None):
        """Reset the environment for a new episode."""
        super().reset(seed=seed)

        if seed is not None:
            np.random.seed(seed)

        self.balance = self.initial_balance
        self.position = 0.0
        self.entry_price = 0.0
        self.equity_curve = [self.initial_balance]
        self.returns_history = []
        self.peak_equity = self.initial_balance
        self.total_trades = 0
        self.winning_trades = 0

        # Start at a random position to avoid always starting at the same state
        self.current_step = self.window_size + np.random.randint(0, max(1, self.max_steps - 2 * self.window_size))

        obs = self._get_observation()
        info = self._get_info()

        return obs, info

    def step(self, action):
        """Execute one trading step.

        Args:
            action: np.ndarray — target position weight [-1, 1]

        Returns:
            observation, reward, terminated, truncated, info
        """
        if self.current_step >= self.max_steps:
            return self._get_observation(), 0.0, True, False, {}

        target_position = float(np.clip(action[0], -1.0, 1.0)) * self.max_position
        current_price = self.df.iloc[self.current_step][self.price_col]
        next_price = self.df.iloc[min(self.current_step + 1, self.max_steps)][self.price_col]

        # Calculate position change and associated costs
        position_change = abs(target_position - self.position)
        trade_cost = 0.0

        if position_change > 0.01:  # Minimum meaningful position change
            # Commission (both entry and exit)
            trade_cost = position_change * self.commission * 2
            # Slippage
            trade_cost += position_change * self.slippage

            # Count trades
            if abs(self.position) > 0.01 and abs(target_position) < 0.01:
                # Closing a position
                self.total_trades += 1
                pnl = self.position * (current_price - self.entry_price) / self.entry_price * self.balance
                if pnl > 0:
                    self.winning_trades += 1

            if abs(self.position) < 0.01 and abs(target_position) > 0.01:
                # Opening a position
                self.total_trades += 1
                self.entry_price = current_price

        # Update position
        self.position = target_position

        # Calculate step return
        price_return = (next_price - current_price) / current_price
        position_return = self.position * price_return
        net_return = position_return - trade_cost

        # Update equity
        new_equity = self.equity_curve[-1] * (1 + net_return)
        self.equity_curve.append(new_equity)
        self.returns_history.append(net_return)

        if new_equity > self.peak_equity:
            self.peak_equity = new_equity

        # Calculate reward
        reward = self._calculate_reward()

        # Advance step
        self.current_step += 1

        # Check termination
        terminated = False
        truncated = self.current_step >= self.max_steps

        # Liquidation: if equity drops below 10% of initial
        if new_equity < self.initial_balance * 0.1:
            terminated = True

        obs = self._get_observation()
        info = self._get_info()

        return obs, reward, terminated, truncated, info

    def _calculate_reward(self) -> float:
        """Calculate reward based on selected scheme."""
        if len(self.returns_history) < 2:
            return 0.0

        if self.reward_scheme == "sharpe":
            return self._sharpe_reward()
        elif self.reward_scheme == "sortino":
            return self._sortino_reward()
        elif self.reward_scheme == "return":
            return self.returns_history[-1] * 100
        else:
            return self._sharpe_reward()

    def _sharpe_reward(self) -> float:
        """Sharpe Ratio based reward.

        Optimizes risk-adjusted returns directly.
        """
        window = min(20, len(self.returns_history))
        recent_returns = self.returns_history[-window:]

        mean_ret = np.mean(recent_returns)
        std_ret = np.std(recent_returns)

        if std_ret < 1e-8:
            return 0.0

        # Annualized Sharpe (hourly data → * sqrt(8760))
        sharpe = (mean_ret / std_ret) * np.sqrt(window)

        # Clamp reward to [-3, 3] for training stability
        return float(np.clip(sharpe, -3.0, 3.0))

    def _sortino_reward(self) -> float:
        """Sortino Ratio based reward.

        Only penalizes downside volatility.
        """
        window = min(20, len(self.returns_history))
        recent_returns = np.array(self.returns_history[-window:])

        mean_ret = np.mean(recent_returns)

        # Downside deviation
        negative_returns = recent_returns[recent_returns < 0]
        if len(negative_returns) == 0:
            return float(mean_ret * 10)

        downside_std = np.std(negative_returns)
        if downside_std < 1e-8:
            return float(mean_ret * 10)

        sortino = (mean_ret / downside_std) * np.sqrt(window)
        return float(np.clip(sortino, -3.0, 3.0))

    def _get_observation(self) -> np.ndarray:
        """Build observation window."""
        start = max(0, self.current_step - self.window_size + 1)
        end = self.current_step + 1

        obs = self.df.iloc[start:end][self.feature_cols].values.astype(np.float32)

        # Pad if not enough data
        if len(obs) < self.window_size:
            padding = np.zeros((self.window_size - len(obs), len(self.feature_cols)), dtype=np.float32)
            obs = np.vstack([padding, obs])

        return obs

    def _get_info(self) -> dict:
        """Return current portfolio metrics."""
        return {
            "step": self.current_step,
            "equity": self.equity_curve[-1],
            "position": self.position,
            "total_return": (self.equity_curve[-1] / self.initial_balance - 1) * 100,
            "total_trades": self.total_trades,
        }

    def get_portfolio_metrics(self) -> dict:
        """Return comprehensive portfolio metrics."""
        if len(self.equity_curve) < 2:
            return {
                "total_return": 0, "sharpe_ratio": 0, "sortino_ratio": 0,
                "max_drawdown": 0, "win_rate": 0, "total_trades": 0,
                "equity_curve": self.equity_curve,
            }

        equity = np.array(self.equity_curve)
        returns = np.diff(equity) / equity[:-1]

        # Sharpe
        if np.std(returns) > 0:
            sharpe = (np.mean(returns) / np.std(returns)) * np.sqrt(8760)
        else:
            sharpe = 0

        # Sortino
        neg_returns = returns[returns < 0]
        if len(neg_returns) > 0 and np.std(neg_returns) > 0:
            sortino = (np.mean(returns) / np.std(neg_returns)) * np.sqrt(8760)
        else:
            sortino = sharpe  # No downside volatility

        # Max Drawdown
        peak = np.maximum.accumulate(equity)
        drawdown = (peak - equity) / peak * 100
        max_dd = float(np.max(drawdown))

        # Win rate (based on step returns)
        positive_returns = returns[returns > 0]
        win_rate = len(positive_returns) / max(len(returns), 1) * 100

        return {
            "total_return": float((equity[-1] / self.initial_balance - 1) * 100),
            "sharpe_ratio": round(float(sharpe), 4),
            "sortino_ratio": round(float(sortino), 4),
            "max_drawdown": round(max_dd, 2),
            "win_rate": round(win_rate, 2),
            "total_trades": self.total_trades,
            "equity_curve": equity.tolist(),
        }


def sharpe_reward(equity_curve: list, risk_free_rate: float = 0.02) -> float:
    """Standalone Sharpe-based reward function.

    Can be used outside the environment for evaluation.
    """
    if len(equity_curve) < 2:
        return 0.0

    equity = np.array(equity_curve)
    returns = np.diff(equity) / equity[:-1]

    mean_ret = np.mean(returns)
    std_ret = np.std(returns)

    if std_ret < 1e-8:
        return 0.0

    # Hourly returns → annualize with risk-free rate
    hourly_rf = risk_free_rate / 8760
    sharpe = (mean_ret - hourly_rf) / std_ret * np.sqrt(8760)

    return float(sharpe)
