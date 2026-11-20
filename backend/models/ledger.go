package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	BlockIndex       uint
	PreviousHash     string
	BlockHash        string
	Nonce            int
}

// canonicalTime is the one textual form of a timestamp that the block hash uses. PostgreSQL stores
// microseconds and returns times in its own time zone, so a hash over the raw time.Now() value
// (nanoseconds, local zone) can never be reproduced after a database round trip. UTC at
// microsecond precision survives it.
func canonicalTime(t time.Time) string {
	return t.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}

func ComputeBlockHash(trade P2PTrade) string {
	data := fmt.Sprintf("%s%s%f%f%f%s%s%d",
		trade.TradeID,
		trade.ContributorID,
		trade.EnergyProvidedKw,
		trade.CreditEarned,
		trade.GridFrequency,
		trade.PreviousHash,
		canonicalTime(trade.SettledAt),
		trade.Nonce,
	)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func MineBlock(trade *P2PTrade, previousHash string) {
	trade.PreviousHash = previousHash
	trade.Nonce = 0
	for {
		hash := ComputeBlockHash(*trade)
		if hash[:2] == "00" {
			trade.BlockHash = hash
			break
		}
		trade.Nonce++
	}
}
