"""
HADES Feature 계산 — features.go와 동일한 27개 feature를 Python으로 구현

Usage:
    from hades_features import compute_features
    features = compute_features(candles_df, funding_rates=[])
    # returns dict of feature_name -> float64

Input:
    candles: list of dicts with keys [timestamp, open, high, low, close, volume]
             오래된 순서로 정렬되어 있어야 함 (최소 200개 필요)
    funding_rates: list of dicts with keys [timestamp, rate] (optional)

Output:
    dict of 27 features matching Go FeaturePipeline exactly
"""

import numpy as np
from typing import List, Dict, Optional


# === EMA / SMA ===

def ema(series: np.ndarray, period: int) -> float:
    if len(series) < period:
        return 0.0
    result = np.mean(series[:period])
    multiplier = 2.0 / (period + 1.0)
    for i in range(period, len(series)):
        result = (series[i] - result) * multiplier + result
    return result


def sma(series: np.ndarray, period: int) -> float:
    if len(series) < period:
        return 0.0
    return float(np.mean(series[-period:]))


# === RSI ===

def rsi(closes: np.ndarray, period: int) -> float:
    if len(closes) < period + 1:
        return 50.0
    changes = np.diff(closes[-(period + 1):])
    gains = np.where(changes > 0, changes, 0.0)
    losses = np.where(changes < 0, -changes, 0.0)
    avg_gain = np.mean(gains)
    avg_loss = np.mean(losses)
    if avg_loss == 0:
        return 100.0
    rs = avg_gain / avg_loss
    return 100.0 - (100.0 / (1.0 + rs))


# === MACD ===

def macd_line(closes: np.ndarray) -> float:
    if len(closes) < 26:
        return 0.0
    return ema(closes, 12) - ema(closes, 26)


def macd_signal(closes: np.ndarray) -> float:
    if len(closes) < 35:
        return 0.0
    macd_series = []
    for i in range(26, len(closes) + 1):
        window = closes[:i]
        macd_series.append(ema(window, 12) - ema(window, 26))
    macd_arr = np.array(macd_series)
    if len(macd_arr) < 9:
        return 0.0
    result = float(np.mean(macd_arr[:9]))
    multiplier = 2.0 / 10.0
    for i in range(9, len(macd_arr)):
        result = (macd_arr[i] - result) * multiplier + result
    return result


# === ATR ===

def atr(candles: List[Dict], period: int) -> float:
    n = len(candles)
    if n < period + 1:
        return 0.0
    total = 0.0
    for i in range(n - period, n):
        h = candles[i]["high"]
        l = candles[i]["low"]
        pc = candles[i - 1]["close"]
        tr = max(h - l, abs(h - pc), abs(l - pc))
        total += tr
    return total / period


# === Bollinger ===

def bollinger(closes: np.ndarray, period: int):
    mid = sma(closes, period)
    if mid == 0 or len(closes) < period:
        return mid, 0.0
    std = float(np.std(closes[-period:]))
    return mid, std


# === ADX ===

def adx(candles: List[Dict], period: int) -> float:
    n = len(candles)
    if n < period * 2 + 1:
        return 0.0

    plus_dm_list, minus_dm_list, tr_list = [], [], []
    for i in range(1, n):
        h, l = candles[i]["high"], candles[i]["low"]
        ph, pl = candles[i - 1]["high"], candles[i - 1]["close"]
        pc = candles[i - 1]["close"]

        tr_val = max(h - l, abs(h - pc), abs(l - pc))
        tr_list.append(tr_val)

        plus_dm = max(h - ph, 0.0)
        minus_dm = max(pl - l, 0.0)
        if plus_dm > minus_dm:
            minus_dm = 0.0
        else:
            plus_dm = 0.0
        plus_dm_list.append(plus_dm)
        minus_dm_list.append(minus_dm)

    if len(tr_list) < period * 2:
        return 0.0

    # Wilder smoothing
    def wilder(series, p):
        s = sum(series[:p]) / p
        result = [s]
        for i in range(p, len(series)):
            s = s - s / p + series[i]
            result.append(s)
        return result

    smooth_tr = wilder(tr_list, period)
    smooth_plus = wilder(plus_dm_list, period)
    smooth_minus = wilder(minus_dm_list, period)

    dx_list = []
    for i in range(len(smooth_tr)):
        if smooth_tr[i] == 0:
            continue
        pdi = (smooth_plus[i] / smooth_tr[i]) * 100
        mdi = (smooth_minus[i] / smooth_tr[i]) * 100
        di_sum = pdi + mdi
        if di_sum == 0:
            continue
        dx = abs(pdi - mdi) / di_sum * 100
        dx_list.append(dx)

    if len(dx_list) < period:
        return 0.0

    adx_vals = wilder(dx_list, period)
    return adx_vals[-1] if adx_vals else 0.0


# === OBV ===

