// Package chain is a permissioned, validator-signed blockchain for settlement.
//
// Each market interval becomes one block. A block carries the raw evidence for every
// zone (signed home meter readings, the signed feeder-head reading, the cleared
// trades) and the settlement outcome the delivery rules produced from it.
//
// Consensus. There are N validators (for example four utilities, a regulator and a
// community body). Block heights rotate through them as proposer. A validator signs a
// block only after it has re-run the delivery rules (delivery.Settle) itself on the
// block's evidence and got the identical outcome, and rebuilt the trade Merkle root.
// A block is final with 2f+1 signatures out of N = 3f+1, so up to f validators may be
// faulty or malicious without being able to finalise a wrong settlement or to stop
// honest ones from finalising a right one. This is the settlement logic running as a
// deterministic state transition on every validator: the "smart contract" is
// delivery.Settle.
//
// What a block does not prove. The evidence is only as good as the meters that signed
// it, and the cleared trades are taken as given: bids are private and are not on
// chain, so validators cannot re-run the auction itself. See docs/ARCHITECTURE.md.
//
// This implementation runs all validators inside one process. The protocol, the
// signatures and the checks are real, but there is no network layer, so it
// demonstrates the consensus rules, not a deployed multi-organisation network.
package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/DHSY-ishere/Nexus-VPP/backend/delivery"
	"github.com/DHSY-ishere/Nexus-VPP/backend/feeder"
	"github.com/DHSY-ishere/Nexus-VPP/backend/market"
	"github.com/DHSY-ishere/Nexus-VPP/backend/settlement"
)

// Genesis is the PrevHash of the first block.
const Genesis = "GENESIS"

// ZoneRecord is one zone's evidence and its settlement outcome for the interval.
type ZoneRecord struct {
	Input  delivery.Input  `json:"input"`
	Result delivery.Result `json:"result"`
}

// Sig is one validator's signature over a block hash.
type Sig struct {
	Validator string `json:"validator"`
	Sig       []byte `json:"sig"`
}

// Block is one interval's settlement.
type Block struct {
	Height    uint64       `json:"height"`
	PrevHash  string       `json:"prev_hash"`
	Time      time.Time    `json:"time"`
	Proposer  string       `json:"proposer"`
	Zones     []ZoneRecord `json:"zones"`
	TradeRoot string       `json:"trade_root"`
	Hash      string       `json:"hash"`
	Sigs      []Sig        `json:"sigs"`
}

type body struct {
	Height    uint64       `json:"height"`
	PrevHash  string       `json:"prev_hash"`
	Time      time.Time    `json:"time"`
	Proposer  string       `json:"proposer"`
	Zones     []ZoneRecord `json:"zones"`
	TradeRoot string       `json:"trade_root"`
}

// HashOf returns the block hash: SHA-256 over the canonical JSON of everything except
// the hash and the signatures.
func HashOf(b Block) string {
	raw, _ := json.Marshal(body{b.Height, b.PrevHash, b.Time, b.Proposer, b.Zones, b.TradeRoot})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func signPayload(hash string) []byte { return []byte("nexus-block|" + hash) }

// Leaf is a trade's canonical leaf string. It contains only fields every validator can
// verify from the block's evidence.
func Leaf(t market.Trade) string {
	return fmt.Sprintf("%s|%s|%s|%d|%.6f|%.6f|%s|%s|%.6f|%.6f",
		t.TradeID, t.SellerID, t.BuyerID, t.IntervalStart.Unix(),
		t.EnergyKWh, t.Price, t.GateResult, t.Status, t.Fill, t.Charged)
}

// FinalTrades returns the zone's trades with the settlement outcome applied.
func FinalTrades(zr ZoneRecord) []market.Trade {
	out := make([]market.Trade, len(zr.Input.Trades))
	copy(out, zr.Input.Trades)
	for i := range out {
		if i < len(zr.Result.Trades) && zr.Result.Trades[i].TradeID == out[i].TradeID {
			o := zr.Result.Trades[i]
			out[i].Status, out[i].Fill, out[i].Charged = o.Status, o.Fill, o.Charge
		}
	}
	return out
}

// TradeRoot is the Merkle root over every final trade of the interval, ordered by id.
func TradeRoot(zones []ZoneRecord) string {
	var all []market.Trade
	for _, z := range zones {
		all = append(all, FinalTrades(z)...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].TradeID < all[j].TradeID })
	leaves := make([]string, len(all))
	for i, t := range all {
		leaves[i] = settlement.LeafHash(Leaf(t))
	}
	return settlement.MerkleRoot(leaves)
}

