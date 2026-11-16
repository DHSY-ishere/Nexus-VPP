package models

import (
	"time"

	"gorm.io/gorm"
)

// MarketTrade is one settled (or disputed) trade from the grid-aware market. It
// replaces P2PTrade for new trades: energy is in kWh per delivery interval, the
// trade has a buyer, a zone and a gate result, and integrity comes from signed
// meter readings plus a Merkle root per interval instead of proof-of-work.
type MarketTrade struct {
	gorm.Model
	TradeID         string    `gorm:"uniqueIndex"`
	IntervalStart   time.Time `gorm:"index"`
	ZoneID          string    `gorm:"index"`
	SellerID        string    `gorm:"index"`
	BuyerID         string    `gorm:"index"`
	EnergyKWh       float64
	ClearingPrice   float64
	LossFactor      float64
	GateResult      string // PASS | REROUTED | CURTAILED
	MeasuredKWh     float64
	StabilityCredit float64
	Levy            float64
	Status          string // SETTLED | DISPUTED
	LeafHash        string
	MerkleRoot      string `gorm:"index"`
}

// IntervalRoot is the published Merkle root of one market interval.
type IntervalRoot struct {
	gorm.Model
	IntervalStart time.Time `gorm:"uniqueIndex"`
	Root          string
	TradeCount    int
	Mode          string // GRID_CONNECTED | ISLANDED
}
