// Package settlement is L5: signed meter readings, escrow, and a Merkle tree over
// each interval's settled trades. It replaces the proof-of-work chain: with one
// operator writing the database, mining added cost but no security, whereas signed
// readings and a published root let any household check that its trade is recorded.
package settlement

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"
)

// Trade states.
const (
	StatusCleared    = "CLEARED"
	StatusDispatched = "DISPATCHED"
	StatusVerified   = "VERIFIED"
	StatusSettled    = "SETTLED"
	StatusDisputed   = "DISPUTED"
)

// Reading is a meter's signed net energy for one interval (positive = exported).
type Reading struct {
	DeviceID      string    `json:"device_id"`
	IntervalStart time.Time `json:"interval_start"`
	NetKWh        float64   `json:"net_kwh"`
	Sig           []byte    `json:"sig"`
}

func (r Reading) payload() []byte {
	return []byte(fmt.Sprintf("%s|%d|%.6f", r.DeviceID, r.IntervalStart.Unix(), r.NetKWh))
}

// SignReading produces a signed reading for a device.
func SignReading(priv ed25519.PrivateKey, device string, start time.Time, netKWh float64) Reading {
	r := Reading{DeviceID: device, IntervalStart: start, NetKWh: netKWh}
	r.Sig = ed25519.Sign(priv, r.payload())
	return r
}

// Registry holds the public key of every enrolled meter.
type Registry struct {
	mu   sync.RWMutex
	keys map[string]ed25519.PublicKey
}

func NewRegistry() *Registry { return &Registry{keys: map[string]ed25519.PublicKey{}} }

func (g *Registry) Enroll(device string, pub ed25519.PublicKey) {
	g.mu.Lock()
	g.keys[device] = pub
	g.mu.Unlock()
}

// Verify reports whether the reading carries a valid signature from its device.
func (g *Registry) Verify(r Reading) bool {
	g.mu.RLock()
	pub, ok := g.keys[r.DeviceID]
	g.mu.RUnlock()
	return ok && ed25519.Verify(pub, r.payload(), r.Sig)
}

// Verdict is the outcome of checking one trade against both meters.
type Verdict struct {
	OK     bool
	Reason string
}

// CheckDelivery accepts a trade only if both readings are validly signed for the
// right interval and each meter's net energy is within tolerance of what the twin
// expected (forecast baseline plus the trade).
func CheckDelivery(reg *Registry, seller, buyer Reading, expSeller, expBuyer, tradeKWh float64) Verdict {
	if !reg.Verify(seller) || !reg.Verify(buyer) {
		return Verdict{false, "invalid meter signature"}
	}
	if !seller.IntervalStart.Equal(buyer.IntervalStart) {
		return Verdict{false, "interval mismatch"}
	}
	tol := math.Max(0.02, 0.15*tradeKWh)
	if d := seller.NetKWh - expSeller; math.Abs(d) > tol {
		return Verdict{false, fmt.Sprintf("seller delivery off by %.3f kWh", d)}
	}
	if d := buyer.NetKWh - expBuyer; math.Abs(d) > tol {
		return Verdict{false, fmt.Sprintf("buyer draw off by %.3f kWh", d)}
	}
	return Verdict{true, ""}
}

// Ledger holds wallets and escrow. Money moves only through Hold, Release and Refund.
type Ledger struct {
	mu       sync.Mutex
	Balances map[string]float64
	held     map[string]heldFunds
	Pool     float64 // solidarity pool
}

type heldFunds struct {
	buyer  string
	amount float64
}

func NewLedger() *Ledger {
	return &Ledger{Balances: map[string]float64{}, held: map[string]heldFunds{}}
}

// Hold moves the buyer's payment into escrow.
func (l *Ledger) Hold(tradeID, buyer string, amount float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Balances[buyer] -= amount
	l.held[tradeID] = heldFunds{buyer, amount}
}

// Release pays the seller from escrow, after taking the solidarity levy.
// It returns the levy taken.
func (l *Ledger) Release(tradeID, seller string, levyRate float64) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.held[tradeID]
	if !ok {
		return 0
	}
	delete(l.held, tradeID)
	levy := h.amount * levyRate
	l.Balances[seller] += h.amount - levy
	l.Pool += levy
	return levy
}

// Refund returns escrow to the buyer (used for disputed trades).
func (l *Ledger) Refund(tradeID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if h, ok := l.held[tradeID]; ok {
		l.Balances[h.buyer] += h.amount
		delete(l.held, tradeID)
	}
}

// Snapshot returns a copy of every wallet and the solidarity pool.
func (l *Ledger) Snapshot() (map[string]float64, float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]float64, len(l.Balances))
	for k, v := range l.Balances {
		out[k] = v
	}
	return out, l.Pool
}

// Credit adds money to a wallet (reflex payments, stability credits, pool payouts).
func (l *Ledger) Credit(id string, amount float64) {
	l.mu.Lock()
	l.Balances[id] += amount
	l.mu.Unlock()
}

// ---- Merkle tree over trade leaf hashes ----

// LeafHash hashes a canonical trade description.
func LeafHash(data string) string {
	h := sha256.Sum256(append([]byte{0x00}, data...))
	return hex.EncodeToString(h[:])
}

func node(l, r string) string {
	lb, _ := hex.DecodeString(l)
	rb, _ := hex.DecodeString(r)
	h := sha256.Sum256(append(append([]byte{0x01}, lb...), rb...))
	return hex.EncodeToString(h[:])
}

// MerkleRoot returns the root of the tree; an odd last node is paired with itself.
func MerkleRoot(leaves []string) string {
	if len(leaves) == 0 {
		h := sha256.Sum256(nil)
		return hex.EncodeToString(h[:])
	}
	level := append([]string(nil), leaves...)
	for len(level) > 1 {
		var next []string
		for i := 0; i < len(level); i += 2 {
			r := level[i]
			if i+1 < len(level) {
				r = level[i+1]
			}
			next = append(next, node(level[i], r))
		}
		level = next
	}
	return level[0]
}

// ProofStep is one sibling hash on the path from a leaf to the root.
type ProofStep struct {
	Hash string `json:"hash"`
	Left bool   `json:"left"` // sibling is on the left
}

// MerkleProof returns the inclusion proof for leaves[idx].
func MerkleProof(leaves []string, idx int) []ProofStep {
	var proof []ProofStep
	level := append([]string(nil), leaves...)
	for len(level) > 1 {
		sib := idx ^ 1
		if sib >= len(level) {
			sib = idx
		}
		proof = append(proof, ProofStep{Hash: level[sib], Left: sib < idx})
		var next []string
		for i := 0; i < len(level); i += 2 {
			r := level[i]
			if i+1 < len(level) {
				r = level[i+1]
			}
			next = append(next, node(level[i], r))
		}
		level = next
		idx /= 2
	}
	return proof
}

// VerifyProof checks that leaf is included under root.
func VerifyProof(leaf string, proof []ProofStep, root string) bool {
	cur := leaf
	for _, s := range proof {
		if s.Left {
			cur = node(s.Hash, cur)
		} else {
			cur = node(cur, s.Hash)
		}
	}
	return cur == root
}
