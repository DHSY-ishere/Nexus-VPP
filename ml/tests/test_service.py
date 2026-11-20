import grpc
import pytest
from fastapi.testclient import TestClient

import forecast as fc
from grpc_client import GrpcForecaster
from grpc_server import serve
from service import create_app


@pytest.fixture(scope="module")
def forecaster():
    model, _ = fc.train(train_days=40, val_days=10, test_days=10, seed=2, max_iter=60, save=False)
    return fc.Forecaster(model)


@pytest.fixture(scope="module")
def grpc_target(forecaster):
    server = serve(forecaster, "127.0.0.1:0")
    yield f"127.0.0.1:{server.bound_port}"
    server.stop(0)


def test_grpc_forecast_matches_local_model(forecaster, grpc_target):
    client = GrpcForecaster(grpc_target)
    hist = [0.95, 0.9, 0.8, 0.7, 0.6]
    remote, local = client.predict(13.0, hist), forecaster.predict(13.0, hist)
    assert remote.k_hat == pytest.approx(local.k_hat)
    assert remote.solar_fraction == pytest.approx(local.solar_fraction)
    assert remote.persistence_k == pytest.approx(0.6)
    assert client.health().ready


def test_grpc_rejects_bad_requests(grpc_target):
    client = GrpcForecaster(grpc_target)
    with pytest.raises(grpc.RpcError) as e:
        client.predict(13.0, [])
    assert e.value.code() == grpc.StatusCode.INVALID_ARGUMENT
    with pytest.raises(grpc.RpcError) as e:
        client.predict(30.0, [0.5])
    assert e.value.code() == grpc.StatusCode.INVALID_ARGUMENT


def test_rest_forecast_and_validation(forecaster):
    http = TestClient(create_app(forecaster))
    assert http.get("/health").json()["ready"] is True
    r = http.post("/forecast", json={"hour_of_day": 13.0, "clear_sky_index_history": [0.9, 0.8, 0.7]})
    assert r.status_code == 200 and 0 <= r.json()["clear_sky_index_15m"] <= 1
    assert http.post("/forecast", json={"hour_of_day": 99, "clear_sky_index_history": [0.5]}).status_code == 422
    assert http.post("/forecast", json={"hour_of_day": 12, "clear_sky_index_history": []}).status_code == 422
