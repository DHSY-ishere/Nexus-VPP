"""15-minute-ahead solar forecasting for the Nexus arbiter.

What it predicts. The engine models a home's solar output as
    kWp * sin(pi * (hour - 6) / 12) * k
where k is the clear-sky index (1 = clear, ~0.15 = heavy cloud). The model predicts k
fifteen minutes (three 5-minute steps) ahead from the recent history of k and the time
of day, and the forecast solar fraction is k_hat * clear_sky(hour + 0.25).

What it is. A gradient-boosted regressor (scikit-learn HistGradientBoostingRegressor),
trained here, not a rule. It replaces the arbiter's fixed thresholds with a forecast.

What it is NOT. It is trained on SYNTHETIC cloud data (a regime-switching cloud process
with afternoon build-up, see simulate_clouds). Its skill over persistence on held-out
synthetic days says the learning pipeline works; it is not evidence about real weather.
Training on real irradiance data (for example a pyranometer or satellite-derived series)
is the next step and uses the same code path.

    python forecast.py train        # trains, evaluates on held-out days, writes models/
"""

from __future__ import annotations

import json
import math
import sys
from dataclasses import dataclass
from pathlib import Path

import joblib
import numpy as np
from sklearn.ensemble import HistGradientBoostingRegressor

STEP_MIN = 5
HORIZON_STEPS = 3  # 15 minutes
HISTORY = 13  # k_t ... k_{t-12}
MODEL_DIR = Path(__file__).parent / "models"
MODEL_PATH = MODEL_DIR / "forecaster.joblib"
METRICS_PATH = MODEL_DIR / "metrics.json"
MODEL_VERSION = "hgb-clear-sky-index-v1"

FEATURE_NAMES = [
    "hour_sin", "hour_cos", "cs_now", "cs_future",
    "k0", "k1", "k2", "k3", "k6", "k12",
    "d1", "d3", "std6", "mean12", "min6", "max6",
]


# --------------------------------------------------------------------------- physics
def clear_sky(hour):
    """Per-kWp solar output with no cloud; identical to the engine's curve."""
    hour = np.asarray(hour, dtype=float)
    out = np.sin(np.pi * (hour - 6.0) / 12.0)
    return np.where((hour < 6.0) | (hour > 18.0), 0.0, np.clip(out, 0.0, None))


def _gauss(x, mu, s):
    return math.exp(-((x - mu) ** 2) / (2 * s * s))


# --------------------------------------------------------------------------- data
DAY_TYPES = {"clear": 0.002, "partly": 0.02, "overcast": 0.05}  # per-step front probability


def simulate_clouds(rng: np.random.Generator, day_type: str | None = None) -> np.ndarray:
    """One day of clear-sky index at 5-minute steps (288 values).

    A cloud front arrives with a probability that grows through the afternoon (convective
    build-up) and depends on the day type; it lasts a random time and then clears. The
    index ramps toward its target over a few steps and carries small measurement noise.
    """
    n = 24 * 60 // STEP_MIN
    day_type = day_type or rng.choice(list(DAY_TYPES), p=[0.3, 0.45, 0.25])
    base_p = DAY_TYPES[day_type]
    k = np.empty(n)
    level, target, cloudy = 0.97, 0.97, False
    mean_dur = rng.uniform(5, 18)  # steps
    for i in range(n):
        hour = i * STEP_MIN / 60.0
        if not cloudy and rng.random() < base_p * (1 + 1.5 * _gauss(hour, 14.0, 2.5)):
            cloudy, target = True, rng.uniform(0.15, 0.6)
        elif cloudy and rng.random() < 1.0 / mean_dur:
            cloudy, target = False, rng.uniform(0.92, 1.0)
            mean_dur = rng.uniform(5, 18)
        level += 0.45 * (target - level)
        k[i] = level + rng.normal(0, 0.025)
    return np.clip(k, 0.05, 1.0)


def _features(hour, lags):
    """hour: (n,), lags: (n, HISTORY) with lags[:, 0] = k_t (newest) ... lags[:, 12] = k_{t-12}."""
    hour = np.asarray(hour, dtype=float)
    lags = np.asarray(lags, dtype=float)
    w = 2 * np.pi * hour / 24.0
    cs_now = clear_sky(hour)
    cs_future = clear_sky(hour + HORIZON_STEPS * STEP_MIN / 60.0)
    k = lambda i: lags[:, i]
    recent = lags[:, :6]
    return np.column_stack([
        np.sin(w), np.cos(w), cs_now, cs_future,
        k(0), k(1), k(2), k(3), k(6), k(12),
        k(0) - k(1), k(0) - k(3),
        recent.std(axis=1), lags[:, :12].mean(axis=1), recent.min(axis=1), recent.max(axis=1),
    ])


