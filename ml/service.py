"""FastAPI front for the same forecaster: REST for humans and dashboards, gRPC for services.

    GET  /health     liveness and model version
    GET  /metrics    held-out evaluation of the loaded model (written by `forecast.py train`)
    POST /forecast   {"hour_of_day": 13.5, "clear_sky_index_history": [..]} -> 15-minute forecast
"""

from __future__ import annotations

import json

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from forecast import METRICS_PATH, MODEL_VERSION, Forecaster


class ForecastIn(BaseModel):
    node_id: str = ""
    hour_of_day: float = Field(ge=0, le=24)
    clear_sky_index_history: list[float] = Field(min_length=1, max_length=64)


class ForecastOut(BaseModel):
    clear_sky_index_15m: float
    solar_fraction_15m: float
    persistence_index_15m: float
    model_version: str


def create_app(forecaster: Forecaster) -> FastAPI:
    app = FastAPI(title="Nexus forecast service", version=MODEL_VERSION)

    @app.get("/health")
    def health():
        return {"ready": True, "model_version": MODEL_VERSION}

    @app.get("/metrics")
    def metrics():
        if not METRICS_PATH.exists():
            raise HTTPException(404, "no metrics file; run `python forecast.py train`")
        return json.loads(METRICS_PATH.read_text())

    @app.post("/forecast", response_model=ForecastOut)
    def forecast(req: ForecastIn):
        f = forecaster.predict(req.hour_of_day, req.clear_sky_index_history)
        return ForecastOut(
            clear_sky_index_15m=f.k_hat, solar_fraction_15m=f.solar_fraction,
            persistence_index_15m=f.persistence_k, model_version=f.model_version,
        )

    return app
