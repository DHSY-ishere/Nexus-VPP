"""gRPC server for the trained forecaster (contract: proto/forecast.proto)."""

from __future__ import annotations

from concurrent import futures

import grpc

from forecast import MODEL_VERSION, Forecaster
from forecastpb import forecast_pb2 as pb
from forecastpb import forecast_pb2_grpc as rpc


class ForecasterService(rpc.ForecasterServicer):
    def __init__(self, forecaster: Forecaster):
        self.forecaster = forecaster

    def Forecast(self, request, context):
        if not request.clear_sky_index_history:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "clear_sky_index_history is empty")
        if not 0.0 <= request.hour_of_day <= 24.0:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "hour_of_day must be within 0..24")
        f = self.forecaster.predict(request.hour_of_day, list(request.clear_sky_index_history))
        return pb.ForecastResponse(
            clear_sky_index_15m=f.k_hat,
            solar_fraction_15m=f.solar_fraction,
            persistence_index_15m=f.persistence_k,
            model_version=f.model_version,
        )

    def Health(self, request, context):
        return pb.HealthResponse(ready=True, model_version=MODEL_VERSION)


def serve(forecaster: Forecaster, address: str = "[::]:50051", workers: int = 4) -> grpc.Server:
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=workers))
    rpc.add_ForecasterServicer_to_server(ForecasterService(forecaster), server)
    port = server.add_insecure_port(address)
    if port == 0:
        raise RuntimeError(f"could not bind {address}")
    server.start()
    server.bound_port = port  # useful when address uses port 0
    return server
