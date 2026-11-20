import pytest

import forecast as fc
import processor
import tariff


def telemetry(freq=50.0, soc=80.0, repl=4.0, cons=1.0, ts="2026-06-01T12:00:00+00:00", node="n1"):
    return {"node_id": node, "local_frequency_hz": freq, "state_of_charge_percent": soc,
            "replenishment_rate_kw": repl, "current_consumption_kw": cons, "timestamp": ts}


def fixed_forecast(solar_fraction, k=0.5):
    return lambda hour, hist: fc.Forecast(k, solar_fraction, hist[-1] if hist else 0, "test")


def warm(arb, node="n1", repl=4.0):
    # Establish the node's capacity and history around solar noon.
    for _ in range(5):
        arb.decide(telemetry(repl=repl, node=node))


def test_no_dispatch_when_grid_normal_and_solar_steady():
    arb = processor.Arbiter(fixed_forecast(solar_fraction=1.0, k=1.0))
    warm(arb)
    assert arb.decide(telemetry()) is None


def test_reactive_dispatch_is_priced_by_the_dynamic_tariff():
    arb = processor.Arbiter(fixed_forecast(1.0, 1.0))
    warm(arb)
    mild = arb.decide(telemetry(freq=49.6))
    severe = arb.decide(telemetry(freq=49.0))
    assert mild["trigger"] == severe["trigger"] == "reactive"
    assert mild["price_kw"] == pytest.approx(tariff.price(49.6))
    assert severe["price_kw"] == pytest.approx(0.85)
    assert mild["price_kw"] < severe["price_kw"]
    assert severe["credit"] == pytest.approx(severe["energy_kw"] * 0.85)


def test_reactive_requires_enough_battery():
    arb = processor.Arbiter(fixed_forecast(1.0, 1.0))
    warm(arb)
    assert arb.decide(telemetry(freq=49.0, soc=20.0)) is None


def test_forecast_trigger_fires_before_frequency_falls():
    # Normal frequency, but the model expects solar to fall by 70%.
    arb = processor.Arbiter(fixed_forecast(solar_fraction=0.0, k=0.1))
    warm(arb)
    cmd = arb.decide(telemetry(freq=50.0))
    assert cmd is not None and cmd["trigger"] == "forecast"
    assert cmd["price_kw"] == pytest.approx(tariff.MIN)  # no strain yet, so the floor tariff


def test_forecast_trigger_ignores_small_drops():
    arb = processor.Arbiter(fixed_forecast(solar_fraction=0.8, k=0.8))
    warm(arb)
    assert arb.decide(telemetry(freq=50.0)) is None


def test_reactive_plans_with_forecast_not_current_solar():
    bright = processor.Arbiter(fixed_forecast(1.0, 1.0))
    dim = processor.Arbiter(fixed_forecast(0.0, 0.0))
    for a in (bright, dim):
        warm(a)
    e_bright = bright.decide(telemetry(freq=49.4, repl=4.0, cons=1.0))["energy_kw"]
    e_dim = dim.decide(telemetry(freq=49.4, repl=4.0, cons=1.0))["energy_kw"]
    assert e_dim < e_bright


def test_no_history_at_night_means_no_forecast_dispatch():
    arb = processor.Arbiter(fixed_forecast(0.0, 0.0))
    night = telemetry(ts="2026-06-01T02:00:00+00:00", repl=0.0)
    assert arb.decide(night) is None


def test_with_the_real_trained_model_a_clear_day_stays_quiet():
    model, _ = fc.train(train_days=60, val_days=10, test_days=10, seed=5, max_iter=100, save=False)
    arb = processor.Arbiter(fc.Forecaster(model).predict)
    cmds = [arb.decide(telemetry(freq=50.0, repl=4.0 * float(fc.clear_sky(12.0 + i * 5 / 60)),
                                  ts=f"2026-06-01T{12 + (i * 5) // 60:02d}:{(i * 5) % 60:02d}:00+00:00"))
            for i in range(10)]
    assert all(c is None for c in cmds)
