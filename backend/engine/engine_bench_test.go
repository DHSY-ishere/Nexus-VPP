package engine

import "testing"

// One full market interval for the default community (4 zones x 10 homes):
// bids, clearing, network gate, dispatch, meters, escrow, attribution, Merkle root.
func BenchmarkStepDefault(b *testing.B) {
	cfg := DefaultConfig()
	cfg.StartHour = 9
	e := New(cfg, nil, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Step()
	}
}

// Scaling: same interval with more zones and homes per zone.
func BenchmarkStepScaling(b *testing.B) {
	for _, c := range []struct {
		name         string
		zones, buses int
	}{{"4x10", 4, 10}, {"16x10", 16, 10}, {"16x50", 16, 50}} {
		b.Run(c.name, func(b *testing.B) {
			cfg := DefaultConfig()
			cfg.Zones, cfg.BusesPerZone = c.zones, c.buses
			e := New(cfg, nil, nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e.Step()
			}
		})
	}
}

// Same interval with the blockchain off: the cost of consensus is the difference.
func BenchmarkStepNoChain(b *testing.B) {
	cfg := DefaultConfig()
	cfg.Chain = false
	e := New(cfg, nil, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Step()
	}
}

// A full outside audit of a day of blocks (288 blocks, every settlement re-executed).
func BenchmarkAuditDay(b *testing.B) {
	cfg := DefaultConfig()
	cfg.StartHour = 0
	e := New(cfg, nil, nil)
	for i := 0; i < 288; i++ {
		e.Step()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.Chain().Verify(); err != nil {
			b.Fatal(err)
		}
	}
}
