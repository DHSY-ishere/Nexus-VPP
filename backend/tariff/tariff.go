// Package tariff is the dynamic stability tariff for frequency-response dispatch.
//
// The legacy dispatch path used a flat $0.15 per kW. Here the price rises linearly
// with grid strain: $0.15 at the 49.8 Hz trigger (where dispatch starts) up to $0.85
// at 49.0 Hz and below. Price discovery for the P2P market itself comes from the
// auction's clearing price (package market); this tariff prices only the
// frequency-response energy that nodes are asked to inject when the grid is under strain.
//
// ml/tariff.py implements the same formula; both are tested against ml/tariff_vectors.json.
package tariff

const (
	Min       = 0.15 // $/kW at (or above) the trigger frequency
	Max       = 0.85 // $/kW at (or below) the floor frequency
	TriggerHz = 49.8 // dispatch starts below this
	FloorHz   = 49.0 // price is capped at and below this
)

// Strain is 0 at the trigger frequency and 1 at the floor, linear in between.
func Strain(freqHz float64) float64 {
	s := (TriggerHz - freqHz) / (TriggerHz - FloorHz)
	if s < 0 {
		return 0
	}
	if s > 1 {
		return 1
	}
	return s
}

// Price is the $/kW paid for frequency-response energy at the given grid frequency.
func Price(freqHz float64) float64 { return Min + (Max-Min)*Strain(freqHz) }

// Credit is the payment for energyKW delivered at the given grid frequency.
func Credit(energyKW, freqHz float64) float64 { return energyKW * Price(freqHz) }
