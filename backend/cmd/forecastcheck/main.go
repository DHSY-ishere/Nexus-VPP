// Command forecastcheck calls the running Python forecast service over gRPC.
//
//	go run ./cmd/forecastcheck -addr 127.0.0.1:50051
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/DHSY-ishere/Nexus-VPP/backend/forecastclient"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:50051", "forecast service address")
	flag.Parse()
	c, err := forecastclient.Dial(*addr)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	ok, ver, err := c.Ready(context.Background())
	if err != nil {
		log.Fatalf("health: %v", err)
	}
	fmt.Printf("service ready=%v model=%s\n", ok, ver)

	cases := map[string][]float64{
		"steady clear sky":     {0.97, 0.97, 0.96, 0.97, 0.97, 0.96, 0.97},
		"cloud front arriving": {0.97, 0.95, 0.9, 0.8, 0.65, 0.5, 0.4},
		"cloud clearing":       {0.3, 0.3, 0.35, 0.45, 0.6, 0.75, 0.85},
	}
	for name, hist := range cases {
		f, err := c.Forecast(context.Background(), "demo", 14.0, hist)
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		fmt.Printf("%-22s now=%.2f  predicted k(+15min)=%.2f  solar fraction=%.2f  (persistence %.2f)\n",
			name, hist[len(hist)-1], f.ClearSkyIndex15m, f.SolarFraction15m, f.PersistenceIndex15)
	}
}
