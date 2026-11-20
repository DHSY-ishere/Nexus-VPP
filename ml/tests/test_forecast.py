import numpy as np
import pytest

import forecast as fc


@pytest.fixture(scope="module")
def small_model():
    # Small but real: trains from scratch on simulated days, never touches the saved model.
    model, metrics = fc.train(train_days=80, val_days=20, test_days=40, seed=11, max_iter=120, save=False)
    return model, metrics


def test_clear_sky_matches_engine_curve():
    assert fc.clear_sky(6.0) == 0 and fc.clear_sky(18.0) < 1e-12 and fc.clear_sky(3.0) == 0
    assert abs(float(fc.clear_sky(12.0)) - 1.0) < 1e-12


def test_dataset_is_reproducible_and_daytime_only():
    X1, y1, h1, _ = fc.make_dataset(5, seed=3)
    X2, y2, _, _ = fc.make_dataset(5, seed=3)
    assert np.array_equal(X1, X2) and np.array_equal(y1, y2)
    assert X1.shape[1] == len(fc.FEATURE_NAMES)
    assert h1.min() >= 6 and h1.max() <= 18
    assert ((y1 >= 0.05) & (y1 <= 1.0)).all()


def test_model_beats_persistence_on_unseen_days(small_model):
    _, m = small_model
    r = m["rmse_solar_per_kwp"]
    assert r["model"] < r["persistence"] < r["clear_sky"]
    assert m["skill_vs_persistence"] > 0
    # The gain comes from fronts arriving or clearing, where persistence cannot help.
    assert m["skill_vs_persistence_hard_cases"] > m["skill_vs_persistence"]


def test_predict_is_bounded_and_pads_short_history(small_model):
    f = fc.Forecaster(small_model[0])
    p = f.predict(12.0, [0.9])  # one reading only
    assert 0.0 <= p.k_hat <= 1.0 and 0.0 <= p.solar_fraction <= 1.0
    assert p.persistence_k == 0.9
    with pytest.raises(ValueError):
        f.predict(12.0, [])


def test_forecast_anticipates_a_falling_cloud_index(small_model):
    f = fc.Forecaster(small_model[0])
    falling = f.predict(14.0, [0.97, 0.95, 0.9, 0.8, 0.65, 0.5, 0.4])
    steady = f.predict(14.0, [0.97] * 7)
    assert falling.k_hat < steady.k_hat
