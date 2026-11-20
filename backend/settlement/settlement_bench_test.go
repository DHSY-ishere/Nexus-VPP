package settlement

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"testing"
	"time"
)

func BenchmarkSignReading(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	t := time.Now()
	for i := 0; i < b.N; i++ {
		SignReading(priv, "h1", t, 1.25)
	}
}

func BenchmarkVerifyReading(b *testing.B) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	reg := NewRegistry()
	reg.Enroll("h1", pub)
	r := SignReading(priv, "h1", time.Now(), 1.25)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.Verify(r)
	}
}

func leaves(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = LeafHash(fmt.Sprintf("trade-%d", i))
	}
	return out
}

func BenchmarkMerkleRoot(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		l := leaves(n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				MerkleRoot(l)
			}
		})
	}
}

func BenchmarkMerkleProofVerify(b *testing.B) {
	l := leaves(1000)
	root := MerkleRoot(l)
	p := MerkleProof(l, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VerifyProof(l[500], p, root)
	}
}
