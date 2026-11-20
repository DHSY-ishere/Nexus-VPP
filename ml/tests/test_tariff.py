import json
from pathlib import Path

import tariff

VECTORS = json.loads((Path(__file__).parent.parent / "tariff_vectors.json").read_text())


def test_constants_match_shared_vectors():
    assert (tariff.MIN, tariff.MAX, tariff.TRIGGER_HZ, tariff.FLOOR_HZ) == (
        VECTORS["min"], VECTORS["max"], VECTORS["trigger_hz"], VECTORS["floor_hz"])


def test_matches_go_vectors():
    for c in VECTORS["cases"]:
        assert abs(tariff.price(c["freq"]) - c["price"]) < 1e-9, c


def test_bounded_and_monotonic():
    prev = -1.0
    f = 50.5
    while f >= 47.0:
        p = tariff.price(f)
        assert tariff.MIN - 1e-12 <= p <= tariff.MAX + 1e-12
        assert p >= prev - 1e-12
        prev = p
        f -= 0.01


def test_credit():
    assert abs(tariff.credit(10, 49.0) - 8.5) < 1e-9
