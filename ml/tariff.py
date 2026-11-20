"""Dynamic stability tariff. Mirrors backend/tariff/tariff.go; tested against tariff_vectors.json.

$0.15/kW at (or above) the 49.8 Hz trigger, rising linearly to $0.85/kW at 49.0 Hz and below.
"""

MIN = 0.15
MAX = 0.85
TRIGGER_HZ = 49.8
FLOOR_HZ = 49.0


def strain(freq_hz: float) -> float:
    s = (TRIGGER_HZ - freq_hz) / (TRIGGER_HZ - FLOOR_HZ)
    return max(0.0, min(1.0, s))


def price(freq_hz: float) -> float:
    return MIN + (MAX - MIN) * strain(freq_hz)


def credit(energy_kw: float, freq_hz: float) -> float:
    return energy_kw * price(freq_hz)