def obv(candles: List[Dict]) -> float:
    if len(candles) < 2:
        return 0.0
    obv = 0.0
    obv_series = [0.0]
    for i in range(1, len(candles)):
        if candles[i]["close"] > candles[i - 1]["close"]:
            obv += candles[i]["volume"]
        elif candles[i]["close"] < candles[i - 1]["close"]:
            obv -= candles[i]["volume"]
        obv_series.append(obv)

    latest = obv_series[-1]
    if len(obv_series) >= 20:
        obv_sma = sum(obv_series[-20:]) / 20.0
        if obv_sma != 0:
            return latest / obv_sma
    return latest


# === Volume Ratio ===

def volume_ratio(candles: List[Dict], period: int = 20) -> float:
    n = len(candles)
    if n < period:
        return 1.0
    current = candles[-1]["volume"]
    avg = sum(c["volume"] for c in candles[n - period:]) / period
    return current / avg if avg > 0 else 1.0


# === Volatility ===

def volatility(candles: List[Dict], periods: int) -> float:
    n = len(candles)
    if n < periods + 1:
        return 0.0
    returns = []
    for i in range(n - periods, n):
        if i > 0:
            ret = (candles[i]["close"] - candles[i - 1]["close"]) / candles[i - 1]["close"]
            returns.append(ret)
    if not returns:
        return 0.0
    return float(np.std(returns)) * 100


# === Momentum ===

def momentum(candles: List[Dict], periods: int) -> float:
    n = len(candles)
    if n < periods + 1:
        return 0.0
    current = candles[-1]["close"]
    past = candles[-1 - periods]["close"]
    if past == 0:
        return 0.0
    return (current - past) / past * 100


# === Regime Detection ===

def detect_regime(candles: List[Dict]) -> Dict[str, float]:
    if len(candles) < 50:
        return {"regime_trending": 0.33, "regime_ranging": 0.34, "regime_volatile": 0.33}

    adx_val = adx(candles, 14)
    vol_val = volatility(candles, 24)

    trending = min(adx_val / 50.0, 1.0) * 0.7
    vol_regime = min(vol_val / 5.0, 1.0) * 0.7
    ranging = 1.0 - trending - vol_regime
    if ranging < 0:
        ranging = 0.0

    total = trending + ranging + vol_regime
    if total > 0:
        trending /= total
        ranging /= total
        vol_regime /= total

    return {"regime_trending": trending, "regime_ranging": ranging, "regime_volatile": vol_regime}


# === Main Feature Computation ===

def compute_features(candles: List[Dict], funding_rates: Optional[List[Dict]] = None) -> Dict[str, float]:
    """
    27개 feature 계산 (features.go FeaturePipeline과 동일)

    Args:
        candles: [{"timestamp", "open", "high", "low", "close", "volume"}, ...]
                 오래된 순서, 최소 200개 필요
        funding_rates: [{"timestamp", "rate"}, ...] (optional)

    Returns:
        dict of feature_name -> float64
    """
    if len(candles) < 200:
        raise ValueError(f"최소 200개 캔들 필요, 현재 {len(candles)}개")

    closes = np.array([c["close"] for c in candles])
    features = {}

    # Technical indicators (15개)
    features["ema_5"] = ema(closes, 5)
    features["ema_20"] = ema(closes, 20)
    features["ema_50"] = ema(closes, 50)
    features["ema_200"] = ema(closes, 200)
    features["rsi_14"] = rsi(closes, 14)
    features["rsi_28"] = rsi(closes, 28)
    features["macd_line"] = macd_line(closes)
    features["macd_signal"] = macd_signal(closes)
    features["macd_histogram"] = features["macd_line"] - features["macd_signal"]
    features["atr_14"] = atr(candles, 14)
    mid, std = bollinger(closes, 20)
    features["bollinger_upper"] = mid + 2 * std
    features["bollinger_lower"] = mid - 2 * std
    features["adx_14"] = adx(candles, 14)
    features["obv"] = obv(candles)
    features["volume_ratio"] = volume_ratio(candles, 20)

    # Microstructure (3개)
    latest_funding = 0.0
    funding_changes = []
    if funding_rates:
        for fr in funding_rates:
            if fr["timestamp"] <= candles[-1]["timestamp"]:
                latest_funding = fr["rate"]
        for i in range(max(0, len(funding_rates) - 3), len(funding_rates) - 1):
            if i + 1 < len(funding_rates):
                funding_changes.append(funding_rates[i + 1]["rate"] - funding_rates[i]["rate"])

    features["funding_rate"] = latest_funding
    features["funding_rate_change_1h"] = funding_changes[-1] if funding_changes else 0.0
    features["funding_rate_change_8h"] = funding_changes[0] if len(funding_changes) > 1 else 0.0

    # Derived (9개)
    features["volatility_1h"] = volatility(candles, 1)
    features["volatility_4h"] = volatility(candles, 4)
    features["volatility_24h"] = volatility(candles, 24)
    features["momentum_1h"] = momentum(candles, 1)
    features["momentum_4h"] = momentum(candles, 4)

    ema20 = features["ema_20"]
    if ema20 > 0:
        features["mean_reversion_score"] = (closes[-1] - ema20) / ema20 * 100
    else:
        features["mean_reversion_score"] = 0.0

    regime = detect_regime(candles)
    features.update(regime)

    return features