def make_dataset(days: int, seed: int):
    """Daytime samples from `days` simulated days: features, target k_{t+3}, and context."""
    rng = np.random.default_rng(seed)
    X, y, hours, k_now = [], [], [], []
    for _ in range(days):
        k = simulate_clouds(rng)
        for t in range(HISTORY - 1, len(k) - HORIZON_STEPS):
            hour = t * STEP_MIN / 60.0
            if clear_sky(hour + HORIZON_STEPS * STEP_MIN / 60.0) < 0.05 or clear_sky(hour) < 0.05:
                continue
            lags = k[t - HISTORY + 1: t + 1][::-1]
            X.append((hour, lags))
            y.append(k[t + HORIZON_STEPS])
            hours.append(hour)
            k_now.append(k[t])
    hours = np.array(hours)
    lags = np.array([x[1] for x in X])
    return _features(hours, lags), np.array(y), hours, np.array(k_now)


# --------------------------------------------------------------------------- model
@dataclass
class Forecast:
    k_hat: float  # predicted clear-sky index in 15 minutes
    solar_fraction: float  # predicted solar output as a fraction of installed kWp
    persistence_k: float  # naive forecast: the index stays as it is now
    model_version: str


class Forecaster:
    def __init__(self, model):
        self.model = model

    @classmethod
    def load(cls, path: Path = MODEL_PATH) -> "Forecaster":
        return cls(joblib.load(path))

    def predict(self, hour: float, history: list[float]) -> Forecast:
        """history: clear-sky index readings, oldest first, newest last (5-minute steps).
        Shorter histories are padded by repeating the oldest reading."""
        if not history:
            raise ValueError("history must contain at least one reading")
        h = [float(min(1.0, max(0.0, v))) for v in history][-HISTORY:]
        h = [h[0]] * (HISTORY - len(h)) + h
        lags = np.array(h[::-1])[None, :]
        x = _features(np.array([hour]), lags)
        k_hat = float(np.clip(self.model.predict(x)[0], 0.0, 1.0))
        cs = float(clear_sky(hour + HORIZON_STEPS * STEP_MIN / 60.0))
        return Forecast(k_hat, k_hat * cs, h[-1], MODEL_VERSION)


# --------------------------------------------------------------------------- training
def _rmse(a, b):
    return float(np.sqrt(np.mean((np.asarray(a) - np.asarray(b)) ** 2)))


def _mae(a, b):
    return float(np.mean(np.abs(np.asarray(a) - np.asarray(b))))


def train(train_days=300, val_days=60, test_days=100, seed=7, max_iter=300, save=True):
    Xtr, ytr, _, _ = make_dataset(train_days, seed)
    Xva, yva, _, _ = make_dataset(val_days, seed + 1)
    model = HistGradientBoostingRegressor(
        max_iter=max_iter, learning_rate=0.06, max_depth=6, min_samples_leaf=40,
        l2_regularization=1.0, early_stopping=False, random_state=seed,
    )
    model.fit(Xtr, ytr)

    Xte, yte, hte, know = make_dataset(test_days, seed + 2)  # days never seen in training
    cs_f = Xte[:, FEATURE_NAMES.index("cs_future")]
    k_pred = np.clip(model.predict(Xte), 0, 1)
    solar_true, solar_model = yte * cs_f, k_pred * cs_f
    solar_persist, solar_clear = know * cs_f, 1.0 * cs_f

    hard = np.abs(yte - know) > 0.30  # a front arrives or clears within the horizon
    metrics = {
        "model_version": MODEL_VERSION,
        "data": "synthetic regime-switching cloud process (see forecast.py); not real weather",
        "horizon_minutes": HORIZON_STEPS * STEP_MIN,
        "days": {"train": train_days, "val": val_days, "test": test_days},
        "samples": {"train": int(len(ytr)), "test": int(len(yte)), "test_hard": int(hard.sum())},
        "rmse_solar_per_kwp": {
            "model": _rmse(solar_true, solar_model),
            "persistence": _rmse(solar_true, solar_persist),
            "clear_sky": _rmse(solar_true, solar_clear),
        },
        "mae_solar_per_kwp": {
            "model": _mae(solar_true, solar_model),
            "persistence": _mae(solar_true, solar_persist),
        },
        "rmse_solar_per_kwp_hard_cases": {
            "model": _rmse(solar_true[hard], solar_model[hard]),
            "persistence": _rmse(solar_true[hard], solar_persist[hard]),
        },
        "val_rmse_k": _rmse(yva, np.clip(model.predict(Xva), 0, 1)),
    }
    m = metrics["rmse_solar_per_kwp"]
    metrics["skill_vs_persistence"] = 1.0 - m["model"] / m["persistence"]
    h = metrics["rmse_solar_per_kwp_hard_cases"]
    metrics["skill_vs_persistence_hard_cases"] = 1.0 - h["model"] / h["persistence"]

    if save:
        MODEL_DIR.mkdir(exist_ok=True)
        joblib.dump(model, MODEL_PATH)
        METRICS_PATH.write_text(json.dumps(metrics, indent=2))
    return model, metrics


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "train":
        _, m = train()
        print(json.dumps(m, indent=2))
    else:
        print(__doc__)
