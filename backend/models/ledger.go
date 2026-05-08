package models

import (
	"time"

	"gorm.io/gorm"
)

// P2PTrade represents a single energy transaction where a contributor supplied the grid.
type P2PTrade struct {
	gorm.Model
	TradeID          string `gorm:"uniqueIndex"`
	ContributorID    string `gorm:"index"`
	EnergyProvidedKw float64
	CreditEarned     float64
	GridFrequency    float64 // The frequency at the time of the trade (proves strain)
	SettledAt        time.Time
}
