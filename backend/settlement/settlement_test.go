package settlement

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"
	"time"
)

func keys(t *testing.T, reg *Registry, id string) ed25519.PrivateKey {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reg.Enroll(id, pub)
	return priv
}

func TestSignatureRoundTripAndTamper(t *testing.T) {
	reg := NewRegistry()
	priv := keys(t, reg, "h1")
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	r := SignReading(priv, "h1", start, 1.25)
	if !reg.Verify(r) {
		t.Fatal("valid reading rejected")
	}
	r.NetKWh = 9
	if reg.Verify(r) {
		t.Fatal("tampered reading accepted")
	}
	other := SignReading(priv, "unknown", start, 1)
	if reg.Verify(other) {
		t.Fatal("unenrolled device accepted")
	}
}

func TestCheckDelivery(t *testing.T) {
	reg := NewRegistry()
	ps, pb := keys(t, reg, "s"), keys(t, reg, "b")
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	good := CheckDelivery(reg, SignReading(ps, "s", start, 1.0), SignReading(pb, "b", start, -1.0), 1.0, -1.0, 1.0)
	if !good.OK {
		t.Fatalf("good delivery rejected: %s", good.Reason)
	}
	short := CheckDelivery(reg, SignReading(ps, "s", start, 0.3), SignReading(pb, "b", start, -1.0), 1.0, -1.0, 1.0)
	if short.OK {
		t.Fatal("under-delivery accepted")
	}
	mismatch := CheckDelivery(reg, SignReading(ps, "s", start, 1.0), SignReading(pb, "b", start.Add(time.Minute), -1.0), 1.0, -1.0, 1.0)
	if mismatch.OK {
		t.Fatal("interval mismatch accepted")
	}
}

func TestEscrow(t *testing.T) {
	l := NewLedger()
	l.Hold("t1", "buyer", 10)
	levy := l.Release("t1", "seller", 0.02)
	if levy != 0.2 || l.Balances["seller"] != 9.8 || l.Balances["buyer"] != -10 || l.Pool != 0.2 {
		t.Fatalf("release wrong: %+v pool=%v levy=%v", l.Balances, l.Pool, levy)
	}
	l.Hold("t2", "buyer", 5)
	l.Refund("t2")
	if l.Balances["buyer"] != -10 {
		t.Fatalf("refund wrong: %v", l.Balances["buyer"])
	}
	// Money released or refunded cannot be released again.
	if l.Release("t2", "seller", 0) != 0 || l.Release("t1", "seller", 0) != 0 {
		t.Fatal("double release")
	}
	total := 0.0
	for _, v := range l.Balances {
		total += v
	}
	if total+l.Pool > 1e-9 || total+l.Pool < -1e-9 {
		t.Fatalf("money not conserved: %v", total+l.Pool)
	}
}

func TestMerkleProofs(t *testing.T) {
	for n := 1; n <= 9; n++ {
		var leaves []string
		for i := 0; i < n; i++ {
			leaves = append(leaves, LeafHash(fmt.Sprintf("trade-%d", i)))
		}
		root := MerkleRoot(leaves)
		for i := range leaves {
			p := MerkleProof(leaves, i)
			if !VerifyProof(leaves[i], p, root) {
				t.Fatalf("n=%d leaf %d proof failed", n, i)
			}
			if VerifyProof(LeafHash("forged"), p, root) {
				t.Fatalf("n=%d forged leaf accepted", n)
			}
		}
	}
}
