package models

import (
	"testing"
	"time"
)

// A block's hash must survive being stored in PostgreSQL and read back: the database keeps
// microseconds and returns another time zone, neither of which may change the hash.
func TestBlockHashSurvivesADatabaseRoundTrip(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	trade := P2PTrade{
		TradeID: "TRD-1", ContributorID: "node_7", EnergyProvidedKw: 3.14159265, CreditEarned: 2.6, GridFrequency: 49.2,
		SettledAt: time.Date(2026, 11, 20, 15, 25, 23, 123456789, ist), // nanoseconds, local zone
	}
	MineBlock(&trade, "GENESIS")
	if trade.BlockHash[:2] != "00" || trade.BlockHash != ComputeBlockHash(trade) {
		t.Fatal("freshly mined block must verify")
	}

	stored := trade
	stored.SettledAt = trade.SettledAt.Truncate(time.Microsecond).UTC() // what PostgreSQL hands back
	if ComputeBlockHash(stored) != trade.BlockHash {
		t.Fatal("hash changed after a database round trip")
	}
}

func TestTamperingChangesTheHash(t *testing.T) {
	trade := P2PTrade{TradeID: "TRD-2", ContributorID: "n", EnergyProvidedKw: 1, CreditEarned: 0.5, GridFrequency: 49.5, SettledAt: time.Now()}
	MineBlock(&trade, "GENESIS")
	tampered := trade
	tampered.CreditEarned = 50
	if ComputeBlockHash(tampered) == trade.BlockHash {
		t.Fatal("editing the credit must invalidate the block")
	}
}