// Oracle is the public data validators need to re-run the rules: meter public keys
// and the feeder models (public network data).
type Oracle struct {
	Reg     *settlement.Registry
	Feeders map[string]*feeder.Feeder
}

// Validator is one consensus participant.
type Validator struct {
	ID     string
	priv   ed25519.PrivateKey
	pub    ed25519.PublicKey
	Faulty bool // test hook: signs anything without checking (a Byzantine validator)
	Down   bool // test hook: signs nothing (a crashed validator)
}

// reexecute is the validity rule: everything a validator must independently confirm.
func (o Oracle) reexecute(b Block) error {
	if HashOf(b) != b.Hash {
		return errors.New("block hash does not match contents")
	}
	for _, z := range b.Zones {
		f, ok := o.Feeders[z.Input.Zone]
		if !ok {
			return fmt.Errorf("unknown zone %q", z.Input.Zone)
		}
		want := delivery.Settle(z.Input, o.Reg, f)
		wj, _ := json.Marshal(want)
		gj, _ := json.Marshal(z.Result)
		if string(wj) != string(gj) {
			return fmt.Errorf("zone %s: settlement outcome differs from independent re-execution", z.Input.Zone)
		}
	}
	if got := TradeRoot(b.Zones); got != b.TradeRoot {
		return errors.New("trade root does not match the settled trades")
	}
	return nil
}

// Chain is the validator set and the ledger they maintain.
type Chain struct {
	mu     sync.RWMutex
	vals   []*Validator
	quorum int
	oracle Oracle
	blocks []Block
	base   struct { // what precedes blocks[0] once old blocks are pruned
		height uint64
		prev   string
	}
	retain int
}

// New creates a chain with n validators (n >= 4 recommended) and quorum 2f+1 where
// f = (n-1)/3. retain caps stored blocks (0 = keep all).
func New(n int, oracle Oracle, retain int) *Chain {
	c := &Chain{oracle: oracle, retain: retain}
	c.base.prev = Genesis
	for i := 0; i < n; i++ {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		c.vals = append(c.vals, &Validator{ID: fmt.Sprintf("V%d", i+1), priv: priv, pub: pub})
	}
	f := (n - 1) / 3
	c.quorum = 2*f + 1
	return c
}

// Validators exposes the validator set (for tests that inject faults).
func (c *Chain) Validators() []*Validator { return c.vals }

// Quorum is the number of signatures a block needs.
func (c *Chain) Quorum() int { return c.quorum }

func (c *Chain) head() (height uint64, hash string) {
	if len(c.blocks) == 0 {
		return c.base.height, c.base.prev
	}
	b := c.blocks[len(c.blocks)-1]
	return b.Height + 1, b.Hash
}

