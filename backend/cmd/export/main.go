// Command export runs the simulated community for N intervals (default 288, one
// day) with no Kafka/Postgres and writes CSVs for report graphs:
//
//	go run ./cmd/export -out ../results -steps 288 -hour 0
//	go run ./cmd/export -out ../results/lie -steps 120 -hour 9 -fault lie -fault-from 48
//
// -fault is none, tamper (a seller under-delivers, honest meter) or lie (a seller
// under-delivers and its meter claims full delivery). It starts at interval -fault-from.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/DHSY-ishere/Nexus-VPP/backend/engine"
)

func f(v float64) string { return fmt.Sprintf("%.6f", v) }
func b(v bool) string    { return fmt.Sprint(v) }

func main() {
	out := flag.String("out", "results", "output directory")
	steps := flag.Int("steps", 288, "intervals to simulate (288 = 24h)")
	hour := flag.Float64("hour", 0, "start hour of day")
	gate := flag.String("gate", engine.GateLearned, "learned or model")
	fault := flag.String("fault", "none", "none, tamper or lie")
	faultFrom := flag.Int("fault-from", 0, "interval at which the fault starts")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}

	cfg := engine.DefaultConfig()
	cfg.StartHour, cfg.Gate = *hour, *gate
	e := engine.New(cfg, nil, nil)

	zf, _ := os.Create(filepath.Join(*out, "zones.csv"))
	tf, _ := os.Create(filepath.Join(*out, "trades.csv"))
	vf, _ := os.Create(filepath.Join(*out, "intervals.csv"))
	defer zf.Close()
	defer tf.Close()
	defer vf.Close()
	zw, tw, vw := csv.NewWriter(zf), csv.NewWriter(tf), csv.NewWriter(vf)

	zw.Write([]string{"interval", "zone", "price", "auction_kwh", "cleared_kwh", "gated_kwh", "trades", "stress_before", "stress_after", "peak_load_pct", "min_pu", "max_pu", "gate_mode", "buses_learned", "sens_err_pct", "model_cleared_kwh", "conserved", "excess_kwh", "tolerance_kwh", "loss_kwh", "claimed_usd", "confirmed_usd"})
	tw.Write([]string{"interval", "trade_id", "zone", "seller", "buyer", "energy_kwh", "price", "loss_factor", "gate_result", "measured_kwh", "stability_credit", "levy", "status", "fill", "charged"})
	vw.Write([]string{"interval", "mode", "cloud", "trades", "disputed", "merkle_root", "pool_balance", "community_savings", "gini", "stress_removed", "reflex_kw", "block_height", "block_hash", "block_sigs", "proposer", "fault"})

	for i := 0; i < *steps; i++ {
		if i == *faultFrom {
			switch *fault {
			case "tamper":
				e.SetTamper(true)
			case "lie":
				e.SetMeterLie(true)
			}
		}
		active := "none"
		if i >= *faultFrom {
			active = *fault
		}
		r := e.Step()
		ts := r.Interval.Format("2006-01-02T15:04:05")
		for _, z := range r.Zones {
			zw.Write([]string{ts, z.Zone, f(z.Price), f(z.AuctionKWh), f(z.ClearedKWh), f(z.GatedKWh), fmt.Sprint(z.Trades), f(z.StressBefore), f(z.StressAfter), f(z.PeakLoadPct), f(z.MinPU), f(z.MaxPU), z.GateMode, fmt.Sprint(z.BusesLearned), f(z.SensErrPct), f(z.ModelClearedKWh), b(z.Conserved), f(z.ExcessKWh), f(z.ToleranceKWh), f(z.LossKWh), f(z.ClaimedUSD), f(z.ConfirmedUSD)})
		}
		for _, t := range r.Trades {
			tw.Write([]string{ts, t.TradeID, t.Zone, t.SellerID, t.BuyerID, f(t.EnergyKWh), f(t.Price), f(t.LossFactor), t.GateResult, f(t.MeasuredKWh), f(t.StabilityCredit), f(t.Levy), t.Status, f(t.Fill), f(t.Charged)})
		}
		vw.Write([]string{ts, r.Mode, f(r.Cloud), fmt.Sprint(len(r.Trades)), fmt.Sprint(r.Disputed), r.MerkleRoot, f(r.PoolBalance), f(r.CommunitySavings), f(r.Gini), f(r.StressRemoved), f(r.ReflexKW), fmt.Sprint(r.BlockHeight), r.BlockHash, fmt.Sprint(r.BlockSigs), r.Proposer, active})
	}
	zw.Flush()
	tw.Flush()
	vw.Flush()
	if err := e.Chain().Verify(); err != nil {
		log.Fatalf("chain audit failed: %v", err)
	}
	fmt.Printf("wrote %s/{zones,trades,intervals}.csv for %d intervals; chain audit passed (%d blocks)\n", *out, *steps, e.Chain().Info().Blocks)
}
