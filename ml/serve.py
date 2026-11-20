"""Run the forecast service: gRPC on :50051 and FastAPI on :8000 over one loaded model.

    python serve.py            # trains first if models/forecaster.joblib is missing
Env: GRPC_ADDR (default [::]:50051), HTTP_PORT (default 8000).
"""

import os

import uvicorn

import forecast
from grpc_server import serve
from service import create_app


def main():
    if not forecast.MODEL_PATH.exists():
        print("No trained model found; training one (about 5 seconds)...")
        forecast.train()
    model = forecast.Forecaster.load()
    grpc_server = serve(model, os.environ.get("GRPC_ADDR", "[::]:50051"))
    print(f"gRPC forecaster on port {grpc_server.bound_port}")
    uvicorn.run(create_app(model), host="0.0.0.0", port=int(os.environ.get("HTTP_PORT", "8000")))


if __name__ == "__main__":
    main()