// Propose builds the next block from the zone records, gathers validator signatures
// and appends it if a quorum signs.
func (c *Chain) Propose(t time.Time, zones []ZoneRecord) (Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	height, prev := c.head()
	b := Block{
		Height: height, PrevHash: prev, Time: t, Zones: zones,
		Proposer:  c.vals[int(height)%len(c.vals)].ID,
		TradeRoot: TradeRoot(zones),
	}
	b.Hash = HashOf(b)
	for _, v := range c.vals {
		if v.Down {
			continue
		}
		if !v.Faulty {
			if err := c.oracle.reexecute(b); err != nil {
				continue // an honest validator refuses to sign a block it cannot reproduce
			}
		}
		b.Sigs = append(b.Sigs, Sig{Validator: v.ID, Sig: ed25519.Sign(v.priv, signPayload(b.Hash))})
	}
	if len(b.Sigs) < c.quorum {
		return b, fmt.Errorf("block %d rejected: %d of %d signatures, need %d", height, len(b.Sigs), len(c.vals), c.quorum)
	}
	c.blocks = append(c.blocks, b)
	if c.retain > 0 && len(c.blocks) > c.retain {
		drop := len(c.blocks) - c.retain
		c.base.height, c.base.prev = c.blocks[drop-1].Height+1, c.blocks[drop-1].Hash
		c.blocks = append([]Block(nil), c.blocks[drop:]...)
	}
	return b, nil
}

// Verify audits the whole retained chain as an outsider would: hash links, heights,
// proposer rotation, a quorum of valid signatures from distinct known validators, and
// a full re-execution of every block's settlement. Any edit to history fails here.
func (c *Chain) Verify() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	prev, height := c.base.prev, c.base.height
	pubs := map[string]ed25519.PublicKey{}
	for _, v := range c.vals {
		pubs[v.ID] = v.pub
	}
	for _, b := range c.blocks {
		if b.Height != height {
			return fmt.Errorf("block %d: expected height %d", b.Height, height)
		}
		if b.PrevHash != prev {
			return fmt.Errorf("block %d: broken link to previous block", b.Height)
		}
		if want := c.vals[int(b.Height)%len(c.vals)].ID; b.Proposer != want {
			return fmt.Errorf("block %d: proposer %s, expected %s", b.Height, b.Proposer, want)
		}
		if err := c.oracle.reexecute(b); err != nil {
			return fmt.Errorf("block %d: %w", b.Height, err)
		}
		seen := map[string]bool{}
		for _, s := range b.Sigs {
			pub, ok := pubs[s.Validator]
			if !ok || seen[s.Validator] || !ed25519.Verify(pub, signPayload(b.Hash), s.Sig) {
				continue
			}
			seen[s.Validator] = true
		}
		if len(seen) < c.quorum {
			return fmt.Errorf("block %d: %d valid signatures, need %d", b.Height, len(seen), c.quorum)
		}
		prev, height = b.Hash, b.Height+1
	}
	return nil
}

// Info summarises the chain.
type Info struct {
	Height     uint64   `json:"height"` // number of the next block
	Blocks     int      `json:"blocks"` // blocks retained
	HeadHash   string   `json:"head_hash"`
	Validators []string `json:"validators"`
	Quorum     int      `json:"quorum"`
}

func (c *Chain) Info() Info {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h, hash := c.head()
	ids := make([]string, len(c.vals))
	for i, v := range c.vals {
		ids[i] = v.ID
	}
	return Info{Height: h, Blocks: len(c.blocks), HeadHash: hash, Validators: ids, Quorum: c.quorum}
}

// Block returns the block at a height, if retained.
func (c *Chain) Block(height uint64) (Block, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, b := range c.blocks {
		if b.Height == height {
			return b, true
		}
	}
	return Block{}, false
}

// Latest returns the newest block.
func (c *Chain) Latest() (Block, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.blocks) == 0 {
		return Block{}, false
	}
	return c.blocks[len(c.blocks)-1], true
}

// FindTrade returns the block and zone record that settled a trade, newest first.
func (c *Chain) FindTrade(tradeID string) (Block, ZoneRecord, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := len(c.blocks) - 1; i >= 0; i-- {
		for _, z := range c.blocks[i].Zones {
			for _, t := range z.Input.Trades {
				if t.TradeID == tradeID {
					return c.blocks[i], z, true
				}
			}
		}
	}
	return Block{}, ZoneRecord{}, false
}
