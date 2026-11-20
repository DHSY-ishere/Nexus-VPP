"""Python client for the forecast gRPC service (used by the arbiter when FORECAST_MODE=grpc)."""

from __future__ import annotations

import grpc

from forecast import Forecast
from forecastpb import forecast_pb2 as pb
from forecastpb import forecast_pb2_grpc as rpc


class GrpcForecaster:
    def __init__(self, target: str, timeout: float = 2.0):
        self.channel = grpc.insecure_channel(target)
        self.stub = rpc.ForecasterStub(self.channel)
        self.timeout = timeout

    def predict(self, hour: float, history: list[float], node_id: str = "") -> Forecast:
        r = self.stub.Forecast(
            pb.ForecastRequest(node_id=node_id, hour_of_day=hour, clear_sky_index_history=history),
            timeout=self.timeout,
        )
        return Forecast(r.clear_sky_index_15m, r.solar_fraction_15m, r.persistence_index_15m, r.model_version)

    def health(self):
        return self.stub.Health(pb.HealthRequest(), timeout=self.timeout)

    def close(self):
        self.channel.close()
