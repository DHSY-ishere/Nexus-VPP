"""Nexus arbiter: reads node telemetry from Kafka and decides frequency-response dispatch.

Two triggers, both priced by the dynamic stability tariff (tariff.py, $0.15-$0.85/kW):

  reactive   grid frequency is below 49.8 Hz and the node's battery is above a
             time-of-day threshold.
  forecast   frequency is still normal, but the trained forecaster (forecast.py) expects
             this node's solar output to fall sharply within 15 minutes, so the node is
             asked to discharge before the shortfall reaches the grid.

The decision logic lives in Arbiter, which has no Kafka dependency and is unit tested.
The Kafka loop at the bottom only moves messages.

Forecast source: FORECAST_MODE=local (default, loads models/forecaster.joblib) or
FORECAST_MODE=grpc (calls the forecast service at FORECAST_GRPC, default 127.0.0.1:50051).
"""

from __future__ import annotations

import json
import os
import uuid
from collections import defaultdict, deque
from datetime import datetime, timezone
from typing import Callable

import tariff
from forecast import HISTORY, Forecast, Forecaster, clear_sky

FORECAST_DROP_TRIGGER = 0.35  # forecast solar fall (fraction of current) that triggers pre-emptive dispatch
MIN_SOLAR_KW = 0.5  # ignore nodes with negligible solar right now
PREEMPT_SHARE = 0.5  # pre-emptive dispatch uses half the reactive battery contribution

ForecastFn = Callable[[float, list], Forecast]


def parse_hour(ts: str | None) -> float:
    """UTC hour of day from an ISO timestamp; falls back to the wall clock."""
    try:
        d = datetime.fromisoformat(ts.replace("Z", "+00:00"))  # type: ignore[union-attr]
    except Exception:
        d = datetime.now(timezone.utc)
    return d.hour + d.minute / 60.0


def provisioning(hour: float):
    """(battery threshold %, discharge multiplier) for the time of day."""
    if 10 <= hour < 16:  # peak solar replenishment
        return 40.0, 0.15
    if not (6 <= hour < 18):  # night: conserve the battery
        return 60.0, 0.05
    return 50.0, 0.10


class Arbiter:
    def __init__(self, forecast_fn: ForecastFn):
        self.forecast_fn = forecast_fn
        self.k_history: dict[str, deque] = defaultdict(lambda: deque(maxlen=HISTORY))
        self.kwp_est: dict[str, float] = {}

    def observe(self, node: str, hour: float, solar_kw: float) -> float | None:
        """Update this node's clear-sky-index history from its current solar output.

        Installed capacity is not in the telemetry, so it is estimated as the largest
        solar_kw / clear_sky(hour) seen near solar noon. Returns the index, or None at night."""
        cs = float(clear_sky(hour))
        if cs < 0.15:
            return None
        ratio = solar_kw / cs
        if cs > 0.5:
            self.kwp_est[node] = max(self.kwp_est.get(node, 0.0), ratio)
        kwp = self.kwp_est.get(node, ratio)
        if kwp <= 0:
            return None
        k = max(0.0, min(1.0, solar_kw / (kwp * cs)))
        self.k_history[node].append(k)
        return k

    def decide(self, data: dict) -> dict | None:
        node = data.get("node_id", "unknown")
        freq = float(data.get("local_frequency_hz", 50.0))
        battery = float(data.get("state_of_charge_percent", 0.0))
        consumption = float(data.get("current_consumption_kw", 0.0))
        replenishment = float(data.get("replenishment_rate_kw", 0.0))
        hour = parse_hour(data.get("timestamp"))

        self.observe(node, hour, replenishment)
        hist = list(self.k_history[node])
        fc = self.forecast_fn(hour, hist) if hist else None
        threshold, multiplier = provisioning(hour)
        contribution = battery * multiplier
        kwp = self.kwp_est.get(node, 0.0)
        expected_repl = kwp * fc.solar_fraction if fc else replenishment

        # Reactive: the grid is already under strain. Plan with the *forecast* solar output
        # so a node about to lose its sun does not over-promise.
        if freq < tariff.TRIGGER_HZ and battery > threshold:
            net = min(replenishment, expected_repl) - consumption
            energy = contribution + net * 0.8 if net > 0 else max(1.0, contribution + net)
            return self._trade(node, energy, freq, "reactive", fc)

        # Forecast: normal frequency, but the sun is about to go.
        if (
            fc is not None and len(hist) >= 3 and replenishment >= MIN_SOLAR_KW
            and battery > threshold + 10
        ):
            drop = (replenishment - expected_repl) / replenishment
            if drop >= FORECAST_DROP_TRIGGER:
                return self._trade(node, max(0.5, contribution * PREEMPT_SHARE), freq, "forecast", fc)
        return None

    @staticmethod
    def _trade(node: str, energy: float, freq: float, trigger: str, fc: Forecast | None) -> dict:
        return {
            "type": "EXECUTE_TRADE",
            "trade_id": str(uuid.uuid4()),
            "contributor_id": node,
            "energy_kw": energy,
            "price_kw": tariff.price(freq),
            "credit": tariff.credit(energy, freq),
            "grid_frequency": freq,
            "trigger": trigger,
            "forecast_k_15m": fc.k_hat if fc else None,
            "timestamp": datetime.now(timezone.utc).isoformat(),
        }


def make_forecast_fn() -> ForecastFn:
    if os.environ.get("FORECAST_MODE", "local") == "grpc":
        from grpc_client import GrpcForecaster

        client = GrpcForecaster(os.environ.get("FORECAST_GRPC", "127.0.0.1:50051"))
        return lambda hour, hist: client.predict(hour, hist)
    f = Forecaster.load()
    return f.predict


def main():
    from confluent_kafka import Consumer, Producer

    brokers = os.environ.get("KAFKA_BROKERS", "127.0.0.1:9092")
    consumer = Consumer({"bootstrap.servers": brokers, "group.id": "ml-logic", "auto.offset.reset": "latest"})
    producer = Producer({"bootstrap.servers": brokers})
    consumer.subscribe(["nexus.telemetry.raw"])
    arbiter = Arbiter(make_forecast_fn())

    print(f"Arbiter running (forecast mode: {os.environ.get('FORECAST_MODE', 'local')}, brokers: {brokers})")
    while True:
        msg = consumer.poll(1.0)
        if msg is None or msg.error():
            continue
        cmd = arbiter.decide(json.loads(msg.value().decode("utf-8")))
        if cmd:
            producer.produce("nexus.commands", json.dumps(cmd).encode("utf-8"))
            producer.flush()
            print(f"{cmd['trigger']} dispatch for {cmd['contributor_id']}: {cmd['energy_kw']:.2f} kW at ${cmd['price_kw']:.3f}/kW")


if __name__ == "__main__":
    main()
